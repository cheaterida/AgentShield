//! ProbeEvent (kernel-side raw bytes) → AuditEventPayload conversion.

use agentshield_ebpf_common::ProbeEvent;
use crate::client::AuditEventPayload;
use std::collections::HashMap;
use std::time::{SystemTime, UNIX_EPOCH};

/// Maps a kernel syscall name to an AgentShield action string.
pub fn syscall_to_action(syscall: &str) -> &str {
    match syscall {
        "openat" => "read",
        "execve" => "exec",
        "connect" => "network_connect",
        "bind" => "socket_create",
        _ => syscall,
    }
}

/// Derives a resource_ref string from the ProbeEvent fields.
pub fn resource_ref_from_event(event: &ProbeEvent) -> String {
    let filename = event.filename_str();
    if !filename.is_empty() && filename != "(unknown)" {
        return filename.to_string();
    }
    event.syscall_str().to_string()
}

/// Build attributes map from ProbeEvent metadata.
pub fn attrs_from_event(event: &ProbeEvent) -> HashMap<String, String> {
    let mut attrs = HashMap::new();
    attrs.insert("pid".to_string(), event.pid.to_string());
    attrs.insert("comm".to_string(), event.comm_str().to_string());
    attrs.insert("uid".to_string(), event.uid.to_string());
    attrs
}

/// Generate a unique event_id based on timestamp.
pub fn make_event_id() -> String {
    let ts = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos();
    format!("ebpf_{:x}", ts)
}

/// RFC 3339 timestamp string.
pub fn make_timestamp() -> String {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default();
    let secs = now.as_secs();
    let nanos = now.subsec_nanos();
    let (y, m, d, h, min, s) = civil_from_secs(secs);
    format!("{:04}-{:02}-{:02}T{:02}:{:02}:{:02}.{:09}Z", y, m, d, h, min, s, nanos)
}

/// Convert a raw eBPF ProbeEvent into an AuditEventPayload.
pub fn convert(event: &ProbeEvent, agent_id: &str, family_group_id: &str) -> AuditEventPayload {
    let syscall = event.syscall_str();
    let action = syscall_to_action(&syscall);
    let resource_ref = resource_ref_from_event(event);

    AuditEventPayload {
        event_id: make_event_id(),
        occurred_at: make_timestamp(),
        family_group_id: family_group_id.to_string(),
        agent_id: agent_id.to_string(),
        resource_ref,
        action: action.to_string(),
        attributes: attrs_from_event(event),
    }
}

fn civil_from_secs(unix_secs: u64) -> (i32, u32, u32, u32, u32, u32) {
    let days_since_epoch = (unix_secs / 86400) as i64;
    let secs_of_day = (unix_secs % 86400) as u32;

    let z = days_since_epoch + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u32;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };

    let h = secs_of_day / 3600;
    let min = (secs_of_day % 3600) / 60;
    let s = secs_of_day % 60;

    (y as i32, m, d, h, min, s)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_syscall_to_action_known() {
        assert_eq!(syscall_to_action("openat"), "read");
        assert_eq!(syscall_to_action("execve"), "exec");
        assert_eq!(syscall_to_action("connect"), "network_connect");
        assert_eq!(syscall_to_action("bind"), "socket_create");
    }

    #[test]
    fn test_syscall_to_action_unknown_passthrough() {
        assert_eq!(syscall_to_action("mmap"), "mmap");
    }

    #[test]
    fn test_resource_ref_uses_filename() {
        let mut event = ProbeEvent::default();
        event.set_filename("test_file");
        let r = resource_ref_from_event(&event);
        assert!(r.contains("test_file"));
    }

    #[test]
    fn test_resource_ref_falls_back_to_syscall() {
        let event = ProbeEvent::default();
        let r = resource_ref_from_event(&event);
        // Default syscall bytes are all zeros => empty string after trim
        assert!(r.is_empty());
    }

    #[test]
    fn test_attrs_contains_pid_comm_uid() {
        let mut event = ProbeEvent::default();
        event.pid = 1234;
        event.uid = 1000;
        event.set_comm("testproc");
        let attrs = attrs_from_event(&event);
        assert_eq!(attrs.get("pid").unwrap(), "1234");
        assert_eq!(attrs.get("comm").unwrap(), "testproc");
        assert_eq!(attrs.get("uid").unwrap(), "1000");
    }

    #[test]
    fn test_convert_output_fields() {
        let mut event = ProbeEvent::default();
        event.pid = 42;
        event.set_syscall("openat");
        event.set_filename("/etc/hosts");
        event.set_comm("curl");
        let payload = convert(&event, "agent-1", "fg-1");
        assert_eq!(payload.agent_id, "agent-1");
        assert_eq!(payload.family_group_id, "fg-1");
        assert_eq!(payload.action, "read");
        assert_eq!(payload.resource_ref, "/etc/hosts");
        assert!(payload.event_id.starts_with("ebpf_"));
    }

    #[test]
    fn test_make_event_id_unique() {
        let a = make_event_id();
        std::thread::sleep(std::time::Duration::from_millis(1));
        let b = make_event_id();
        assert_ne!(a, b);
    }

    #[test]
    fn test_make_timestamp_format() {
        let ts = make_timestamp();
        assert!(ts.ends_with("Z"));
        assert!(ts.contains("T"));
        assert_eq!(ts.len(), 30); // YYYY-MM-DDTHH:MM:SS.fffffffffZ
    }
}
