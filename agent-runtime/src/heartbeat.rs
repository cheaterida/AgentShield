//! 周期性心跳任务：定期向 management-server 报告 agent 状态。

use crate::client::ManagementClient;
use crate::event_buffer::EventBuffer;
use crate::policy_cache::PolicyCache;
use crate::probe_manager::ProbeManager;
use std::sync::Arc;
use std::sync::Mutex;
use std::time::Duration;
use sysinfo::{Pid, System};

pub struct HeartbeatTask {
    client: ManagementClient,
    interval: Duration,
    probe_manager: Arc<ProbeManager>,
    event_buffer: Arc<EventBuffer>,
    policy_cache: Arc<PolicyCache>,
    /// Cached sysinfo handle + own PID for CPU/mem collection.
    sys: Mutex<System>,
    own_pid: Pid,
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
        }
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
                    if resp.suggested_action == "isolate" {
                        tracing::warn!(
                            agent_id = %self.client.agent_id,
                            "management-server requested isolation"
                        );
                    }
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

    fn collect_cpu(&self) -> f64 {
        let mut sys = self.sys.lock().unwrap();
        sys.refresh_processes(sysinfo::ProcessesToUpdate::Some(&[self.own_pid]));
        sys.refresh_cpu_all();
        if let Some(proc) = sys.process(self.own_pid) {
            // cpu_usage() returns percentage (0–100 × core-count)
            proc.cpu_usage() as f64
        } else {
            0.0
        }
    }

    fn collect_memory(&self) -> u64 {
        let mut sys = self.sys.lock().unwrap();
        sys.refresh_processes(sysinfo::ProcessesToUpdate::Some(&[self.own_pid]));
        if let Some(proc) = sys.process(self.own_pid) {
            proc.memory() // bytes
        } else {
            0
        }
    }
}
