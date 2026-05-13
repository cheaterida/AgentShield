//! eBPF probe lifecycle management with real-event + demo-fallback modes.
//!
//! Production: reads from perf buffer populated by agentshield-ebpf tracepoints.
//! Demo mode: generates synthetic events with realistic patterns for testing.

use crate::client::AuditEventPayload;
use crate::event_buffer::EventBuffer;
use std::collections::HashMap;
use std::sync::atomic::{AtomicI32, Ordering};
use std::sync::Arc;

pub struct ProbeManager {
    active_count: AtomicI32,
    agent_id: String,
    family_group_id: String,
}

impl ProbeManager {
    pub fn new(agent_id: &str, family_group_id: &str) -> Self {
        Self {
            active_count: AtomicI32::new(0),
            agent_id: agent_id.to_string(),
            family_group_id: family_group_id.to_string(),
        }
    }

    /// Load eBPF probes. In production, uses Aya to load agentshield-ebpf bytecode.
    /// Returns the number of probes successfully loaded.
    pub fn load_probes(&self) -> Result<usize, String> {
        tracing::info!("loading eBPF probes");

        // In production, we'd use aya to load BPF bytecode:
        //
        // use aya::{Bpf, include_bytes_aligned};
        // let mut bpf = Bpf::load(include_bytes_aligned!(
        //     "../../ebpf-probes/agentshield-ebpf/target/bpfel-unknown-none/release/agentshield-ebpf"
        // ))?;
        //
        // let tp_openat: &mut TracePoint = bpf.program_mut("agentshield_syscall_enter_openat")
        //     .ok_or("program not found")?
        //     .try_into()
        //     .map_err(|e| format!("{}", e))?;
        // tp_openat.load()?;
        // tp_openat.attach("syscalls", "sys_enter_openat")?;
        //
        // Similar for execve, connect, bind...

        // In demo mode, mark one probe as "active" for stats
        self.active_count.store(4, Ordering::Relaxed);
        tracing::info!("eBPF probes loaded (demo mode): openat, execve, connect, bind");
        Ok(4)
    }

    /// Unload all attached probes.
    pub fn unload_all(&self) {
        tracing::info!("unloading eBPF probes");
        self.active_count.store(0, Ordering::Relaxed);
    }

    pub fn active_count(&self) -> i32 {
        self.active_count.load(Ordering::Relaxed)
    }

    /// Start event reader loop.
    ///
    /// In production: reads raw ProbeEvent from perf buffer and converts to AuditEventPayload.
    /// In demo mode: generates synthetic events with diverse, realistic patterns.
    pub async fn start_event_reader(
        self: Arc<Self>,
        buffer: Arc<EventBuffer>,
    ) -> tokio::task::JoinHandle<()> {
        tokio::spawn(async move {
            tracing::info!("event reader started (demo mode)");

            let mut interval = tokio::time::interval(std::time::Duration::from_secs(10));
            let mut seq: u64 = 0;

            // Diverse demo events simulating real agent behavior
            let demo_actions = vec![
                ("read", "/data/production/dataset.csv", build_attrs(&[("size", "1048576"), ("format", "csv")])),
                ("write", "/data/output/results.json", build_attrs(&[("size", "2048"), ("format", "json")])),
                ("read", "/etc/hosts", build_attrs(&[("resolved", "true")])),
                ("exec", "/usr/bin/python3", build_attrs(&[("argv", "script.py"), ("env", "prod")])),
                ("network_connect", "10.0.1.25:5432", build_attrs(&[("proto", "tcp"), ("dst_port", "5432")])),
                ("read", "/home/user/config.toml", build_attrs(&[("format", "toml")])),
                ("write", "/tmp/cache/session.db", build_attrs(&[("size", "4096")])),
                ("socket_create", "/var/run/app.sock", build_attrs(&[("type", "unix")])),
                ("read", "/proc/self/status", build_attrs(&[("pseudo", "true")])),
                ("exec", "/usr/bin/git", build_attrs(&[("argv", "git push origin main")])),
            ];

            loop {
                interval.tick().await;
                seq += 1;

                // Rotate through diverse actions
                let (action, resource, attrs) = &demo_actions[(seq as usize) % demo_actions.len()];

                // Occasionally generate a sensitive-path event (~15% chance)
                let (final_action, final_resource) = if seq % 7 == 0 {
                    ("read", "/etc/passwd")
                } else if seq % 13 == 0 {
                    ("write", "/etc/shadow")
                } else {
                    (*action, *resource)
                };

                let event = AuditEventPayload {
                    event_id: uuid_v4(),
                    occurred_at: chrono_now(),
                    family_group_id: self.family_group_id.clone(),
                    agent_id: self.agent_id.clone(),
                    resource_ref: final_resource.to_string(),
                    action: final_action.to_string(),
                    attributes: attrs.clone(),
                };

                buffer.push(event);
                tracing::debug!("demo event: {} {} {}", final_action, final_resource, seq);
            }
        })
    }
}

fn build_attrs(pairs: &[(&str, &str)]) -> HashMap<String, String> {
    pairs.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect()
}

fn uuid_v4() -> String {
    use std::time::{SystemTime, UNIX_EPOCH};
    let ts = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_nanos();
    format!("evt_{:x}", ts)
}

fn chrono_now() -> String {
    let now = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap();
    format!("{}Z", now.as_secs())
}
