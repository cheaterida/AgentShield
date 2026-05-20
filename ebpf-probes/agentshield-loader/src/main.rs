//! eBPF probe loader — loads agentshield-ebpf bytecode and attaches to kernel
//! tracepoints. Reads events via perf buffer and logs them for the agent-runtime.

use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use aya::Ebpf;
use aya::maps::perf::AsyncPerfEventArray;
use aya::programs::TracePoint;
use aya::include_bytes_aligned;
use aya::util::online_cpus;
use bytes::BytesMut;
use tokio::sync::mpsc;
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

use agentshield_ebpf_common::ProbeEvent;

const PERF_BUF_PAGES: usize = 16;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::registry()
        .with(tracing_subscriber::EnvFilter::from_default_env())
        .with(tracing_subscriber::fmt::layer())
        .init();

    tracing::info!("AgentShield eBPF loader starting");

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
        ("syscalls", "sys_enter_openat", "agentshield_sys_enter_openat"),
        ("syscalls", "sys_enter_execve", "agentshield_sys_enter_execve"),
        ("syscalls", "sys_enter_connect", "agentshield_sys_enter_connect"),
        ("syscalls", "sys_enter_bind", "agentshield_sys_enter_bind"),
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

    // Open perf event array for reading events
    let perf_map = match bpf.take_map("EVENTS") {
        Some(m) => m,
        None => {
            tracing::warn!("EVENTS map not found, running demo mode");
            run_demo_mode().await;
            return Ok(());
        }
    };

    let mut perf_array = AsyncPerfEventArray::try_from(perf_map)?;
    let event_count = Arc::new(AtomicU64::new(0));
    let (tx, mut rx) = mpsc::channel::<ProbeEvent>(1024);

    // Spawn perf reader per CPU
    for cpu_id in online_cpus().map_err(|(_, e)| e)? {
        let mut buf = perf_array.open(cpu_id, Some(PERF_BUF_PAGES))?;
        let count = event_count.clone();
        let tx = tx.clone();

        tokio::spawn(async move {
            let mut bufs = vec![BytesMut::with_capacity(std::mem::size_of::<ProbeEvent>())];
            loop {
                match buf.read_events(&mut bufs).await {
                    Ok(events) => {
                        let available = bufs[0].len() / std::mem::size_of::<ProbeEvent>();
                        for _ in 0..available {
                            // discard — loader is test-only, events are not processed here
                        }
                        count.fetch_add(events.read as u64, Ordering::Relaxed);
                        // Forward raw bytes — in production, deserialize properly
                        if !bufs[0].is_empty() {
                            let bytes = bufs[0].split();
                            if bytes.len() >= std::mem::size_of::<ProbeEvent>() {
                                let event: ProbeEvent = unsafe {
                                    std::ptr::read_unaligned(bytes.as_ptr() as *const ProbeEvent)
                                };
                                let _ = tx.send(event).await;
                            }
                        }
                        bufs[0].clear();
                    }
                    Err(e) => {
                        tracing::error!("Perf buffer read error: {}", e);
                    }
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

    // Forward events to agent-runtime (placeholder)
    tokio::spawn(async move {
        while let Some(event) = rx.recv().await {
            tracing::debug!(
                "eBPF event: pid={} syscall={} file={}",
                event.pid,
                event.syscall_str(),
                event.filename_str(),
            );
        }
    });

    tracing::info!("eBPF loader running — press Ctrl+C to stop");
    tokio::signal::ctrl_c().await?;
    tracing::info!("Shutting down eBPF loader");

    Ok(())
}

fn load_bpf() -> Result<Ebpf, Box<dyn std::error::Error>> {
    #[cfg(target_os = "linux")]
    {
        let bytes = include_bytes_aligned!("../../agentshield-ebpf/target/bpfel-unknown-none/debug/agentshield-ebpf");
        tracing::info!("Loading embedded eBPF bytecode ({} bytes)", bytes.len());
        Ok(Ebpf::load(bytes)?)
    }

    #[cfg(not(target_os = "linux"))]
    {
        Err("eBPF loading not supported on this platform".into())
    }
}

fn attach_tracepoint(
    bpf: &mut Ebpf,
    category: &str,
    name: &str,
    prog_name: &str,
) -> Result<(), Box<dyn std::error::Error>> {
    let program: &mut TracePoint = bpf
        .program_mut(prog_name)
        .ok_or_else(|| format!("program not found: {}", prog_name))?
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
