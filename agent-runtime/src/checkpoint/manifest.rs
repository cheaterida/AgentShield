//! 文件清单 + BLAKE3 哈希。
//!
//! 在每次 checkpoint 时扫描工作区文件，记录每个文件的 BLAKE3 哈希。
//! 回滚时通过对比当前状态与 manifest 确定文件操作矩阵。

use crate::checkpoint::CheckpointError;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs;
use std::io::Read;
use std::path::Path;

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct FileEntry {
    pub blake3_hash: String,
    pub size_bytes: u64,
    pub modified_at: String,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct FileManifest {
    pub workspace_root: String,
    pub files: HashMap<String, FileEntry>,
    pub created_at: String,
}

#[derive(Clone, Debug, PartialEq)]
pub struct DiffResult {
    pub created: Vec<String>,
    pub modified: Vec<String>,
    pub deleted: Vec<String>,
    pub unchanged: Vec<String>,
}

/// 递归扫描工作区目录，为每个文件计算 BLAKE3 哈希。
pub fn compute_manifest(workspace_root: &Path) -> Result<FileManifest, CheckpointError> {
    let mut files = HashMap::new();
    let root_str = workspace_root
        .to_str()
        .ok_or_else(|| CheckpointError::Manifest("workspace path is not valid UTF-8".into()))?;

    walk_dir(workspace_root, workspace_root, &mut files)?;

    Ok(FileManifest {
        workspace_root: root_str.to_string(),
        files,
        created_at: chrono_now(),
    })
}

fn walk_dir(
    base: &Path,
    current: &Path,
    files: &mut HashMap<String, FileEntry>,
) -> Result<(), CheckpointError> {
    for entry in fs::read_dir(current)? {
        let entry = entry?;
        let path = entry.path();
        if path.is_dir() {
            walk_dir(base, &path, files)?;
        } else if path.is_file() {
            let rel = path
                .strip_prefix(base)
                .map_err(|e| CheckpointError::Manifest(format!("strip prefix: {}", e)))?;
            let rel_str = rel
                .to_str()
                .ok_or_else(|| CheckpointError::Manifest("non-UTF-8 path".into()))?;

            let mut f = fs::File::open(&path)?;
            let mut hasher = blake3::Hasher::new();
            let mut buf = [0u8; 8192];
            loop {
                let n = f.read(&mut buf)?;
                if n == 0 {
                    break;
                }
                hasher.update(&buf[..n]);
            }
            let hash = hasher.finalize().to_hex().to_string();

            let meta = f.metadata()?;
            let mtime = format_mtime(
                meta.modified()
                    .map_err(|e| CheckpointError::Manifest(format!("mtime: {}", e)))?,
            );

            files.insert(
                rel_str.to_string(),
                FileEntry {
                    blake3_hash: hash,
                    size_bytes: meta.len(),
                    modified_at: mtime,
                },
            );
        }
    }
    Ok(())
}

/// 对比当前 manifest 与基线 manifest，分类文件变更。
pub fn diff(current: &FileManifest, baseline: &FileManifest) -> DiffResult {
    let mut created = Vec::new();
    let mut modified = Vec::new();
    let mut deleted = Vec::new();
    let mut unchanged = Vec::new();

    // 当前有而基线无 → created
    // 当前有且基线有但 hash 不同 → modified
    // 当前有且基线有且 hash 相同 → unchanged
    for (path, entry) in &current.files {
        match baseline.files.get(path) {
            None => created.push(path.clone()),
            Some(base_entry) => {
                if entry.blake3_hash == base_entry.blake3_hash {
                    unchanged.push(path.clone());
                } else {
                    modified.push(path.clone());
                }
            }
        }
    }

    // 基线有而当前无 → deleted
    for path in baseline.files.keys() {
        if !current.files.contains_key(path) {
            deleted.push(path.clone());
        }
    }

    DiffResult {
        created,
        modified,
        deleted,
        unchanged,
    }
}

/// 备份将被修改或删除的文件到 clean_copies 目录。
pub fn backup_files(
    manifest: &FileManifest,
    diff: &DiffResult,
    clean_dir: &Path,
) -> Result<(), CheckpointError> {
    let workspace_root = Path::new(&manifest.workspace_root);

    fs::create_dir_all(clean_dir)?;

    // 备份被修改的文件（当前版本 → clean_copies）
    for path in &diff.modified {
        let src = workspace_root.join(path);
        let dst = clean_dir.join(path);
        if src.exists() {
            if let Some(parent) = dst.parent() {
                fs::create_dir_all(parent)?;
            }
            fs::copy(&src, &dst)?;
        }
    }

    // 备份被删除的文件（从工作区复制到 clean_copies，因为即将被删除）
    for path in &diff.deleted {
        let src = workspace_root.join(path);
        let dst = clean_dir.join(path);
        if src.exists() {
            if let Some(parent) = dst.parent() {
                fs::create_dir_all(parent)?;
            }
            fs::copy(&src, &dst)?;
        }
    }

    Ok(())
}

fn chrono_now() -> String {
    // 避免引入 chrono 依赖，手动构造 ISO 8601 格式
    use std::time::SystemTime;
    match SystemTime::now().duration_since(SystemTime::UNIX_EPOCH) {
        Ok(dur) => {
            let secs = dur.as_secs();
            // 简单 UNIX timestamp 字符串作为 fallback
            format!("{}", secs)
        }
        Err(_) => "unknown".into(),
    }
}

fn format_mtime(time: std::time::SystemTime) -> String {
    match time.duration_since(std::time::SystemTime::UNIX_EPOCH) {
        Ok(dur) => format!("{}", dur.as_secs()),
        Err(_) => "unknown".into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
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
    fn test_manifest_compute() {
        let dir = std::env::temp_dir().join("agent-runtime-test-manifest");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();

        write_file(&dir, "a.txt", b"hello world");
        write_file(&dir, "b.txt", b"foo bar baz");
        write_file(&dir, "sub/c.txt", b"nested file content");

        let manifest = compute_manifest(&dir).expect("compute should succeed");
        assert_eq!(manifest.files.len(), 3);

        let hash_a = &manifest.files.get("a.txt").unwrap().blake3_hash;
        assert_eq!(hash_a.len(), 64, "BLAKE3 hex hash should be 64 chars");
        assert!(!hash_a.is_empty());
        assert_eq!(manifest.files.get("a.txt").unwrap().size_bytes, 11);
        assert_eq!(manifest.files.get("b.txt").unwrap().size_bytes, 11);
        assert!(manifest.files.contains_key("sub/c.txt"));

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_manifest_diff_all_cases() {
        let manifest_base = FileManifest {
            workspace_root: "/ws".into(),
            files: {
                let mut m = HashMap::new();
                m.insert(
                    "keep.txt".into(),
                    FileEntry {
                        blake3_hash: "aaa".into(),
                        size_bytes: 10,
                        modified_at: "1000".into(),
                    },
                );
                m.insert(
                    "mod.txt".into(),
                    FileEntry {
                        blake3_hash: "bbb".into(),
                        size_bytes: 20,
                        modified_at: "1000".into(),
                    },
                );
                m.insert(
                    "del.txt".into(),
                    FileEntry {
                        blake3_hash: "ccc".into(),
                        size_bytes: 30,
                        modified_at: "1000".into(),
                    },
                );
                m
            },
            created_at: "1000".into(),
        };

        let manifest_current = FileManifest {
            workspace_root: "/ws".into(),
            files: {
                let mut m = HashMap::new();
                // keep.txt: unchanged
                m.insert(
                    "keep.txt".into(),
                    FileEntry {
                        blake3_hash: "aaa".into(),
                        size_bytes: 10,
                        modified_at: "1000".into(),
                    },
                );
                // mod.txt: hash changed
                m.insert(
                    "mod.txt".into(),
                    FileEntry {
                        blake3_hash: "bbb_modified".into(),
                        size_bytes: 25,
                        modified_at: "2000".into(),
                    },
                );
                // new.txt: created (not in baseline)
                m.insert(
                    "new.txt".into(),
                    FileEntry {
                        blake3_hash: "ddd".into(),
                        size_bytes: 5,
                        modified_at: "2000".into(),
                    },
                );
                // del.txt: not present → deleted
                m
            },
            created_at: "2000".into(),
        };

        let diff_result = diff(&manifest_current, &manifest_base);
        assert_eq!(diff_result.unchanged, vec!["keep.txt"]);
        assert_eq!(diff_result.modified, vec!["mod.txt"]);
        assert_eq!(diff_result.created, vec!["new.txt"]);
        assert_eq!(diff_result.deleted, vec!["del.txt"]);
    }

    #[test]
    fn test_backup_files_restore() {
        let dir = std::env::temp_dir().join("agent-runtime-test-backup");
        let clean_dir = dir.join("clean_copies");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();

        write_file(&dir, "original.txt", b"original content");

        let manifest = compute_manifest(&dir).expect("compute should succeed");

        // Simulate modification: backup original, then change it
        let diff_result = DiffResult {
            created: vec![],
            modified: vec!["original.txt".into()],
            deleted: vec![],
            unchanged: vec![],
        };

        backup_files(&manifest, &diff_result, &clean_dir).expect("backup should succeed");
        assert!(clean_dir.join("original.txt").exists());

        // Modify the file
        write_file(&dir, "original.txt", b"modified content!!");

        // Verify modified
        let modified = fs::read_to_string(dir.join("original.txt")).unwrap();
        assert_eq!(modified, "modified content!!");

        // Restore from backup
        fs::copy(clean_dir.join("original.txt"), dir.join("original.txt"))
            .expect("restore should succeed");

        let restored = fs::read_to_string(dir.join("original.txt")).unwrap();
        assert_eq!(restored, "original content");

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_manifest_empty_dir() {
        let dir = std::env::temp_dir().join("agent-runtime-test-manifest-empty");
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();

        let manifest = compute_manifest(&dir).expect("compute should succeed");
        assert!(manifest.files.is_empty());

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_diff_no_changes() {
        let manifest = FileManifest {
            workspace_root: "/ws".into(),
            files: {
                let mut m = HashMap::new();
                m.insert(
                    "f.txt".into(),
                    FileEntry {
                        blake3_hash: "aaa".into(),
                        size_bytes: 10,
                        modified_at: "1000".into(),
                    },
                );
                m
            },
            created_at: "1000".into(),
        };

        let result = diff(&manifest, &manifest);
        assert!(result.created.is_empty());
        assert!(result.modified.is_empty());
        assert!(result.deleted.is_empty());
        assert_eq!(result.unchanged, vec!["f.txt"]);
    }
}
