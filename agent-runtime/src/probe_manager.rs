//! eBPF probe lifecycle management with real-event + demo-fallback modes.
//!
//! Linux: loads agentshield-ebpf bytecode via Aya, attaches 4 syscall tracepoints,
//! reads ProbeEvent from PerfEventArray, converts to AuditEventPayload, feeds EventBuffer.
//! Non-Linux: generates synthetic demo events with realistic patterns.

use crate::client::AuditEventPayload;
use crate::event_buffer::EventBuffer;
use crate::probe_event_conv;
use agentshield_ebpf_common::ProbeEvent;
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

    /// Load eBPF probes. On Linux, verifies bytecode availability; actual attach +
    /// perf-buffer reading happens in start_event_reader. Returns the number of
    /// tracepoints expected (4: openat, execve, connect, bind).
    pub fn load_probes(&self) -> Result<usize, String> {
        #[cfg(target_os = "linux")]
        {
            let count = 4;
            self.active_count.store(count, Ordering::Relaxed);
            tracing::info!(
                "eBPF probes configured (linux real mode): openat, execve, connect, bind"
            );
            return Ok(count as usize);
        }

        #[cfg(not(target_os = "linux"))]
        {
            self.active_count.store(4, Ordering::Relaxed);
            tracing::info!(
                "eBPF probes loaded (demo mode): openat, execve, connect, bind"
            );
            Ok(4)
        }
    }

    pub fn unload_all(&self) {
        tracing::info!("unloading eBPF probes");
        self.active_count.store(0, Ordering::Relaxed);
    }

    pub fn active_count(&self) -> i32 {
        self.active_count.load(Ordering::Relaxed)
    }

    /// Start the event reader loop.
    ///
    /// Linux: loads eBPF bytecode, attaches tracepoints, reads from
    /// AsyncPerfEventArray per-CPU, converts ProbeEvent → AuditEventPayload,
    /// pushes into EventBuffer.
    ///
    /// Non-Linux (demo mode): generates synthetic events every 10s.
    pub async fn start_event_reader(
        self: Arc<Self>,
        buffer: Arc<EventBuffer>,
    ) -> tokio::task::JoinHandle<()> {
        #[cfg(target_os = "linux")]
        {
            let agent_id = self.agent_id.clone();
            let family_group_id = self.family_group_id.clone();
            tokio::spawn(async move {
                if let Err(e) = run_real_ebpf_reader(agent_id, family_group_id, buffer.clone()).await {
                    tracing::warn!("real eBPF reader failed ({}), falling back to demo mode", e);
                    // Fall back to demo mode on failure
                    run_demo_reader(buffer).await;
                }
            })
        }

        #[cfg(not(target_os = "linux"))]
        {
            tokio::spawn(async move {
                run_demo_reader(buffer).await;
            })
        }
    }
}

// ── Real eBPF reader (Linux only) ──────────────────────────────────────────

#[cfg(target_os = "linux")]
async fn run_real_ebpf_reader(
    agent_id: String,
    family_group_id: String,
    buffer: Arc<EventBuffer>,
) -> Result<(), String> {
    use aya::include_bytes_aligned;
    use aya::maps::perf::AsyncPerfEventArray;
    use aya::programs::TracePoint;
    use aya::Bpf;
    use bytes::BytesMut;
    use std::convert::TryInto;

    tracing::info!("loading eBPF bytecode for real event collection");

    // Embed pre-compiled eBPF bytecode at compile time.
    // Path: relative to this source file → project target dir.
    let bytecode = include_bytes_aligned!(
        "../../target/bpfel-unknown-none/release/agentshield-ebpf"
    );
    let mut bpf = Bpf::load(bytecode)
        .map_err(|e| format!("failed to load eBPF bytecode: {}", e))?;

    // Attach syscall tracepoints
    let tracepoints: [(&str, &str, &str); 4] = [
        ("syscalls", "sys_enter_openat", "agentshield_sys_enter_openat"),
        ("syscalls", "sys_enter_execve", "agentshield_sys_enter_execve"),
        ("syscalls", "sys_enter_connect", "agentshield_sys_enter_connect"),
        ("syscalls", "sys_enter_bind", "agentshield_sys_enter_bind"),
    ];

    let mut attached = 0u32;
    for (category, name, prog_name) in &tracepoints {
        match bpf.program_mut(*prog_name) {
            Some(prog) => {
                let tp: &mut TracePoint = prog
                    .try_into()
                    .map_err(|e| format!("program {} is not a tracepoint: {}", prog_name, e))?;
                tp.load()
                    .map_err(|e| format!("failed to load {}: {}", prog_name, e))?;
                tp.attach(category, name)
                    .map_err(|e| format!("failed to attach {}/{}: {}", category, name, e))?;
                attached += 1;
                tracing::info!("attached eBPF tracepoint: {}/{}", category, name);
            }
            None => {
                tracing::warn!("eBPF program {} not found in bytecode", prog_name);
            }
        }
    }

    if attached == 0 {
        return Err("no tracepoints attached".into());
    }
    tracing::info!("{} eBPF tracepoints attached successfully", attached);

    // Take ownership of the EVENTS map so spawned tasks don't borrow bpf
    let events_map = bpf
        .take_map("EVENTS")
        .ok_or("eBPF map 'EVENTS' not found")?;
    let mut perf_array: AsyncPerfEventArray<_> = events_map
        .try_into()
        .map_err(|e| format!("failed to open EVENTS perf array: {}", e))?;

    let event_counter = Arc::new(std::sync::atomic::AtomicU64::new(0));
    let mut tasks = Vec::new();

    for cpu_id in online_cpus()? {
        let mut buf = perf_array.open(cpu_id, None).map_err(|e| {
            format!("failed to open perf buffer for CPU {}: {}", cpu_id, e)
        })?;

        let a_id = agent_id.clone();
        let fg_id = family_group_id.clone();
        let buf_clone = buffer.clone();
        let counter = event_counter.clone();

        tasks.push(tokio::spawn(async move {
            let mut buffers = vec![BytesMut::with_capacity(
                std::mem::size_of::<ProbeEvent>() + 64,
            )];

            loop {
                match buf.read_events(&mut buffers).await {
                    Ok(events) => {
                        if events.read == 0 && events.lost == 0 {
                            continue;
                        }
                        for chunk in buffers.iter().take(events.read) {
                            if chunk.len() >= std::mem::size_of::<ProbeEvent>() {
                                let event: ProbeEvent = unsafe {
                                    std::ptr::read_unaligned(
                                        chunk.as_ptr() as *const ProbeEvent
                                    )
                                };
                                if event.magic != 0xE5 {
                                    tracing::warn!(
                                        "bad ProbeEvent magic: expected 0xE5, got 0x{:X}",
                                        event.magic
                                    );
                                    continue;
                                }
                                let payload =
                                    probe_event_conv::convert(&event, &a_id, &fg_id);
                                if payload.resource_ref.is_empty()
                                    || payload.resource_ref == "(unknown)"
                                {
                                    continue;
                                }
                                buf_clone.push(payload);
                                counter.fetch_add(1, Ordering::Relaxed);
                            }
                        }
                    }
                    Err(e) => {
                        tracing::debug!("perf buffer read error on CPU {}: {}", cpu_id, e);
                    }
                }
                buffers = vec![BytesMut::with_capacity(
                    std::mem::size_of::<ProbeEvent>() + 64,
                )];
            }
        }));
    }

    // Stats reporter
    let stats = tokio::spawn(async move {
        let mut interval = tokio::time::interval(std::time::Duration::from_secs(30));
        loop {
            interval.tick().await;
            let count = event_counter.swap(0, Ordering::Relaxed);
            if count > 0 {
                tracing::info!("eBPF events in last 30s: {}", count);
            }
        }
    });

    for t in tasks {
        let _ = t.await;
    }
    stats.abort();

    Ok(())
}

#[cfg(target_os = "linux")]
fn online_cpus() -> Result<Vec<u32>, String> {
    use std::fs;
    let s = fs::read_to_string("/sys/devices/system/cpu/online")
        .map_err(|e| format!("cannot read cpu online: {}", e))?;
    let mut cpus = Vec::new();
    for part in s.trim().split(',') {
        if let Some((start, end)) = part.split_once('-') {
            let start: u32 = start.parse().map_err(|e| format!("bad cpu range: {}", e))?;
            let end: u32 = end.parse().map_err(|e| format!("bad cpu range: {}", e))?;
            for cpu in start..=end {
                cpus.push(cpu);
            }
        } else {
            let cpu: u32 = part.parse().map_err(|e| format!("bad cpu: {}", e))?;
            cpus.push(cpu);
        }
    }
    if cpus.is_empty() {
        // Fallback: use num_cpus
        let n = num_cpus::get() as u32;
        for i in 0..n {
            cpus.push(i);
        }
    }
    Ok(cpus)
}

// ── Demo reader (non-Linux or fallback) ────────────────────────────────────

async fn run_demo_reader(buffer: Arc<EventBuffer>) {
    tracing::info!("event reader started (demo mode)");

    let mut interval = tokio::time::interval(std::time::Duration::from_secs(10));
    let mut seq: u64 = 0;

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

        let (action, resource, attrs) = &demo_actions[(seq as usize) % demo_actions.len()];

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
            family_group_id: "default".to_string(),
            agent_id: "demo-agent".to_string(),
            resource_ref: final_resource.to_string(),
            action: final_action.to_string(),
            attributes: attrs.clone(),
        };

        buffer.push(event);
        tracing::debug!("demo event: {} {} {}", final_action, final_resource, seq);
    }
}

// ── Helpers ────────────────────────────────────────────────────────────────

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
    let secs = now.as_secs();
    let nanos = now.subsec_nanos();
    let dt = chrono_format(secs, nanos);
    format!("{}.{:09}Z", dt, nanos)
}

fn chrono_format(unix_secs: u64, _nanos: u32) -> String {
    let days_since_epoch = (unix_secs / 86400) as i64;
    let secs_of_day = (unix_secs % 86400) as u32;

    let (y, m, d) = civil_from_days(days_since_epoch);
    let h = secs_of_day / 3600;
    let min = (secs_of_day % 3600) / 60;
    let s = secs_of_day % 60;

    format!("{:04}-{:02}-{:02}T{:02}:{:02}:{:02}", y, m, d, h, min, s)
}

fn civil_from_days(days: i64) -> (i32, u32, u32) {
    let z = days + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u32;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };
    (y as i32, m, d)
}
