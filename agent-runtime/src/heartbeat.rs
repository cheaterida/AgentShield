//! 周期性心跳任务：定期向 management-server 报告 agent 状态。
//! 同时处理服务端下发的 suggested_action 指令。

use crate::client::ManagementClient;
use crate::event_buffer::EventBuffer;
use crate::policy_cache::PolicyCache;
use crate::probe_manager::ProbeManager;
use crate::supervisor::Supervisor;
use std::sync::Arc;
use std::sync::Mutex;
use std::time::Duration;
use sysinfo::{Pid, System};

#[cfg(feature = "checkpoint")]
use crate::checkpoint::CheckpointManager;

pub struct HeartbeatTask {
    client: ManagementClient,
    interval: Duration,
    probe_manager: Arc<ProbeManager>,
    event_buffer: Arc<EventBuffer>,
    policy_cache: Arc<PolicyCache>,
    /// Cached sysinfo handle + own PID for CPU/mem collection.
    sys: Mutex<System>,
    own_pid: Pid,
    supervisor: Option<Arc<Supervisor>>,
    #[cfg(feature = "checkpoint")]
    checkpoint_manager: Option<Arc<CheckpointManager>>,
}

impl HeartbeatTask {
    pub fn new(
        client: ManagementClient,
        interval_secs: u64,
        probe_manager: Arc<ProbeManager>,
        event_buffer: Arc<EventBuffer>,
        policy_cache: Arc<PolicyCache>,
    ) -> Self {
        let own_pid = Pid::from(std::process::id() as usize);
        Self {
            client,
            interval: Duration::from_secs(interval_secs),
            probe_manager,
            event_buffer,
            policy_cache,
            sys: Mutex::new(System::new_all()),
            own_pid,
            supervisor: None,
            #[cfg(feature = "checkpoint")]
            checkpoint_manager: None,
        }
    }

    #[cfg(feature = "checkpoint")]
    pub fn with_checkpoint_manager(mut self, cm: Arc<CheckpointManager>) -> Self {
        self.checkpoint_manager = Some(cm);
        self
    }

    pub fn with_supervisor(mut self, sup: Arc<Supervisor>) -> Self {
        self.supervisor = Some(sup);
        self
    }

    pub async fn run(self) {
        let mut interval = tokio::time::interval(self.interval);
        // skip the immediate first tick (let register complete)
        interval.tick().await;

        loop {
            interval.tick().await;

            let cpu = self.collect_cpu();
            let mem = self.collect_memory();
            let active_probes = self.probe_manager.active_count();
            let buffered = self.event_buffer.len() as i32;
            let policy_ver = self.policy_cache.current_version();

            match self
                .client
                .heartbeat(cpu, mem, active_probes, &policy_ver, buffered)
                .await
            {
                Ok(resp) => {
                    self.handle_suggested_action(&resp);
                    if !resp.latest_policy_version.is_empty()
                        && resp.latest_policy_version != policy_ver
                    {
                        tracing::info!(
                            current = %policy_ver,
                            latest = %resp.latest_policy_version,
                            "policy update available"
                        );
                    }
                }
                Err(e) => {
                    tracing::warn!("heartbeat failed: {}", e);
                }
            }
        }
    }

    fn handle_suggested_action(&self, resp: &crate::client::HeartbeatResp) {
        let action = resp.suggested_action.trim();

        match action {
            "isolate" => {
                tracing::warn!(
                    agent_id = %self.client.agent_id,
                    "management-server requested isolation"
                );
            }
            "quota_exceeded" => {
                tracing::error!(
                    agent_id = %self.client.agent_id,
                    quota_status = %resp.quota_status,
                    usage = resp.token_usage_today,
                    limit = resp.token_quota_daily,
                    "token quota exceeded — stopping hermes"
                );
                if let Some(ref sup) = self.supervisor {
                    sup.stop();
                }
            }
            _ => {}
        }

        #[cfg(feature = "checkpoint")]
        self.handle_checkpoint_action(action);
    }

    #[cfg(feature = "checkpoint")]
    fn handle_checkpoint_action(&self, action: &str) {
        if action.starts_with("rollback_to:") {
            let checkpoint_id = action.strip_prefix("rollback_to:").unwrap_or("");
            tracing::warn!(
                checkpoint_id = checkpoint_id,
                "management-server requested rollback"
            );

            if let Some(ref cm) = self.checkpoint_manager {
                match cm.rollback(checkpoint_id, "management-server directive", 0.8, &[]) {
                    Ok(_entry) => {
                        tracing::info!(
                            checkpoint_id = checkpoint_id,
                            "rollback completed, remediation prompt injected"
                        );

                        // 重启 Hermes 以使用恢复后的状态
                        if let Some(ref sup) = self.supervisor {
                            if let Some(ref path) = std::env::var("AGENTSHIELD_HERMES_BINARY").ok() {
                                if let Err(e) = sup.restart(path) {
                                    tracing::error!(error = %e, "hermes restart after rollback failed");
                                } else {
                                    tracing::info!("hermes restarted with rollback state");
                                }
                            }
                        }
                    }
                    Err(e) => {
                        tracing::error!(
                            checkpoint_id = checkpoint_id,
                            error = %e,
                            "rollback failed"
                        );
                    }
                }
            } else {
                tracing::warn!("rollback requested but checkpoint manager not configured");
            }
        }

        if action == "create_checkpoint" {
            tracing::info!("management-server requested checkpoint creation");

            if let Some(ref cm) = self.checkpoint_manager {
                // 创建最小快照（心跳任务不持有消息历史，仅做文件级快照）
                let entry = crate::checkpoint::journal::JournalEntry {
                    checkpoint_id: format!(
                        "hb-{}",
                        std::time::SystemTime::now()
                            .duration_since(std::time::SystemTime::UNIX_EPOCH)
                            .map(|d| d.as_secs().to_string())
                            .unwrap_or_default()
                    ),
                    agent_id: self.client.agent_id.clone(),
                    created_at: chrono_now(),
                    step_number: 0,
                    messages: vec![],
                    tool_definitions: "[]".into(),
                    working_memory: String::new(),
                    token_usage: crate::checkpoint::journal::TokenUsage {
                        input_tokens: 0,
                        output_tokens: 0,
                    },
                    risk_snapshot: crate::checkpoint::journal::RiskSnapshot {
                        ema_score: 0.0,
                        threshold: 0.6,
                    },
                    previous_checkpoint_id: None,
                };

                match cm.create(&entry) {
                    Ok(id) => tracing::info!(checkpoint_id = %id, "checkpoint created via heartbeat"),
                    Err(e) => tracing::error!(error = %e, "checkpoint creation failed"),
                }
            } else {
                tracing::warn!("checkpoint requested but checkpoint manager not configured");
            }
        }
    }

    fn collect_cpu(&self) -> f64 {
        let mut sys = self.sys.lock().unwrap();
        sys.refresh_processes(sysinfo::ProcessesToUpdate::Some(&[self.own_pid]));
        sys.refresh_cpu_all();
        if let Some(proc) = sys.process(self.own_pid) {
            proc.cpu_usage() as f64
        } else {
            0.0
        }
    }

    fn collect_memory(&self) -> u64 {
        let mut sys = self.sys.lock().unwrap();
        sys.refresh_processes(sysinfo::ProcessesToUpdate::Some(&[self.own_pid]));
        if let Some(proc) = sys.process(self.own_pid) {
            proc.memory()
        } else {
            0
        }
    }
}

#[cfg(feature = "checkpoint")]
fn chrono_now() -> String {
    use std::time::SystemTime;
    match SystemTime::now().duration_since(SystemTime::UNIX_EPOCH) {
        Ok(dur) => dur.as_secs().to_string(),
        Err(_) => "unknown".into(),
    }
}
