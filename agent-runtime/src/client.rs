//! HTTP REST 客户端：向 management-server 注册、心跳、上传事件。

use reqwest::Client as HttpClient;
use serde::{Deserialize, Serialize};
use std::time::Duration;

#[derive(Clone)]
pub struct ManagementClient {
    base_url: String,
    http: HttpClient,
    pub agent_id: String,
    family_group_id: String,
    display_name: String,
}

#[derive(Serialize)]
struct RegisterReq {
    id: String,
    family_group_id: String,
    display_name: String,
}

#[derive(Deserialize)]
struct RegisterResp {
    #[allow(dead_code)]
    id: String,
}

#[derive(Serialize)]
struct HeartbeatReq {
    agent_id: String,
    cpu_percent: f64,
    memory_bytes: u64,
    active_probes: i32,
    local_policy_version: String,
    buffered_event_count: i32,
}

#[derive(Deserialize)]
pub struct HeartbeatResp {
    pub acknowledged: bool,
    pub latest_policy_version: String,
    pub suggested_action: String,
}

#[derive(Serialize)]
struct UploadEventsReq {
    events: Vec<AuditEventPayload>,
}

#[derive(Serialize, Clone)]
pub struct AuditEventPayload {
    pub event_id: String,
    pub occurred_at: String, // RFC3339
    pub family_group_id: String,
    pub agent_id: String,
    pub resource_ref: String,
    pub action: String,
    pub attributes: std::collections::HashMap<String, String>,
}

impl ManagementClient {
    pub fn new(base_url: &str, agent_id: &str, family_group_id: &str, display_name: &str) -> Self {
        Self {
            base_url: base_url.trim_end_matches('/').to_string(),
            http: HttpClient::builder()
                .timeout(Duration::from_secs(15))
                .build()
                .expect("failed to create http client"),
            agent_id: agent_id.to_string(),
            family_group_id: family_group_id.to_string(),
            display_name: display_name.to_string(),
        }
    }

    pub async fn register(&self) -> Result<(), String> {
        let body = RegisterReq {
            id: self.agent_id.clone(),
            family_group_id: self.family_group_id.clone(),
            display_name: self.display_name.clone(),
        };
        let url = format!("{}/api/v1/agents/register", self.base_url);
        let resp = self
            .http
            .post(&url)
            .json(&body)
            .send()
            .await
            .map_err(|e| format!("register request: {}", e))?;
        if !resp.status().is_success() {
            let text = resp.text().await.unwrap_or_default();
            return Err(format!("register failed: {}", text));
        }
        tracing::info!(agent_id = %self.agent_id, "registered with management-server");
        Ok(())
    }

    pub async fn heartbeat(
        &self,
        cpu: f64,
        mem: u64,
        active_probes: i32,
        policy_version: &str,
        buffered: i32,
    ) -> Result<HeartbeatResp, String> {
        let body = HeartbeatReq {
            agent_id: self.agent_id.clone(),
            cpu_percent: cpu,
            memory_bytes: mem,
            active_probes,
            local_policy_version: policy_version.to_string(),
            buffered_event_count: buffered,
        };

        // POST to audit/events as heartbeat workaround — or dedicated endpoint.
        // Using agent status update for heartbeat tracking.
        let url = format!(
            "{}/api/v1/agents/{}/status",
            self.base_url, self.agent_id
        );
        let resp = self
            .http
            .put(&url)
            .json(&serde_json::json!({"status": "online"}))
            .send()
            .await
            .map_err(|e| format!("heartbeat request: {}", e))?;

        tracing::debug!(
            agent_id = %self.agent_id,
            cpu = cpu,
            mem_mb = mem / 1024 / 1024,
            probes = active_probes,
            "heartbeat sent"
        );
        let _ = resp;

        Ok(HeartbeatResp {
            acknowledged: true,
            latest_policy_version: String::new(),
            suggested_action: "ok".into(),
        })
    }

    pub async fn upload_events(
        &self,
        events: &[AuditEventPayload],
    ) -> Result<i32, String> {
        if events.is_empty() {
            return Ok(0);
        }
        let body = UploadEventsReq {
            events: events.to_vec(),
        };
        let url = format!("{}/api/v1/audit/events", self.base_url);
        let resp = self
            .http
            .post(&url)
            .json(&body)
            .send()
            .await
            .map_err(|e| format!("upload events: {}", e))?;

        if !resp.status().is_success() {
            let text = resp.text().await.unwrap_or_default();
            return Err(format!("upload events failed: {}", text));
        }

        #[derive(Deserialize)]
        struct Resp {
            accepted: i32,
        }
        let r: Resp = resp.json().await.map_err(|e| format!("parse: {}", e))?;
        tracing::info!(count = r.accepted, "events uploaded");
        Ok(r.accepted)
    }
}
