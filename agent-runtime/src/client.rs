//! HTTP REST 客户端：向 management-server 注册、心跳、上传事件。

use reqwest::Client as HttpClient;
use serde::{Deserialize, Serialize};
use std::net::ToSocketAddrs;
use std::time::Duration;

#[derive(Clone)]
pub struct ManagementClient {
    base_url: String,
    http: HttpClient,
    pub agent_id: String,
    family_group_id: String,
    display_name: String,
}

pub(crate) fn host_port_from_url(url: &str) -> (&str, u16) {
    let s = url
        .trim_start_matches("http://")
        .trim_start_matches("https://");
    match s.split_once(':') {
        Some((host, port)) => (host, port.parse().unwrap_or(80)),
        None => (s, 80),
    }
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
        let base = base_url.trim_end_matches('/').to_string();

        // ── 调试：DNS 预检 ──
        let (host, port) = host_port_from_url(&base);
        tracing::info!(
            mgmt_host = %host,
            mgmt_port = port,
            mgmt_url = %base,
            agent_id = %agent_id,
            "ManagementClient init — performing DNS pre-check"
        );
        match format!("{}:{}", host, port).to_socket_addrs() {
            Ok(addrs) => {
                let addr_list: Vec<_> = addrs.collect();
                if addr_list.is_empty() {
                    tracing::error!(
                        host = %host, port = port,
                        "DNS resolved but returned ZERO addresses — network unreachable"
                    );
                } else {
                    tracing::info!(
                        host = %host, port = port,
                        resolved = ?addr_list,
                        "DNS resolution OK — {} address(es)", addr_list.len()
                    );
                }
            }
            Err(e) => {
                tracing::error!(
                    host = %host, port = port,
                    error = %e,
                    "DNS resolution FAILED — check network or /etc/resolv.conf"
                );
            }
        }

        Self {
            base_url: base,
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

        tracing::info!(url = %url, agent_id = %self.agent_id, "attempting register POST");

        let resp = match self.http.post(&url).json(&body).send().await {
            Ok(r) => r,
            Err(e) => {
                // ── 调试：区分网络错误类型 ──
                let detail = format!("{:?}", e);
                let hint = if e.is_timeout() {
                    "TIMEOUT — TCP SYN 可能被防火墙丢弃，或目标不可达"
                } else if e.is_connect() {
                    "CONNECT FAILED — TCP 连接被拒绝或端口未监听"
                } else if e.is_request() {
                    "REQUEST FAILED — 请求已发出但未收到响应"
                } else if detail.contains("dns") || detail.contains("resolve") {
                    "DNS FAILED — 域名解析失败，检查 /etc/resolv.conf"
                } else {
                    "UNKNOWN — 见上方错误详情"
                };
                tracing::error!(
                    url = %url,
                    error = %e,
                    error_debug = %detail,
                    hint = hint,
                    "register POST failed"
                );
                return Err(format!("register request: {} ({})", e, hint));
            }
        };

        let status = resp.status();
        tracing::info!(
            url = %url,
            status = %status,
            status_code = status.as_u16(),
            "register response received"
        );

        if !status.is_success() {
            let text = resp.text().await.unwrap_or_default();
            tracing::error!(
                status = %status,
                body = %text,
                "register returned non-success"
            );
            return Err(format!("register failed: {} — {}", status, text));
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
        let url = format!("{}/api/v1/agents/heartbeat", self.base_url);

        let body = HeartbeatReq {
            agent_id: self.agent_id.clone(),
            cpu_percent: cpu,
            memory_bytes: mem,
            active_probes,
            local_policy_version: policy_version.to_string(),
            buffered_event_count: buffered,
        };

        let resp = self
            .http
            .post(&url)
            .json(&body)
            .send()
            .await
            .map_err(|e| {
                tracing::error!(
                    url = %url,
                    error = %e,
                    "heartbeat request failed"
                );
                format!("heartbeat request: {}", e)
            })?;

        if !resp.status().is_success() {
            let status = resp.status();
            let text = resp.text().await.unwrap_or_default();
            tracing::warn!(
                url = %url,
                status = %status,
                body = %text,
                "heartbeat returned non-success"
            );
            return Ok(HeartbeatResp {
                acknowledged: false,
                latest_policy_version: String::new(),
                suggested_action: "ok".into(),
            });
        }

        let parsed: HeartbeatResp = resp
            .json()
            .await
            .map_err(|e| format!("heartbeat parse response: {}", e))?;

        tracing::debug!(
            ack = parsed.acknowledged,
            action = %parsed.suggested_action,
            policy_ver = %parsed.latest_policy_version,
            "heartbeat response"
        );

        Ok(parsed)
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
