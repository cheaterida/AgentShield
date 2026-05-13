//! 本地策略缓存：从 management-server 拉取并缓存策略包。

use std::path::PathBuf;
use std::sync::RwLock;

pub struct PolicyCache {
    version: RwLock<String>,
    cache_dir: PathBuf,
}

impl PolicyCache {
    pub fn new(cache_dir: &str) -> Self {
        let dir = PathBuf::from(cache_dir);
        std::fs::create_dir_all(&dir).ok();
        Self {
            version: RwLock::new(String::new()),
            cache_dir: dir,
        }
    }

    /// 返回当前缓存的策略版本。
    pub fn current_version(&self) -> String {
        self.version.read().unwrap().clone()
    }

    /// 更新缓存的策略。
    pub fn store(&self, version: &str, payload: &[u8]) {
        let path = self.cache_dir.join("current_policy.rego");
        if let Err(e) = std::fs::write(&path, payload) {
            tracing::error!(path = %path.display(), error = %e, "failed to write policy");
            return;
        }
        *self.version.write().unwrap() = version.to_string();
        tracing::info!(version, "policy cached");
    }

    /// 读取当前策略内容。
    pub fn get_policy(&self) -> Option<Vec<u8>> {
        let path = self.cache_dir.join("current_policy.rego");
        std::fs::read(&path).ok()
    }
}
