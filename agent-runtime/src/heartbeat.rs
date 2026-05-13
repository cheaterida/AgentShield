//! 周期性心跳任务：定期向 management-server 报告 agent 状态。

use crate::client::ManagementClient;
use crate::event_buffer::EventBuffer;
use crate::probe_manager::ProbeManager;
use std::sync::Arc;
use std::time::Duration;

pub struct HeartbeatTask {
    client: ManagementClient,
    interval: Duration,
    probe_manager: Arc<ProbeManager>,
    event_buffer: Arc<EventBuffer>,
}

impl HeartbeatTask {
    pub fn new(
        client: ManagementClient,
        interval_secs: u64,
        probe_manager: Arc<ProbeManager>,
        event_buffer: Arc<EventBuffer>,
    ) -> Self {
        Self {
            client,
            interval: Duration::from_secs(interval_secs),
            probe_manager,
            event_buffer,
        }
    }

    pub async fn run(self) {
        let mut interval = tokio::time::interval(self.interval);
        // skip the immediate first tick (let register complete)
        interval.tick().await;

        loop {
            interval.tick().await;

            let cpu = collect_cpu();
            let mem = collect_memory();
            let active_probes = self.probe_manager.active_count();
            let buffered = self.event_buffer.len() as i32;

            match self
                .client
                .heartbeat(cpu, mem, active_probes, "v0.1.0", buffered)
                .await
            {
                Ok(resp) => {
                    if resp.suggested_action == "isolate" {
                        tracing::warn!(
                            agent_id = %self.client.agent_id,
                            "management-server requested isolation"
                        );
                    }
                }
                Err(e) => {
                    tracing::warn!("heartbeat failed: {}", e);
                }
            }
        }
    }
}

fn collect_cpu() -> f64 {
    // Placeholder: use sysinfo or read /proc/stat on Linux
    0.0
}

fn collect_memory() -> u64 {
    // Placeholder: use sysinfo or read /proc/self/statm
    0
}
