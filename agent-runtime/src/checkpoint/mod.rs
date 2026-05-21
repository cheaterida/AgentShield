pub mod journal;
pub mod manifest;
pub mod recovery;
pub mod workspace;

use std::fs;
use std::path::PathBuf;
use std::sync::Arc;
use thiserror::Error;

use crate::checkpoint::journal::JournalEntry;
use crate::checkpoint::manifest::FileManifest;
use crate::checkpoint::workspace::WorkspaceTracker;

#[derive(Error, Debug)]
pub enum CheckpointError {
    #[error("serialization failed: {0}")]
    Serialize(String),

    #[error("deserialization failed: {0}")]
    Deserialize(String),

    #[error("io error: {0}")]
    Io(#[from] std::io::Error),

    #[error("checkpoint not found: {0}")]
    NotFound(String),

    #[error("manifest compute failed: {0}")]
    Manifest(String),

    #[error("workspace tracking error: {0}")]
    Workspace(String),

    #[error("recovery failed: {0}")]
    Recovery(String),

    #[error("prune failed: {0}")]
    Prune(String),
}

#[derive(Clone, Debug)]
pub struct CheckpointConfig {
    pub enabled: bool,
    pub checkpoint_dir: PathBuf,
    pub max_count: usize,
    pub workspace_dir: PathBuf,
    pub interval_steps: u64,
}

#[derive(Clone, Debug)]
pub struct CheckpointMeta {
    pub checkpoint_id: String,
    pub step_number: u64,
    pub created_at: String,
    pub file_count: usize,
}

pub struct CheckpointManager {
    config: CheckpointConfig,
    checkpoint_dir: PathBuf,
    pub tracker: Arc<WorkspaceTracker>,
}

impl CheckpointManager {
    pub fn new(
        config: CheckpointConfig,
    ) -> Result<Self, CheckpointError> {
        fs::create_dir_all(&config.checkpoint_dir)?;

        let tracker = Arc::new(WorkspaceTracker::new(&config.workspace_dir)?);

        Ok(Self {
            checkpoint_dir: config.checkpoint_dir.clone(),
            config,
            tracker,
        })
    }

    /// 创建新快照。
    /// 1. 序列化 journal → 写入 {checkpoint_dir}/{id}/journal.msgpack
    /// 2. 计算 manifest → 写入 {checkpoint_dir}/{id}/file_manifest.json
    /// 3. 备份被修改/删除的文件到 {checkpoint_dir}/{id}/clean_copies/
    ///
    /// 返回 checkpoint_id。
    pub fn create(&self, entry: &JournalEntry) -> Result<String, CheckpointError> {
        let ckpt_dir = self.checkpoint_dir.join(&entry.checkpoint_id);
        fs::create_dir_all(&ckpt_dir)?;

        // 1. 保存 journal
        let journal_path = ckpt_dir.join("journal.msgpack");
        journal::save_to_file(entry, &journal_path)?;

        // 2. 计算并保存 manifest
        let current_manifest = manifest::compute_manifest(&self.config.workspace_dir)?;
        let manifest_path = ckpt_dir.join("file_manifest.json");
        let manifest_json = serde_json::to_string_pretty(&current_manifest)
            .map_err(|e| CheckpointError::Serialize(e.to_string()))?;
        fs::write(&manifest_path, manifest_json)?;

        // 3. 备份文件（与前一个 checkpoint 对比）
        let clean_dir = ckpt_dir.join("clean_copies");
        // 对当前已存在的文件做备份（与空 manifest 对比意味着全部备份）
        // 实际场景中应与前一个 checkpoint 对比；初次则全量备份
        let prev_manifest = self.load_latest_manifest()?;
        let diff = manifest::diff(&current_manifest, &prev_manifest);
        manifest::backup_files(&current_manifest, &diff, &clean_dir)?;

        tracing::info!(
            checkpoint_id = %entry.checkpoint_id,
            step = entry.step_number,
            files = current_manifest.files.len(),
            "checkpoint created"
        );

        Ok(entry.checkpoint_id.clone())
    }

    /// 回滚到指定 checkpoint。
    /// 1. 加载目标 checkpoint 的 journal
    /// 2. 执行文件系统回滚
    /// 3. 生成并追加 remediation prompt 到 journal
    /// 4. 返回恢复后的 JournalEntry
    pub fn rollback(
        &self,
        checkpoint_id: &str,
        risk_reason: &str,
        risk_score: f64,
        safe_paths: &[String],
    ) -> Result<JournalEntry, CheckpointError> {
        let ckpt_dir = self.checkpoint_dir.join(checkpoint_id);
        if !ckpt_dir.exists() {
            return Err(CheckpointError::NotFound(checkpoint_id.into()));
        }

        // 1. 加载目标 journal
        let journal_path = ckpt_dir.join("journal.msgpack");
        let mut entry = journal::load_from_file(&journal_path)?;

        // 2. 加载目标 manifest
        let manifest_path = ckpt_dir.join("file_manifest.json");
        let manifest_json = fs::read_to_string(&manifest_path)?;
        let target_manifest: FileManifest = serde_json::from_str(&manifest_json)
            .map_err(|e| CheckpointError::Deserialize(e.to_string()))?;

        // 3. 文件系统回滚
        let clean_dir = ckpt_dir.join("clean_copies");
        recovery::rollback_filesystem(
            &target_manifest,
            &clean_dir,
            &self.config.workspace_dir,
        )?;

        // 4. 注入 remediation prompt
        let prompt = recovery::generate_remediation_prompt(
            entry.step_number,
            risk_reason,
            risk_score,
            safe_paths,
        );
        entry.messages.push(prompt);

        // 5. 将更新后的 journal 写回（保留回滚记录）
        journal::save_to_file(&entry, &journal_path)?;

        tracing::info!(
            checkpoint_id = %checkpoint_id,
            risk_score = risk_score,
            "rollback completed"
        );

        Ok(entry)
    }

    /// 列出所有 checkpoint 元数据，按创建时间排序（最旧优先）。
    pub fn list(&self) -> Result<Vec<CheckpointMeta>, CheckpointError> {
        let mut metas = Vec::new();

        if !self.checkpoint_dir.exists() {
            return Ok(metas);
        }

        for entry in fs::read_dir(&self.checkpoint_dir)? {
            let entry = entry?;
            if !entry.file_type()?.is_dir() {
                continue;
            }

            let journal_path = entry.path().join("journal.msgpack");
            if !journal_path.exists() {
                continue;
            }

            match journal::load_from_file(&journal_path) {
                Ok(j) => {
                    let manifest_path = entry.path().join("file_manifest.json");
                    let file_count = if manifest_path.exists() {
                        fs::read_to_string(&manifest_path)
                            .ok()
                            .and_then(|s| serde_json::from_str::<FileManifest>(&s).ok())
                            .map(|m| m.files.len())
                            .unwrap_or(0)
                    } else {
                        0
                    };

                    metas.push(CheckpointMeta {
                        checkpoint_id: j.checkpoint_id,
                        step_number: j.step_number,
                        created_at: j.created_at,
                        file_count,
                    });
                }
                Err(e) => {
                    tracing::warn!(
                        path = %journal_path.display(),
                        error = %e,
                        "skipping corrupt checkpoint"
                    );
                }
            }
        }

        metas.sort_by(|a, b| a.created_at.cmp(&b.created_at));
        Ok(metas)
    }

    /// 保留最近 `max_count` 个 checkpoint，删除更旧的。
    pub fn prune(&self, max_count: usize) -> Result<(), CheckpointError> {
        let metas = self.list()?;
        if metas.len() <= max_count {
            return Ok(());
        }

        let to_remove = metas.len() - max_count;
        for meta in metas.iter().take(to_remove) {
            let ckpt_dir = self.checkpoint_dir.join(&meta.checkpoint_id);
            if ckpt_dir.exists() {
                fs::remove_dir_all(&ckpt_dir)?;
                tracing::info!(
                    checkpoint_id = %meta.checkpoint_id,
                    "pruned old checkpoint"
                );
            }
        }

        Ok(())
    }

    /// 加载最新 checkpoint 的 manifest（用于 diff 计算）。
    fn load_latest_manifest(&self) -> Result<FileManifest, CheckpointError> {
        let metas = self.list()?;
        if let Some(last) = metas.last() {
            let manifest_path = self
                .checkpoint_dir
                .join(&last.checkpoint_id)
                .join("file_manifest.json");
            if manifest_path.exists() {
                let json = fs::read_to_string(&manifest_path)?;
                serde_json::from_str(&json)
                    .map_err(|e| CheckpointError::Deserialize(e.to_string()))
            } else {
                Ok(FileManifest {
                    workspace_root: self.config.workspace_dir.to_string_lossy().into(),
                    files: std::collections::HashMap::new(),
                    created_at: String::new(),
                })
            }
        } else {
            Ok(FileManifest {
                workspace_root: self.config.workspace_dir.to_string_lossy().into(),
                files: std::collections::HashMap::new(),
                created_at: String::new(),
            })
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::checkpoint::journal::{ChatMessage, JournalEntry, RiskSnapshot, TokenUsage};
    use std::io::Write;
    use std::path::Path;

    fn write_file(dir: &Path, name: &str, content: &[u8]) {
        let path = dir.join(name);
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).unwrap();
        }
        let mut f = fs::File::create(&path).unwrap();
        f.write_all(content).unwrap();
    }

    fn make_test_entry(id: &str, step: u64) -> JournalEntry {
        JournalEntry {
            checkpoint_id: id.into(),
            agent_id: "test-agent".into(),
            created_at: format!("2000-01-01T00:00:0{}Z", step),
            step_number: step,
            messages: vec![ChatMessage {
                role: "user".into(),
                content: "test message".into(),
                tool_call_id: None,
                tool_calls: None,
            }],
            tool_definitions: "[]".into(),
            working_memory: String::new(),
            token_usage: TokenUsage {
                input_tokens: 10,
                output_tokens: 5,
            },
            risk_snapshot: RiskSnapshot {
                ema_score: 0.1,
                threshold: 0.6,
            },
            previous_checkpoint_id: None,
        }
    }

    #[test]
    fn test_checkpoint_create_rollback_cycle() {
        let base = std::env::temp_dir().join("agent-runtime-test-ckpt-cycle");
        let _ = fs::remove_dir_all(&base);
        let ws = base.join("workspace");
        let ckpt_dir = base.join("checkpoints");
        fs::create_dir_all(&ws).unwrap();

        // Initial file
        write_file(&ws, "data.txt", b"original data");

        let mgr = CheckpointManager::new(CheckpointConfig {
            enabled: true,
            checkpoint_dir: ckpt_dir.clone(),
            max_count: 50,
            workspace_dir: ws.clone(),
            interval_steps: 1,
        })
        .expect("new should succeed");

        // Create checkpoint
        let entry1 = make_test_entry("ckpt-001", 1);
        let id = mgr.create(&entry1).expect("create should succeed");
        assert_eq!(id, "ckpt-001");
        assert!(ckpt_dir.join("ckpt-001/journal.msgpack").exists());
        assert!(ckpt_dir.join("ckpt-001/file_manifest.json").exists());

        // Simulate agent misbehavior: modify, create, delete files
        write_file(&ws, "data.txt", b"CORRUPTED DATA!!!");
        write_file(&ws, "malware.sh", b"evil script");
        fs::remove_file(ws.join("data.txt")).unwrap();
        // Recreate with different content (simulates complex scenario)
        write_file(&ws, "data.txt", b"different content");

        // Rollback
        let restored = mgr
            .rollback("ckpt-001", "test anomaly", 0.75, &["/workspace".into()])
            .expect("rollback should succeed");

        // Verify restoration
        let content = fs::read_to_string(ws.join("data.txt")).unwrap();
        assert_eq!(content, "original data");
        // Malware file should be removed
        assert!(!ws.join("malware.sh").exists());
        // Remediation prompt is appended
        let last_msg = restored.messages.last().unwrap();
        assert_eq!(last_msg.role, "system");
        assert!(last_msg.content.contains("Security Intervention"));

        let _ = fs::remove_dir_all(&base);
    }

    #[test]
    fn test_checkpoint_prune() {
        let base = std::env::temp_dir().join("agent-runtime-test-ckpt-prune");
        let _ = fs::remove_dir_all(&base);
        let ws = base.join("workspace");
        let ckpt_dir = base.join("checkpoints");
        fs::create_dir_all(&ws).unwrap();
        write_file(&ws, "f.txt", b"hello");

        let mgr = CheckpointManager::new(CheckpointConfig {
            enabled: true,
            checkpoint_dir: ckpt_dir.clone(),
            max_count: 50,
            workspace_dir: ws.clone(),
            interval_steps: 1,
        })
        .expect("new should succeed");

        // Create 5 checkpoints
        for i in 0..5 {
            let entry = make_test_entry(&format!("ckpt-{:03}", i), i);
            mgr.create(&entry).expect("create should succeed");
        }

        let metas = mgr.list().expect("list should succeed");
        assert_eq!(metas.len(), 5);

        // Prune to 2
        mgr.prune(2).expect("prune should succeed");
        let metas = mgr.list().expect("list should succeed");
        assert_eq!(metas.len(), 2);
        assert_eq!(metas[0].checkpoint_id, "ckpt-003");
        assert_eq!(metas[1].checkpoint_id, "ckpt-004");

        let _ = fs::remove_dir_all(&base);
    }

    #[test]
    fn test_checkpoint_list_sorted() {
        let base = std::env::temp_dir().join("agent-runtime-test-ckpt-list");
        let _ = fs::remove_dir_all(&base);
        let ws = base.join("workspace");
        let ckpt_dir = base.join("checkpoints");
        fs::create_dir_all(&ws).unwrap();
        write_file(&ws, "f.txt", b"hello");

        let mgr = CheckpointManager::new(CheckpointConfig {
            enabled: true,
            checkpoint_dir: ckpt_dir.clone(),
            max_count: 50,
            workspace_dir: ws.clone(),
            interval_steps: 1,
        })
        .expect("new should succeed");

        for i in 0..3 {
            let entry = make_test_entry(&format!("ckpt-{:03}", i), i);
            mgr.create(&entry).expect("create should succeed");
        }

        let metas = mgr.list().expect("list should succeed");
        assert_eq!(metas.len(), 3);
        // Should be sorted by created_at
        for i in 0..2 {
            assert!(metas[i].created_at <= metas[i + 1].created_at);
        }

        let _ = fs::remove_dir_all(&base);
    }

    #[test]
    fn test_rollback_nonexistent_checkpoint() {
        let base = std::env::temp_dir().join("agent-runtime-test-ckpt-nope");
        let _ = fs::remove_dir_all(&base);
        let ws = base.join("workspace");
        let ckpt_dir = base.join("checkpoints");
        fs::create_dir_all(&ws).unwrap();

        let mgr = CheckpointManager::new(CheckpointConfig {
            enabled: true,
            checkpoint_dir: ckpt_dir,
            max_count: 50,
            workspace_dir: ws,
            interval_steps: 1,
        })
        .expect("new should succeed");

        let result = mgr.rollback("nonexistent", "test", 0.5, &[]);
        assert!(result.is_err());
        match result {
            Err(CheckpointError::NotFound(id)) => assert_eq!(id, "nonexistent"),
            _ => panic!("expected NotFound error"),
        }

        let _ = fs::remove_dir_all(&base);
    }
}
