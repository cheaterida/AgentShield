use agent_runtime::client::AuditEventPayload;
use agent_runtime::event_buffer::EventBuffer;
use std::collections::HashMap;
use std::sync::Arc;

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
async fn test_concurrent_push_drain() {
    let buf = Arc::new(EventBuffer::new(1000));

    let push1 = {
        let buf = buf.clone();
        tokio::spawn(async move {
            for i in 0..100 {
                buf.push(make_event(&format!("a-{}", i)));
            }
        })
    };

    let push2 = {
        let buf = buf.clone();
        tokio::spawn(async move {
            for i in 0..100 {
                buf.push(make_event(&format!("b-{}", i)));
            }
        })
    };

    let drain1 = {
        let buf = buf.clone();
        tokio::spawn(async move {
            let mut total = 0;
            for _ in 0..10 {
                let batch = buf.drain(30);
                total += batch.len();
                tokio::task::yield_now().await;
            }
            total
        })
    };

    let drain2 = {
        let buf = buf.clone();
        tokio::spawn(async move {
            let mut total = 0;
            for _ in 0..10 {
                let batch = buf.drain(30);
                total += batch.len();
                tokio::task::yield_now().await;
            }
            total
        })
    };

    push1.await.unwrap();
    push2.await.unwrap();
    let d1 = drain1.await.unwrap();
    let d2 = drain2.await.unwrap();

    // All 200 events should have been drained (plus any remaining)
    let remaining = buf.len();
    assert_eq!(d1 + d2 + remaining, 200);
}

#[tokio::test]
async fn test_capacity_boundary() {
    let buf = EventBuffer::new(5);
    for i in 0..10 {
        buf.push(make_event(&format!("e{}", i)));
    }
    assert_eq!(buf.len(), 5);
    let drained = buf.drain(10);
    // Oldest 5 events were evicted, so first event should be e5
    assert_eq!(drained[0].event_id, "e5");
    assert_eq!(drained[4].event_id, "e9");
}

#[tokio::test]
async fn test_push_front_batch_order() {
    let buf = EventBuffer::new(100);
    buf.push(make_event("e3"));
    buf.push(make_event("e4"));

    // Drain the two events
    let _ = buf.drain(2);

    // Re-insert at front
    buf.push_front_batch(vec![make_event("e1"), make_event("e2")]);

    // Push new event
    buf.push(make_event("e5"));

    let drained = buf.drain(10);
    assert_eq!(drained.len(), 3);
    assert_eq!(drained[0].event_id, "e1");
    assert_eq!(drained[1].event_id, "e2");
    assert_eq!(drained[2].event_id, "e5");
}
