use agent_runtime::client::{AuditEventPayload, ManagementClient};
use std::collections::HashMap;

fn make_client(server: &mockito::ServerGuard) -> ManagementClient {
    ManagementClient::new(&server.url(), "agent-1", "fg-1", "test-agent")
}

fn make_event(id: &str) -> AuditEventPayload {
    AuditEventPayload {
        event_id: id.to_string(),
        occurred_at: "2024-01-01T00:00:00Z".to_string(),
        family_group_id: "fg-1".to_string(),
        agent_id: "agent-1".to_string(),
        resource_ref: "/tmp/test".to_string(),
        action: "read".to_string(),
        attributes: HashMap::new(),
    }
}

#[tokio::test]
async fn test_register_ok() {
    let mut server = mockito::Server::new_async().await;
    let mock = server
        .mock("POST", "/api/v1/agents/register")
        .with_status(200)
        .with_body(r#"{"id":"agent-1"}"#)
        .create();

    let client = make_client(&server);
    let result = client.register().await;
    mock.assert();
    assert!(result.is_ok());
}

#[tokio::test]
async fn test_register_fails_on_503() {
    let mut server = mockito::Server::new_async().await;
    let mock = server
        .mock("POST", "/api/v1/agents/register")
        .with_status(503)
        .with_body("Service Unavailable")
        .create();

    let client = make_client(&server);
    let result = client.register().await;
    mock.assert();
    assert!(result.is_err());
}

#[tokio::test]
async fn test_heartbeat_parses_response() {
    let mut server = mockito::Server::new_async().await;
    let mock = server
        .mock("POST", "/api/v1/agents/heartbeat")
        .with_status(200)
        .with_body(
            r#"{"acknowledged":true,"latest_policy_version":"v2","suggested_action":"ok"}"#,
        )
        .create();

    let client = make_client(&server);
    let result = client.heartbeat(10.0, 1024, 1, "v1", 5).await;
    mock.assert();
    let resp = result.unwrap();
    assert!(resp.acknowledged);
    assert_eq!(resp.latest_policy_version, "v2");
    assert_eq!(resp.suggested_action, "ok");
}

#[tokio::test]
async fn test_heartbeat_non_success_returns_default() {
    let mut server = mockito::Server::new_async().await;
    let mock = server
        .mock("POST", "/api/v1/agents/heartbeat")
        .with_status(500)
        .create();

    let client = make_client(&server);
    let result = client.heartbeat(0.0, 0, 0, "v0", 0).await;
    mock.assert();
    let resp = result.unwrap();
    assert!(!resp.acknowledged);
    assert!(resp.latest_policy_version.is_empty());
    assert_eq!(resp.suggested_action, "ok");
}

#[tokio::test]
async fn test_upload_events_returns_accepted_count() {
    let mut server = mockito::Server::new_async().await;
    let mock = server
        .mock("POST", "/api/v1/audit/events")
        .with_status(200)
        .with_body(r#"{"accepted":3}"#)
        .create();

    let client = make_client(&server);
    let events = vec![make_event("e1"), make_event("e2"), make_event("e3")];
    let result = client.upload_events(&events).await;
    mock.assert();
    assert_eq!(result.unwrap(), 3);
}

#[tokio::test]
async fn test_upload_events_empty_batch() {
    let server = mockito::Server::new_async().await;
    let client = make_client(&server);
    let result = client.upload_events(&[]).await;
    assert_eq!(result.unwrap(), 0);
}
