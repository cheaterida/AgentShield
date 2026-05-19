//! AgentShield 员工端运行时入口。

mod client;
mod config;
mod event_buffer;
mod event_upload;
mod heartbeat;
mod policy_cache;
mod probe_event_conv;
mod probe_manager;
mod supervisor;

use std::net::{TcpStream, ToSocketAddrs};
use std::sync::Arc;
use std::time::Duration;
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::util::SubscriberInitExt;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::registry()
        .with(tracing_subscriber::EnvFilter::from_default_env())
        .with(tracing_subscriber::fmt::layer())
        .init();

    let cfg = config::Config::from_env()?;

    // ── 启动诊断 ──
    let (mgmt_host, mgmt_port) = client::host_port_from_url(&cfg.management_addr);
    tracing::info!(
        agent_id = %cfg.agent_id,
        family_group = %cfg.family_group_id,
        mgmt_host = %mgmt_host,
        mgmt_port = mgmt_port,
        mgmt_full = %cfg.management_addr,
        probe_enabled = cfg.probe_enabled,
        "=== AgentShield agent-runtime starting ==="
    );

    // DNS 诊断
    let dns_target = format!("{}:{}", mgmt_host, mgmt_port);
    match dns_target.to_socket_addrs() {
        Ok(addrs) => {
            let addr_list: Vec<_> = addrs.collect();
            if addr_list.is_empty() {
                tracing::error!(
                    target = %dns_target,
                    "DNS ZERO results — /etc/resolv.conf may be broken"
                );
            } else {
                tracing::info!(
                    target = %dns_target,
                    resolved = ?addr_list,
                    "DNS resolution OK"
                );
            }
        }
        Err(e) => {
            tracing::error!(
                target = %dns_target,
                error = %e,
                "DNS resolution FAILED"
            );
        }
    }

    // TCP 连通性诊断（只测第一个可用地址）
    let tcp_target = format!("{}:{}", mgmt_host, mgmt_port);
    match TcpStream::connect_timeout(
        &format!("{}:{}", mgmt_host, mgmt_port)
            .as_str()
            .parse()
            .unwrap_or_else(|_| "127.0.0.1:1".parse().unwrap()),
        Duration::from_secs(5),
    ) {
        Ok(stream) => {
            let peer = stream.peer_addr().unwrap_or_else(|_| "unknown".parse().unwrap());
            tracing::info!(
                target = %tcp_target,
                peer = %peer,
                "TCP handshake SUCCESS — network reachable"
            );
        }
        Err(e) => {
            tracing::error!(
                target = %tcp_target,
                error = %e,
                "TCP handshake FAILED — port unreachable or firewall DROP"
            );
        }
    }

    // ── 1. 连接 management-server ──
    let client = client::ManagementClient::new(
        &cfg.management_addr,
        &cfg.agent_id,
        &cfg.family_group_id,
        &cfg.display_name,
    );

    // 注册（带重试）
    let mut registered = false;
    let mut last_error = String::new();
    for attempt in 1..=10 {
        match client.register().await {
            Ok(()) => {
                registered = true;
                break;
            }
            Err(e) => {
                last_error = e.clone();
                let delay = Duration::from_secs(2u64.pow(attempt.min(6)));
                tracing::warn!(
                    attempt,
                    next_delay_ms = delay.as_millis(),
                    error = %e,
                    "register failed, retrying..."
                );
                tokio::time::sleep(delay).await;
            }
        }
    }
    if !registered {
        tracing::error!(
            last_error = %last_error,
            "FAILED after 10 registration attempts — giving up"
        );
        return Err("registration failed".into());
    }

    // ── 2. 事件缓冲 ──
    let event_buffer = Arc::new(event_buffer::EventBuffer::new(10_000));

    // ── 3. eBPF 探针管理 ──
    let probe_manager = Arc::new(probe_manager::ProbeManager::new(&cfg.agent_id, &cfg.family_group_id));
    if cfg.probe_enabled {
        if let Err(e) = probe_manager.load_probes() {
            tracing::warn!(error = %e, "probe load failed (non-fatal)");
        }
    } else {
        tracing::info!("probes disabled — event source is external (Langtrace Bridge)");
    }

    // ── 4. 事件读取（仅探针启用时启动） ──
    let reader_handle: Option<tokio::task::JoinHandle<()>> = if cfg.probe_enabled {
        let pm = probe_manager.clone();
        let buf = event_buffer.clone();
        Some(pm.start_event_reader(buf).await)
    } else {
        None
    };

    // ── 5. 心跳任务 ──
    let policy_cache = Arc::new(policy_cache::PolicyCache::new(&cfg.policy_cache_dir));
    tracing::info!(
        policy_dir = %cfg.policy_cache_dir,
        version = policy_cache.current_version(),
        "policy cache ready"
    );

    let hb_task = heartbeat::HeartbeatTask::new(
        client.clone(),
        cfg.heartbeat_interval_secs,
        probe_manager.clone(),
        event_buffer.clone(),
        policy_cache.clone(),
    );
    let hb_handle = tokio::spawn(hb_task.run());

    // ── 6. 事件上传任务 ──
    let upload_task = event_upload::EventUploadTask::new(
        client.clone(),
        event_buffer.clone(),
        cfg.event_batch_size,
    );
    let upload_handle = tokio::spawn(upload_task.run(cfg.event_upload_interval_secs));

    // ── 7. Hermes Agent 监管（可选） ──
    let supervisor = supervisor::Supervisor::new();
    if let Some(ref hermes_path) = cfg.hermes_binary_path {
        match supervisor.start(hermes_path) {
            Ok(()) => tracing::info!("hermes agent supervised"),
            Err(e) => tracing::warn!(error = %e, "hermes start failed"),
        }
    }

    tracing::info!("agent-runtime running — press Ctrl+C to stop");

    // ── 9. 等待终止信号 ──
    tokio::signal::ctrl_c().await?;
    tracing::info!("agent-runtime shutting down");

    hb_handle.abort();
    upload_handle.abort();
    if let Some(h) = reader_handle {
        h.abort();
    }
    probe_manager.unload_all();
    drop(supervisor); // kills hermes

    tracing::info!("agent-runtime stopped");
    Ok(())
}
