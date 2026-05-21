//! 从环境变量加载 agent-runtime 配置。

use std::env;

#[derive(Clone, Debug)]
pub struct Config {
    pub agent_id: String,
    pub family_group_id: String,
    pub display_name: String,
    pub management_addr: String,
    pub heartbeat_interval_secs: u64,
    pub event_batch_size: usize,
    pub event_upload_interval_secs: u64,
    pub policy_cache_dir: String,
    pub hermes_binary_path: Option<String>,
    pub probe_enabled: bool,
    pub probe_comm_allowlist: Vec<String>,
    #[cfg(feature = "checkpoint")]
    pub checkpoint_enabled: bool,
    #[cfg(feature = "checkpoint")]
    pub checkpoint_dir: String,
    #[cfg(feature = "checkpoint")]
    pub checkpoint_max_count: usize,
    #[cfg(feature = "checkpoint")]
    pub workspace_dir: String,
    #[cfg(feature = "checkpoint")]
    pub checkpoint_interval_steps: u64,
}

impl Config {
    pub fn from_env() -> Result<Self, String> {
        Ok(Config {
            agent_id: getenv("AGENTSHIELD_AGENT_ID")?,
            family_group_id: getenv("AGENTSHIELD_FAMILY_GROUP_ID")?,
            display_name: getenv_opt("AGENTSHIELD_DISPLAY_NAME").unwrap_or_default(),
            management_addr: getenv_opt("AGENTSHIELD_MGMT_ADDR")
                .unwrap_or_else(|| "http://localhost:8080".into()),
            heartbeat_interval_secs: getenv_opt("AGENTSHIELD_HEARTBEAT_INTERVAL_SECS")
                .and_then(|s| s.parse().ok())
                .unwrap_or(10),
            event_batch_size: getenv_opt("AGENTSHIELD_EVENT_BATCH_SIZE")
                .and_then(|s| s.parse().ok())
                .unwrap_or(100),
            event_upload_interval_secs: getenv_opt("AGENTSHIELD_EVENT_UPLOAD_INTERVAL_SECS")
                .and_then(|s| s.parse().ok())
                .unwrap_or(30),
            policy_cache_dir: getenv_opt("AGENTSHIELD_POLICY_CACHE_DIR")
                .unwrap_or_else(|| "/var/lib/agentshield/policies".into()),
            hermes_binary_path: getenv_opt("AGENTSHIELD_HERMES_BINARY"),
            probe_enabled: getenv_opt("AGENTSHIELD_PROBE_ENABLED")
                .map(|s| s == "true" || s == "1")
                .unwrap_or(true),
            probe_comm_allowlist: getenv_opt("AGENTSHIELD_PROBE_COMM_ALLOWLIST")
                .map(|s| s.split(',').map(|s| s.trim().to_string()).collect())
                .unwrap_or_default(),
            #[cfg(feature = "checkpoint")]
            checkpoint_enabled: getenv_opt("AGENTSHIELD_CHECKPOINT_ENABLED")
                .map(|s| s == "true" || s == "1")
                .unwrap_or(true),
            #[cfg(feature = "checkpoint")]
            checkpoint_dir: getenv_opt("AGENTSHIELD_CHECKPOINT_DIR")
                .unwrap_or_else(|| "/var/lib/agentshield/checkpoints".into()),
            #[cfg(feature = "checkpoint")]
            checkpoint_max_count: getenv_opt("AGENTSHIELD_CHECKPOINT_MAX_COUNT")
                .and_then(|s| s.parse().ok())
                .unwrap_or(50),
            #[cfg(feature = "checkpoint")]
            workspace_dir: getenv_opt("AGENTSHIELD_WORKSPACE_DIR")
                .unwrap_or_else(|| "/workspace".into()),
            #[cfg(feature = "checkpoint")]
            checkpoint_interval_steps: getenv_opt("AGENTSHIELD_CHECKPOINT_INTERVAL_STEPS")
                .and_then(|s| s.parse().ok())
                .unwrap_or(1),
        })
    }
}

fn getenv(key: &str) -> Result<String, String> {
    env::var(key).map_err(|_| format!("missing env: {}", key))
}

fn getenv_opt(key: &str) -> Option<String> {
    env::var(key).ok().filter(|s| !s.is_empty())
}
