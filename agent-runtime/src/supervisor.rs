//! Hermes AI Agent 进程监管。

use std::io::{BufRead, BufReader};
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
        let mut child = Command::new(binary_path)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| format!("failed to start hermes: {}", e))?;

        // Drain stdout to prevent pipe buffer deadlock (64KB limit).
        if let Some(stdout) = child.stdout.take() {
            tokio::task::spawn_blocking(move || {
                let reader = BufReader::new(stdout);
                for line in reader.lines() {
                    match line {
                        Ok(line) => tracing::info!(target: "hermes", "{}", line),
                        Err(_) => break,
                    }
                }
            });
        }

        // Drain stderr to prevent pipe buffer deadlock (64KB limit).
        if let Some(stderr) = child.stderr.take() {
            tokio::task::spawn_blocking(move || {
                let reader = BufReader::new(stderr);
                for line in reader.lines() {
                    match line {
                        Ok(line) => tracing::warn!(target: "hermes", "{}", line),
                        Err(_) => break,
                    }
                }
            });
        }

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
