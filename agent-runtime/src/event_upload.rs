//! 后台事件批量上传任务。

use crate::client::ManagementClient;
use crate::event_buffer::EventBuffer;
use std::sync::Arc;
use std::time::Duration;

pub struct EventUploadTask {
    client: ManagementClient,
    buffer: Arc<EventBuffer>,
    batch_size: usize,
}

impl EventUploadTask {
    pub fn new(client: ManagementClient, buffer: Arc<EventBuffer>, batch_size: usize) -> Self {
        Self {
            client,
            buffer,
            batch_size,
        }
    }

    pub async fn run(self, interval_secs: u64) {
        let mut interval = tokio::time::interval(Duration::from_secs(interval_secs));
        loop {
            interval.tick().await;

            let events = self.buffer.drain(self.batch_size);
            if events.is_empty() {
                continue;
            }
            let n = events.len();
            match self.client.upload_events(&events).await {
                Ok(_) => {
                    tracing::debug!(count = n, "batch uploaded");
                }
                Err(e) => {
                    tracing::warn!(count = n, error = %e, "upload failed, re-queuing");
                    self.buffer.push_front_batch(events);
                }
            }
        }
    }
}
