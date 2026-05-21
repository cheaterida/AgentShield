//! 消息历史 MessagePack 序列化。
//!
//! 在 Agent 的每个 ReAct 循环边界保存完整对话状态：
//! - 消息历史（system + user + assistant + tool）
//! - 工具定义
//! - 工作记忆
//! - Token 用量
//! - 风险评分快照

use crate::checkpoint::CheckpointError;
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::Path;

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct ChatMessage {
    pub role: String,
    pub content: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tool_call_id: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub tool_calls: Option<String>,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct TokenUsage {
    pub input_tokens: u64,
    pub output_tokens: u64,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct RiskSnapshot {
    pub ema_score: f64,
    pub threshold: f64,
}

#[derive(Serialize, Deserialize, Clone, Debug, PartialEq)]
pub struct JournalEntry {
    pub checkpoint_id: String,
    pub agent_id: String,
    pub created_at: String,
    pub step_number: u64,
    pub messages: Vec<ChatMessage>,
    pub tool_definitions: String,
    pub working_memory: String,
    pub token_usage: TokenUsage,
    pub risk_snapshot: RiskSnapshot,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub previous_checkpoint_id: Option<String>,
}

/// 将 JournalEntry 序列化为 MessagePack 字节。
pub fn serialize(entry: &JournalEntry) -> Result<Vec<u8>, CheckpointError> {
    rmp_serde::to_vec(entry).map_err(|e| CheckpointError::Serialize(e.to_string()))
}

/// 从 MessagePack 字节反序列化为 JournalEntry。
pub fn deserialize(data: &[u8]) -> Result<JournalEntry, CheckpointError> {
    rmp_serde::from_slice(data).map_err(|e| CheckpointError::Deserialize(e.to_string()))
}

/// 将 JournalEntry 序列化并写入文件。
pub fn save_to_file(entry: &JournalEntry, path: &Path) -> Result<(), CheckpointError> {
    let data = serialize(entry)?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    fs::write(path, &data)?;
    Ok(())
}

/// 从文件读取并反序列化 JournalEntry。
pub fn load_from_file(path: &Path) -> Result<JournalEntry, CheckpointError> {
    let data = fs::read(path)?;
    deserialize(&data)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_test_entry() -> JournalEntry {
        JournalEntry {
            checkpoint_id: "ckpt-00001".into(),
            agent_id: "agent-test".into(),
            created_at: "2026-05-20T12:00:00Z".into(),
            step_number: 3,
            messages: vec![
                ChatMessage {
                    role: "system".into(),
                    content: "You are a helpful assistant.".into(),
                    tool_call_id: None,
                    tool_calls: None,
                },
                ChatMessage {
                    role: "user".into(),
                    content: "Write a function to sort a list.".into(),
                    tool_call_id: None,
                    tool_calls: None,
                },
                ChatMessage {
                    role: "assistant".into(),
                    content: "Let me write that for you.".into(),
                    tool_call_id: None,
                    tool_calls: Some(
                        r#"[{"id":"call_1","name":"write_file","args":{"path":"sort.py","content":"def sort_list(lst): return sorted(lst)"}}]"#.into(),
                    ),
                },
            ],
            tool_definitions: r#"[{"name":"write_file","description":"Write a file to disk"}]"#.into(),
            working_memory: "Task: implement sorting utility".into(),
            token_usage: TokenUsage {
                input_tokens: 150,
                output_tokens: 80,
            },
            risk_snapshot: RiskSnapshot {
                ema_score: 0.15,
                threshold: 0.6,
            },
            previous_checkpoint_id: None,
        }
    }

    #[test]
    fn test_journal_roundtrip() {
        let entry = make_test_entry();
        let data = serialize(&entry).expect("serialize should succeed");
        let restored = deserialize(&data).expect("deserialize should succeed");
        assert_eq!(entry, restored);
    }

    #[test]
    fn test_journal_file_io() {
        let entry = make_test_entry();
        let dir = std::env::temp_dir().join("agent-runtime-test-journal");
        let file_path = dir.join("test.msgpack");

        // Cleanup from previous runs
        let _ = std::fs::remove_dir_all(&dir);

        save_to_file(&entry, &file_path).expect("save should succeed");
        assert!(file_path.exists());

        let loaded = load_from_file(&file_path).expect("load should succeed");
        assert_eq!(entry, loaded);

        // Cleanup
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn test_journal_chain_reference() {
        let mut entry1 = make_test_entry();
        entry1.checkpoint_id = "ckpt-00001".into();

        let mut entry2 = make_test_entry();
        entry2.checkpoint_id = "ckpt-00002".into();
        entry2.step_number = 6;
        entry2.previous_checkpoint_id = Some("ckpt-00001".into());

        let data2 = serialize(&entry2).expect("serialize should succeed");
        let restored2 = deserialize(&data2).expect("deserialize should succeed");
        assert_eq!(
            restored2.previous_checkpoint_id,
            Some("ckpt-00001".into())
        );
        assert_eq!(restored2.step_number, 6);
    }

    #[test]
    fn test_journal_empty_messages() {
        let entry = JournalEntry {
            checkpoint_id: "ckpt-empty".into(),
            agent_id: "agent-test".into(),
            created_at: "2026-05-20T12:00:00Z".into(),
            step_number: 0,
            messages: vec![],
            tool_definitions: "[]".into(),
            working_memory: String::new(),
            token_usage: TokenUsage {
                input_tokens: 0,
                output_tokens: 0,
            },
            risk_snapshot: RiskSnapshot {
                ema_score: 0.0,
                threshold: 0.6,
            },
            previous_checkpoint_id: None,
        };

        let data = serialize(&entry).expect("serialize should succeed");
        let restored = deserialize(&data).expect("deserialize should succeed");
        assert_eq!(entry, restored);
        assert!(restored.messages.is_empty());
    }

    #[test]
    fn test_deserialize_corrupted_data() {
        let corrupted = vec![0xFF, 0x00, 0xAA, 0xBB];
        let result = deserialize(&corrupted);
        assert!(result.is_err());
    }

    #[test]
    fn test_save_to_nonexistent_dir() {
        let entry = make_test_entry();
        let dir = std::env::temp_dir()
            .join("agent-runtime-test-journal-nonexistent")
            .join("subdir");
        let file_path = dir.join("test.msgpack");

        let _ = std::fs::remove_dir_all(dir.parent().unwrap());

        save_to_file(&entry, &file_path).expect("save should create parent dirs");
        assert!(file_path.exists());

        let loaded = load_from_file(&file_path).expect("load should succeed");
        assert_eq!(entry, loaded);

        let _ = std::fs::remove_dir_all(dir.parent().unwrap());
    }
}
