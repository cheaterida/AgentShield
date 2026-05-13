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
}
