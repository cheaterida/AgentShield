use agent_runtime::probe_event_conv::{
    attrs_from_event, convert, make_event_id, resource_ref_from_event, syscall_to_action,
};
use agentshield_ebpf_common::ProbeEvent;

fn make_event(syscall: &str, filename: &str) -> ProbeEvent {
    let mut event = ProbeEvent::default();
    event.set_syscall(syscall);
    event.set_filename(filename);
    event.set_comm("testproc");
    event.pid = 42;
    event.uid = 1000;
    event
}

#[test]
fn test_openat_to_read() {
    let event = make_event("openat", "/etc/hosts");
    let payload = convert(&event, "agent-1", "fg-1");
    assert_eq!(payload.action, "read");
    assert_eq!(payload.resource_ref, "/etc/hosts");
    assert!(payload.event_id.starts_with("ebpf_"));
    assert_eq!(payload.agent_id, "agent-1");
    assert_eq!(payload.family_group_id, "fg-1");
}

#[test]
fn test_execve_to_exec() {
    let event = make_event("execve", "/bin/bash");
    let payload = convert(&event, "a", "f");
    assert_eq!(payload.action, "exec");
}

#[test]
fn test_connect_to_network_connect() {
    assert_eq!(syscall_to_action("connect"), "network_connect");
}

#[test]
fn test_bind_to_socket_create() {
    assert_eq!(syscall_to_action("bind"), "socket_create");
}

#[test]
fn test_empty_filename_fallback() {
    let event = make_event("openat", "");
    // Default syscall bytes are all zeros => trimmed to ""
    // But we set syscall to "openat", so fallback should be "openat"
    let r = resource_ref_from_event(&event);
    assert_eq!(r, "openat");
}

#[test]
fn test_event_id_uniqueness() {
    let a = make_event_id();
    std::thread::sleep(std::time::Duration::from_millis(1));
    let b = make_event_id();
    assert_ne!(a, b);
}

#[test]
fn test_magic_default_is_e5() {
    let event = ProbeEvent::default();
    assert_eq!(event.magic, 0xE5);
}

#[test]
fn test_attrs_contains_pid_comm_uid() {
    let event = make_event("openat", "/tmp/x");
    let attrs = attrs_from_event(&event);
    assert_eq!(attrs.get("pid").unwrap(), "42");
    assert_eq!(attrs.get("comm").unwrap(), "testproc");
    assert_eq!(attrs.get("uid").unwrap(), "1000");
}

#[test]
fn test_unknown_syscall_passthrough() {
    assert_eq!(syscall_to_action("mmap"), "mmap");
    assert_eq!(syscall_to_action("write"), "write");
}
