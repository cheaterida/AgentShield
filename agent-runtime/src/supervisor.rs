//! Hermes AI Agent 进程监管。

use std::process::{Child, Command, Stdio};
use std::sync::Mutex;

pub struct Supervisor {
    child: Mutex<Option<Child>>,
}

impl Supervisor {
    pub fn new() -> Self {
        Self {
            child: Mutex::new(None),
        }
    }

    /// 启动 Hermes agent 子进程。
    pub fn start(&self, binary_path: &str) -> Result<(), String> {
        let child = Command::new(binary_path)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| format!("failed to start hermes: {}", e))?;

        tracing::info!(pid = child.id(), path = binary_path, "hermes started");
        *self.child.lock().unwrap() = Some(child);
        Ok(())
    }

    /// 停止 Hermes agent。
    pub fn stop(&self) {
        let mut guard = self.child.lock().unwrap();
        if let Some(mut child) = guard.take() {
            tracing::info!(pid = child.id(), "stopping hermes");
            let _ = child.kill();
            let _ = child.wait();
        }
    }

    /// 检查 Hermes 是否在运行。
    pub fn is_running(&self) -> bool {
        let mut guard = self.child.lock().unwrap();
        if let Some(ref mut child) = *guard {
            match child.try_wait() {
                Ok(None) => true, // still running
                Ok(Some(status)) => {
                    tracing::warn!("hermes exited: {:?}", status);
                    *guard = None;
                    false
                }
                Err(_) => false,
            }
        } else {
            false
        }
    }

    /// 重启 Hermes agent。
    pub fn restart(&self, binary_path: &str) -> Result<(), String> {
        self.stop();
        self.start(binary_path)
    }
}

impl Drop for Supervisor {
    fn drop(&mut self) {
        self.stop();
    }
}
