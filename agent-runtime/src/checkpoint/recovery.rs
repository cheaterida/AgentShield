//! 回滚编排 + remediation prompt 生成。
//!
//! 当管理端下发 rollback_to 指令时：
//! 1. 对比当前文件系统状态与目标 checkpoint 的 manifest
//! 2. 执行文件操作矩阵（增→删, 改→恢复, 删→恢复, 不变→跳过）
//! 3. 生成并注入 remediation prompt 到消息历史末尾

use crate::checkpoint::journal::ChatMessage;
use crate::checkpoint::manifest::FileManifest;
use crate::checkpoint::CheckpointError;
use std::fs;
use std::path::Path;

/// 生成修复提示词，注入到回滚后的消息历史末尾。
pub fn generate_remediation_prompt(
    step_number: u64,
    risk_reason: &str,
    risk_score: f64,
    safe_paths: &[String],
) -> ChatMessage {
    let safe_paths_str = if safe_paths.is_empty() {
        "No specific restrictions on resource paths.".into()
    } else {
        safe_paths.join(", ")
    };

    let content = format!(
        "[AgentShield Security Intervention]\n\
         Your previous action was intercepted at step #{step_number}.\n\
         Reason: {risk_reason} (risk score: {risk_score:.2}).\n\
         The system has been restored to the state before that action.\n\
         \n\
         Safety guidance:\n\
         - Do NOT retry the same blocked action.\n\
         - Available safe resource paths are: {safe_paths_str}\n\
         - If you need access to a blocked resource, request it through proper channels.\n\
         \n\
         Continue your task from here, avoiding the blocked approach.",
    );

    ChatMessage {
        role: "system".into(),
        content,
        tool_call_id: None,
        tool_calls: None,
    }
}

/// 执行文件系统回滚，将工作区恢复到目标 manifest 状态。
///
/// 文件操作矩阵：
/// - 当前新增（manifest 中无）→ 删除
/// - 当前修改（hash 不同）→ 从 clean_copies 恢复
/// - 当前删除（manifest 中有但磁盘无）→ 从 clean_copies 恢复
/// - 未变更 → 跳过
pub fn rollback_filesystem(
    target_manifest: &FileManifest,
    clean_copies_dir: &Path,
    workspace_root: &Path,
) -> Result<(), CheckpointError> {
    let current_manifest =
        crate::checkpoint::manifest::compute_manifest(workspace_root)?;

    // 对于 target manifest 中的每个文件，检查当前状态
    for (rel_path, target_entry) in &target_manifest.files {
        let current_file = workspace_root.join(rel_path);
        let backup_file = clean_copies_dir.join(rel_path);

        match current_manifest.files.get(rel_path) {
            None => {
                // 文件已被删除 → 从 clean_copies 恢复
                tracing::info!(
                    path = %rel_path,
                    "rollback: restoring deleted file from clean_copies"
                );
                if backup_file.exists() {
                    if let Some(parent) = current_file.parent() {
                        fs::create_dir_all(parent)?;
                    }
                    fs::copy(&backup_file, &current_file)?;
                } else {
                    tracing::warn!(
                        path = %rel_path,
                        "rollback: deleted file has no backup in clean_copies"
                    );
                }
            }
            Some(current_entry) => {
                if current_entry.blake3_hash != target_entry.blake3_hash {
                    // 文件已被修改 → 从 clean_copies 恢复
                    tracing::info!(
                        path = %rel_path,
                        current_hash = %current_entry.blake3_hash,
                        target_hash = %target_entry.blake3_hash,
                        "rollback: restoring modified file from clean_copies"
                    );
                    if backup_file.exists() {
                        fs::copy(&backup_file, &current_file)?;
                    } else {
                        tracing::warn!(
                            path = %rel_path,
                            "rollback: modified file has no backup"
                        );
                    }
                }
                // else: hash 一致 → 跳过
            }
        }
    }

    // 删除 agent 新建的文件（在当前 manifest 中但不在 target manifest 中）
    for rel_path in current_manifest.files.keys() {
        if !target_manifest.files.contains_key(rel_path) {
            let file_path = workspace_root.join(rel_path);
            if file_path.exists() {
                tracing::info!(
                    path = %rel_path,
                    "rollback: removing agent-created file"
                );
                fs::remove_file(&file_path)?;
            }
        }
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::checkpoint::manifest;
    use std::io::Write;

    fn write_file(dir: &Path, name: &str, content: &[u8]) {
        let path = dir.join(name);
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent).unwrap();
        }
        let mut f = fs::File::create(&path).unwrap();
        f.write_all(content).unwrap();
    }

    #[test]
    fn test_remediation_prompt_generation() {
        let prompt = generate_remediation_prompt(
            7,
            "attempted delete of /etc/passwd",
            0.85,
            &["/workspace".into(), "/tmp/safe".into()],
        );

        assert_eq!(prompt.role, "system");
        assert!(prompt.content.contains("step #7"));
        assert!(prompt.content.contains("attempted delete of /etc/passwd"));
        assert!(prompt.content.contains("0.85"));
        assert!(prompt.content.contains("/workspace"));
        assert!(prompt.content.contains("/tmp/safe"));
        assert!(prompt.content.contains("Do NOT retry"));
    }

    #[test]
    fn test_remediation_prompt_empty_safe_paths() {
        let prompt = generate_remediation_prompt(1, "test", 0.3, &[]);
        assert!(prompt.content.contains("No specific restrictions"));
    }

    #[test]
    fn test_rollback_restore_created_file() {
        let dir = std::env::temp_dir().join("agent-runtime-test-rollback-create");
        let clean = dir.join("clean_copies");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&clean).unwrap();

        // Initial state: file_a.txt exists
        write_file(&dir, "file_a.txt", b"original");
        let target_manifest = manifest::compute_manifest(&dir).unwrap();
        // Backup file_a
        fs::copy(dir.join("file_a.txt"), clean.join("file_a.txt")).unwrap();

        // Agent creates new file
        write_file(&dir, "file_b.txt", b"agent created this");

        // Rollback
        rollback_filesystem(&target_manifest, &clean, &dir).expect("rollback should succeed");

        // file_a should still exist and be unchanged
        assert!(dir.join("file_a.txt").exists());
        // file_b should be deleted
        assert!(!dir.join("file_b.txt").exists());

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_rollback_restore_modified_file() {
        let dir = std::env::temp_dir().join("agent-runtime-test-rollback-mod");
        let clean = dir.join("clean_copies");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&clean).unwrap();

        write_file(&dir, "doc.txt", b"original content here");
        let target_manifest = manifest::compute_manifest(&dir).unwrap();
        fs::copy(dir.join("doc.txt"), clean.join("doc.txt")).unwrap();

        // Agent modifies the file
        write_file(&dir, "doc.txt", b"MALICIOUS INJECTED CONTENT!!!");

        // Rollback
        rollback_filesystem(&target_manifest, &clean, &dir).expect("rollback should succeed");

        let restored = fs::read_to_string(dir.join("doc.txt")).unwrap();
        assert_eq!(restored, "original content here");

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_rollback_restore_deleted_file() {
        let dir = std::env::temp_dir().join("agent-runtime-test-rollback-del");
        let clean = dir.join("clean_copies");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&clean).unwrap();

        write_file(&dir, "important.txt", b"critical data");
        let target_manifest = manifest::compute_manifest(&dir).unwrap();
        fs::copy(dir.join("important.txt"), clean.join("important.txt")).unwrap();

        // Agent deletes the file
        fs::remove_file(dir.join("important.txt")).unwrap();

        // Rollback
        rollback_filesystem(&target_manifest, &clean, &dir).expect("rollback should succeed");

        assert!(dir.join("important.txt").exists());
        let restored = fs::read_to_string(dir.join("important.txt")).unwrap();
        assert_eq!(restored, "critical data");

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_rollback_unchanged_file_untouched() {
        let dir = std::env::temp_dir().join("agent-runtime-test-rollback-unchanged");
        let clean = dir.join("clean_copies");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&clean).unwrap();

        write_file(&dir, "static.txt", b"never changes");
        let target_manifest = manifest::compute_manifest(&dir).unwrap();
        fs::copy(dir.join("static.txt"), clean.join("static.txt")).unwrap();

        // Don't touch the file
        rollback_filesystem(&target_manifest, &clean, &dir).expect("rollback should succeed");

        let content = fs::read_to_string(dir.join("static.txt")).unwrap();
        assert_eq!(content, "never changes");

        let _ = fs::remove_dir_all(&dir);
    }
}
