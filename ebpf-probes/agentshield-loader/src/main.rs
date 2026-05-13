//! eBPF probe loader — loads agentshield-ebpf bytecode and attaches to kernel
//! tracepoints. Reads events via perf buffer and logs them for the agent-runtime.
//!
//! Production deployment requires:
//!   - Linux kernel 5.8+ with BPF enabled
//!   - CAP_BPF + CAP_PERFMON (or CAP_SYS_ADMIN / privileged)
//!   - Compiled eBPF bytecode at a known path

use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use aya::maps::perf::PerfEventArray;
use aya::programs::TracePoint;
use aya::{Bpf, include_bytes_aligned};
use tokio::sync::mpsc;
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

use agentshield_ebpf_common::ProbeEvent;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::registry()
        .with(tracing_subscriber::EnvFilter::from_default_env())
        .with(tracing_subscriber::fmt::layer())
        .init();

    tracing::info!("AgentShield eBPF loader starting");

    // Try to load embedded bytecode; fall back gracefully
    let mut bpf = match load_bpf() {
        Ok(b) => {
            tracing::info!("eBPF bytecode loaded successfully");
            b
        }
        Err(e) => {
            tracing::warn!("Failed to load eBPF bytecode (non-fatal): {}", e);
            tracing::info!("Running in demo mode — no kernel probes attached");
            run_demo_mode().await;
            return Ok(());
        }
    };

    // Attach tracepoints
    let tracepoints = [
        ("syscalls", "sys_enter_openat", "agentshield_syscall_enter_openat"),
        ("syscalls", "sys_enter_execve", "agentshield_syscall_enter_execve"),
        ("syscalls", "sys_enter_connect", "agentshield_syscall_enter_connect"),
        ("syscalls", "sys_enter_bind", "agentshield_syscall_enter_bind"),
    ];

    let mut attached = 0;
    for (category, name, prog_name) in &tracepoints {
        match attach_tracepoint(&mut bpf, category, name, prog_name) {
            Ok(()) => {
                tracing::info!("Attached tracepoint: {}/{}", category, name);
                attached += 1;
            }
            Err(e) => {
                tracing::warn!("Failed to attach {}/{}: {}", category, name, e);
            }
        }
    }
    tracing::info!("Attached {}/{} tracepoints", attached, tracepoints.len());

    if attached == 0 {
        tracing::info!("No tracepoints attached — running demo mode");
        run_demo_mode().await;
        return Ok(());
    }

    // Read from perf event array
    let event_count = Arc::new(AtomicU64::new(0));
    let (tx, mut rx) = mpsc::channel::<ProbeEvent>(1024);

    // Spawn perf reader tasks
    if let Ok(events) = PerfEventArray::<ProbeEvent>::try_from(bpf.map_mut("EVENTS")?) {
        let count = event_count.clone();
        let tx_clone = tx.clone();
        tokio::task::spawn_blocking(move || {
            let mut buf = PerfEventArray::<ProbeEvent>::default();
            loop {
                let events_read = events.read_events(&mut buf)?;
                for event in buf.iter() {
                    count.fetch_add(1, Ordering::Relaxed);
                    let _ = tx_clone.blocking_send(*event);
                    tracing::debug!(
                        "eBPF event: pid={} syscall={} file={}",
                        event.pid,
                        event.syscall_str(),
                        event.filename_str(),
                    );
                }
            }
        });
    }

    // Print periodic stats
    let stats_count = event_count.clone();
    tokio::spawn(async move {
        loop {
            tokio::time::sleep(std::time::Duration::from_secs(30)).await;
            let c = stats_count.load(Ordering::Relaxed);
            tracing::info!("eBPF events captured: {}", c);
        }
    });

    // Keep running, forwarding events
    tracing::info!("eBPF loader running — press Ctrl+C to stop");
    tokio::signal::ctrl_c().await?;
    tracing::info!("Shutting down eBPF loader");

    Ok(())
}

fn load_bpf() -> Result<Bpf, Box<dyn std::error::Error>> {
    // Try embedded bytecode first (requires cargo build with bpf-linker)
    #[cfg(target_os = "linux")]
    {
        let bytes = include_bytes_aligned!("../../agentshield-ebpf/target/bpfel-unknown-none/release/agentshield-ebpf");
        tracing::info!("Loading embedded eBPF bytecode ({} bytes)", bytes.len());
        let mut bpf = Bpf::load(bytes)?;
        Ok(bpf)
    }

    #[cfg(not(target_os = "linux"))]
    {
        Err("eBPF loading not supported on this platform".into())
    }
}

fn attach_tracepoint(
    bpf: &mut Bpf,
    category: &str,
    name: &str,
    prog_name: &str,
) -> Result<(), Box<dyn std::error::Error>> {
    let program: &mut TracePoint = bpf.program_mut(prog_name)?
        .try_into()?;
    program.load()?;
    program.attach(category, name)?;
    Ok(())
}

async fn run_demo_mode() {
    tracing::info!("Demo mode: generating synthetic events every 30s");
    loop {
        tokio::time::sleep(std::time::Duration::from_secs(30)).await;
        tracing::debug!("Demo event: read /data/example.txt by agent-001");
    }
}
