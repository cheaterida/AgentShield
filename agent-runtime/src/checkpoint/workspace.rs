//! 工作区文件变更追踪。
//!
//! Linux: 使用 inotify 内核事件监听文件增/改/删。
//! 非 Linux (macOS/Windows): 使用定时轮询 + manifest diff 降级方案。

use crate::checkpoint::manifest::{self, FileManifest};
use crate::checkpoint::CheckpointError;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug, PartialEq)]
pub enum FileChangeEvent {
    Created { path: String },
    Modified { path: String },
    Deleted { path: String },
}

pub struct WorkspaceTracker {
    workspace_root: PathBuf,
    last_manifest: Mutex<FileManifest>,
    events: Mutex<Vec<FileChangeEvent>>,
}

impl WorkspaceTracker {
    /// 创建新的工作区追踪器。
    pub fn new(workspace_root: &Path) -> Result<Self, CheckpointError> {
        let initial_manifest = manifest::compute_manifest(workspace_root)?;
        Ok(Self {
            workspace_root: workspace_root.to_path_buf(),
            last_manifest: Mutex::new(initial_manifest),
            events: Mutex::new(Vec::new()),
        })
    }

    /// 启动后台追踪任务。
    /// Linux 上使用 inotify，其他平台使用定时轮询。
    pub fn start_watching(self: Arc<Self>) -> tokio::task::JoinHandle<()> {
        #[cfg(target_os = "linux")]
        {
            let tracker = self.clone();
            tokio::task::spawn_blocking(move || {
                if let Err(e) = run_inotify_watch(tracker) {
                    tracing::error!(error = %e, "inotify watch failed, falling back to polling");
                }
            })
        }
        #[cfg(not(target_os = "linux"))]
        {
            tokio::spawn(run_polling_watch(self))
        }
    }

    /// 清空并返回所有累积的文件变更事件。
    pub fn drain_events(&self) -> Vec<FileChangeEvent> {
        let mut events = self.events.lock().unwrap();
        std::mem::take(&mut *events)
    }

    /// 返回当前文件清单（上次刷新时的快照）。
    pub fn current_manifest(&self) -> FileManifest {
        self.last_manifest.lock().unwrap().clone()
    }

    /// 重新计算文件清单并更新缓存，返回差异。
    pub fn refresh_manifest(&self) -> Result<(), CheckpointError> {
        let new_manifest = manifest::compute_manifest(&self.workspace_root)?;
        let old = self.last_manifest.lock().unwrap().clone();
        let diff = manifest::diff(&new_manifest, &old);

        let mut events = self.events.lock().unwrap();
        for path in &diff.created {
            events.push(FileChangeEvent::Created {
                path: path.clone(),
            });
        }
        for path in &diff.modified {
            events.push(FileChangeEvent::Modified {
                path: path.clone(),
            });
        }
        for path in &diff.deleted {
            events.push(FileChangeEvent::Deleted {
                path: path.clone(),
            });
        }

        *self.last_manifest.lock().unwrap() = new_manifest;
        Ok(())
    }
}

#[cfg(target_os = "linux")]
fn run_inotify_watch(tracker: Arc<WorkspaceTracker>) -> Result<(), CheckpointError> {
    use inotify::{EventMask, Inotify, WatchMask};

    let mut inotify = Inotify::init().map_err(|e| {
        CheckpointError::Workspace(format!("inotify init: {}", e))
    })?;

    inotify
        .watches()
        .add(
            &tracker.workspace_root,
            WatchMask::CREATE
                | WatchMask::MODIFY
                | WatchMask::DELETE
                | WatchMask::MOVED_FROM
                | WatchMask::MOVED_TO,
        )
        .map_err(|e| CheckpointError::Workspace(format!("inotify add watch: {}", e)))?;

    let mut buffer = [0u8; 4096];
    loop {
        let events = inotify
            .read_events_blocking(&mut buffer)
            .map_err(|e| CheckpointError::Workspace(format!("inotify read: {}", e)))?;

        let mut collected = Vec::new();
        for event in events {
            if let Some(name) = event.name {
                let name_str = name.to_string_lossy().to_string();
                if event.mask.contains(EventMask::CREATE) {
                    collected.push(FileChangeEvent::Created { path: name_str });
                } else if event.mask.contains(EventMask::MODIFY) {
                    collected.push(FileChangeEvent::Modified { path: name_str });
                } else if event.mask.contains(EventMask::DELETE)
                    || event.mask.contains(EventMask::MOVED_FROM)
                {
                    collected.push(FileChangeEvent::Deleted { path: name_str });
                }
            }
        }

        if !collected.is_empty() {
            let mut events = tracker.events.lock().unwrap();
            events.extend(collected);
        }
    }
}

#[cfg(not(target_os = "linux"))]
async fn run_polling_watch(tracker: Arc<WorkspaceTracker>) {
    let mut interval = tokio::time::interval(std::time::Duration::from_millis(500));
    loop {
        interval.tick().await;
        if let Err(e) = tracker.refresh_manifest() {
            tracing::warn!(error = %e, "workspace poll refresh failed");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use std::io::Write;

    fn create_test_file(dir: &Path, name: &str, content: &[u8]) {
        let path = dir.join(name);
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).unwrap();
        }
        let mut f = fs::File::create(&path).unwrap();
        f.write_all(content).unwrap();
    }

    #[test]
    fn test_workspace_tracking_created() {
        let dir = std::env::temp_dir().join("agent-runtime-test-ws-create");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();

        let tracker = Arc::new(WorkspaceTracker::new(&dir).expect("new should succeed"));

        create_test_file(&dir, "new_file.txt", b"hello");

        tracker.refresh_manifest().expect("refresh should succeed");
        let events = tracker.drain_events();
        assert!(!events.is_empty());
        let created = events
            .iter()
            .any(|e| matches!(e, FileChangeEvent::Created { path } if path == "new_file.txt"));
        assert!(created, "should have Created event for new_file.txt");

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_workspace_tracking_modified() {
        let dir = std::env::temp_dir().join("agent-runtime-test-ws-mod");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();

        create_test_file(&dir, "mod_file.txt", b"initial");

        let tracker = Arc::new(WorkspaceTracker::new(&dir).expect("new should succeed"));
        // Drain initial events from the first refresh (the file creation happened before tracker init)
        tracker.refresh_manifest().expect("refresh should succeed");
        let _ = tracker.drain_events();

        // Now modify
        create_test_file(&dir, "mod_file.txt", b"modified content");

        tracker.refresh_manifest().expect("refresh should succeed");
        let events = tracker.drain_events();
        let modified = events
            .iter()
            .any(|e| matches!(e, FileChangeEvent::Modified { path } if path == "mod_file.txt"));
        assert!(modified, "should have Modified event for mod_file.txt");

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_workspace_tracking_deleted() {
        let dir = std::env::temp_dir().join("agent-runtime-test-ws-del");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();

        create_test_file(&dir, "del_file.txt", b"to be deleted");

        let tracker = Arc::new(WorkspaceTracker::new(&dir).expect("new should succeed"));
        tracker.refresh_manifest().expect("refresh should succeed");
        let _ = tracker.drain_events();

        // Delete the file
        fs::remove_file(dir.join("del_file.txt")).unwrap();

        tracker.refresh_manifest().expect("refresh should succeed");
        let events = tracker.drain_events();
        let deleted = events
            .iter()
            .any(|e| matches!(e, FileChangeEvent::Deleted { path } if path == "del_file.txt"));
        assert!(deleted, "should have Deleted event for del_file.txt");

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_workspace_current_manifest() {
        let dir = std::env::temp_dir().join("agent-runtime-test-ws-manifest");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();

        create_test_file(&dir, "f1.txt", b"content 1");

        let tracker = WorkspaceTracker::new(&dir).expect("new should succeed");
        let manifest = tracker.current_manifest();
        assert!(manifest.files.contains_key("f1.txt"));
        assert_eq!(manifest.files.get("f1.txt").unwrap().size_bytes, 9);

        let _ = fs::remove_dir_all(&dir);
    }
}
