//! AgentShield 员工端运行时入口。
//!
//! 职责：
//! - 静默驻留，向 management-server 注册并维持心跳
//! - 加载 eBPF 探针、采集审计事件并批量上报
//! - 缓存并同步管理端下发的策略
//! - 可选：监管 Hermes AI Agent 子进程

mod client;
mod config;
mod event_buffer;
mod event_upload;
mod heartbeat;
mod policy_cache;
mod probe_manager;
mod supervisor;

use std::sync::Arc;
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::util::SubscriberInitExt;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::registry()
        .with(tracing_subscriber::EnvFilter::from_default_env())
        .with(tracing_subscriber::fmt::layer())
        .init();

    let cfg = config::Config::from_env()?;
    tracing::info!(
        agent_id = %cfg.agent_id,
        family_group = %cfg.family_group_id,
        mgmt = %cfg.management_addr,
        "agent-runtime starting"
    );

    // ── 1. 连接 management-server ──
    let client = client::ManagementClient::new(
        &cfg.management_addr,
        &cfg.agent_id,
        &cfg.family_group_id,
        &cfg.display_name,
    );

    // 注册（带重试）
    let mut registered = false;
    for attempt in 1..=10 {
        match client.register().await {
            Ok(()) => {
                registered = true;
                break;
            }
            Err(e) => {
                tracing::warn!(attempt, error = %e, "register failed, retrying...");
                tokio::time::sleep(std::time::Duration::from_secs(2u64.pow(attempt))).await;
            }
        }
    }
    if !registered {
        tracing::error!("failed to register after 10 attempts");
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
    }

    // ── 4. 事件读取 ──
    let reader_handle = {
        let pm = probe_manager.clone();
        let buf = event_buffer.clone();
        pm.start_event_reader(buf).await
    };

    // ── 5. 心跳任务 ──
    let hb_task = heartbeat::HeartbeatTask::new(
        client.clone(),
        cfg.heartbeat_interval_secs,
        probe_manager.clone(),
        event_buffer.clone(),
    );
    let hb_handle = tokio::spawn(hb_task.run());

    // ── 6. 事件上传任务 ──
    let upload_task = event_upload::EventUploadTask::new(
        client.clone(),
        event_buffer.clone(),
        cfg.event_batch_size,
    );
    let upload_handle = tokio::spawn(upload_task.run(cfg.event_upload_interval_secs));

    // ── 7. 策略缓存 ──
    let policy_cache = policy_cache::PolicyCache::new(&cfg.policy_cache_dir);
    tracing::info!(
        policy_dir = %cfg.policy_cache_dir,
        version = policy_cache.current_version(),
        "policy cache ready"
    );

    // ── 8. Hermes Agent 监管（可选） ──
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
    reader_handle.abort();
    probe_manager.unload_all();
    drop(supervisor); // kills hermes

    tracing::info!("agent-runtime stopped");
    Ok(())
}
