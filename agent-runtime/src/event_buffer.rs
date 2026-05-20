//! 线程安全环形缓冲区：缓存 eBPF 采集的审计事件。

use crate::client::AuditEventPayload;
use std::sync::Mutex;

pub struct EventBuffer {
    inner: Mutex<Vec<AuditEventPayload>>,
    capacity: usize,
}

impl EventBuffer {
    pub fn new(capacity: usize) -> Self {
        Self {
            inner: Mutex::new(Vec::with_capacity(capacity)),
            capacity,
        }
    }

    /// 压入事件；超出容量时移除最旧。
    pub fn push(&self, event: AuditEventPayload) {
        let mut buf = self.inner.lock().unwrap();
        if buf.len() >= self.capacity {
            buf.remove(0);
        }
        buf.push(event);
    }

    /// 批量取出最多 max 条事件（FIFO）。
    pub fn drain(&self, max: usize) -> Vec<AuditEventPayload> {
        let mut buf = self.inner.lock().unwrap();
        let n = max.min(buf.len());
        buf.drain(..n).collect()
    }

    /// 将事件重新插入队首以保持顺序。
    pub fn push_front_batch(&self, events: Vec<AuditEventPayload>) {
        let mut buf = self.inner.lock().unwrap();
        for ev in events.into_iter().rev() {
            buf.insert(0, ev);
        }
    }

    pub fn len(&self) -> usize {
        self.inner.lock().unwrap().len()
    }

    pub fn is_empty(&self) -> bool {
        self.inner.lock().unwrap().is_empty()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    fn make_event(id: &str) -> AuditEventPayload {
        AuditEventPayload {
            event_id: id.to_string(),
            occurred_at: "2024-01-01T00:00:00.000Z".to_string(),
            family_group_id: "fg".to_string(),
            agent_id: "a1".to_string(),
            resource_ref: "/tmp/test".to_string(),
            action: "read".to_string(),
            attributes: HashMap::new(),
        }
    }

    #[test]
    fn test_new_buffer_is_empty() {
        let buf = EventBuffer::new(10);
        assert_eq!(buf.len(), 0);
    }

    #[test]
    fn test_push_and_drain_fifo() {
        let buf = EventBuffer::new(100);
        buf.push(make_event("e1"));
        buf.push(make_event("e2"));
        buf.push(make_event("e3"));
        assert_eq!(buf.len(), 3);

        let drained = buf.drain(2);
        assert_eq!(drained.len(), 2);
        assert_eq!(drained[0].event_id, "e1");
        assert_eq!(drained[1].event_id, "e2");
        assert_eq!(buf.len(), 1);
    }

    #[test]
    fn test_capacity_limit() {
        let buf = EventBuffer::new(3);
        buf.push(make_event("e1"));
        buf.push(make_event("e2"));
        buf.push(make_event("e3"));
        buf.push(make_event("e4")); // should evict e1
        assert_eq!(buf.len(), 3);

        let drained = buf.drain(10);
        assert_eq!(drained.len(), 3);
        assert_eq!(drained[0].event_id, "e2");
        assert_eq!(drained[1].event_id, "e3");
        assert_eq!(drained[2].event_id, "e4");
    }

    #[test]
    fn test_drain_more_than_available() {
        let buf = EventBuffer::new(100);
        buf.push(make_event("e1"));
        let drained = buf.drain(10);
        assert_eq!(drained.len(), 1);
        assert_eq!(buf.len(), 0);
    }

    #[test]
    fn test_drain_empty() {
        let buf = EventBuffer::new(100);
        let drained = buf.drain(10);
        assert!(drained.is_empty());
    }

    #[test]
    fn test_push_front_batch_preserves_order() {
        let buf = EventBuffer::new(100);
        buf.push(make_event("e3"));
        buf.push_front_batch(vec![make_event("e1"), make_event("e2")]);
        let drained = buf.drain(10);
        assert_eq!(drained.len(), 3);
        assert_eq!(drained[0].event_id, "e1");
        assert_eq!(drained[1].event_id, "e2");
        assert_eq!(drained[2].event_id, "e3");
    }

    #[test]
    fn test_push_front_batch_empty() {
        let buf = EventBuffer::new(100);
        buf.push_front_batch(vec![]);
        assert_eq!(buf.len(), 0);
    }
}
