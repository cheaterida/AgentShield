use agent_runtime::client::{AuditEventPayload, ManagementClient};
use std::collections::HashMap;

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
async fn test_e2e_register_heartbeat_upload() {
    let mut server = mockito::Server::new_async().await;

    // Mock register endpoint
    let register_mock = server
        .mock("POST", "/api/v1/agents/register")
        .with_status(200)
        .with_body(r#"{"id":"agent-1"}"#)
        .create();

    // Mock heartbeat endpoint
    let heartbeat_mock = server
        .mock("POST", "/api/v1/agents/heartbeat")
        .with_status(200)
        .with_body(
            r#"{"acknowledged":true,"latest_policy_version":"v1","suggested_action":"ok"}"#,
        )
        .create();

    // Mock upload events endpoint
    let upload_mock = server
        .mock("POST", "/api/v1/audit/events")
        .with_status(200)
        .with_body(r#"{"accepted":2}"#)
        .create();

    let client = ManagementClient::new(&server.url(), "agent-1", "fg-1", "test-agent");

    // Step 1: Register
    client.register().await.unwrap();
    register_mock.assert();

    // Step 2: Heartbeat
    let hb = client.heartbeat(5.0, 2048, 2, "v0", 10).await.unwrap();
    assert!(hb.acknowledged);
    heartbeat_mock.assert();

    // Step 3: Upload events
    let events = vec![make_event("e1"), make_event("e2")];
    let accepted = client.upload_events(&events).await.unwrap();
    assert_eq!(accepted, 2);
    upload_mock.assert();
}
