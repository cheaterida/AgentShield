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

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn test_new_cache_has_empty_version() {
        let dir = std::env::temp_dir().join("agentshield-test-cache-new");
        let _ = fs::remove_dir_all(&dir);
        let cache = PolicyCache::new(dir.to_str().unwrap());
        assert_eq!(cache.current_version(), "");
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_store_and_get_policy() {
        let dir = std::env::temp_dir().join("agentshield-test-cache-store");
        let _ = fs::remove_dir_all(&dir);
        let cache = PolicyCache::new(dir.to_str().unwrap());

        cache.store("v1.0.0", b"package agentshield\ndefault allow = true");
        assert_eq!(cache.current_version(), "v1.0.0");

        let content = cache.get_policy().unwrap();
        assert_eq!(std::str::from_utf8(&content).unwrap(), "package agentshield\ndefault allow = true");

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_get_policy_when_empty() {
        let dir = std::env::temp_dir().join("agentshield-test-cache-empty");
        let _ = fs::remove_dir_all(&dir);
        let cache = PolicyCache::new(dir.to_str().unwrap());
        assert!(cache.get_policy().is_none());
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_store_updates_version() {
        let dir = std::env::temp_dir().join("agentshield-test-cache-ver");
        let _ = fs::remove_dir_all(&dir);
        let cache = PolicyCache::new(dir.to_str().unwrap());

        cache.store("v1", b"p1");
        assert_eq!(cache.current_version(), "v1");
        cache.store("v2", b"p2");
        assert_eq!(cache.current_version(), "v2");

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_cache_persists_to_disk() {
        let dir = std::env::temp_dir().join("agentshield-test-cache-persist");
        let _ = fs::remove_dir_all(&dir);
        {
            let cache = PolicyCache::new(dir.to_str().unwrap());
            cache.store("v3", b"test data bytes");
        }
        {
            // New instance from same dir should find the file
            let path = dir.join("current_policy.rego");
            assert!(path.exists());
            let content = fs::read(&path).unwrap();
            assert_eq!(content, b"test data bytes");
        }
        let _ = fs::remove_dir_all(&dir);
    }
}
