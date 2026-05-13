#![no_std]

/// Shared event type between eBPF probes and userspace loader.
/// Mirrored in agent-runtime/src/client.rs as AuditEventPayload.
#[repr(C)]
#[derive(Clone, Copy)]
pub struct ProbeEvent {
    pub pid: u32,
    pub tid: u32,
    pub uid: u32,
    pub comm: [u8; 16],
    pub syscall: [u8; 16],   // "openat", "execve", "connect", "bind"
    pub filename: [u8; 256],
    pub argv: [u8; 256],
    pub retval: i64,
}


impl ProbeEvent {                                 

    pub fn syscall_str(&self) -> &str {
        core::str::from_utf8(&self.syscall)
            .unwrap_or("<unknown>")
            .trim_end_matches('\0')
    }

    pub fn filename_str(&self) -> &str {
        core::str::from_utf8(&self.filename)
            .unwrap_or("<unknown>")
            .trim_end_matches('\0')
    }

    pub fn comm_str(&self) -> &str {
        core::str::from_utf8(&self.comm)
            .unwrap_or("<unknown>")
            .trim_end_matches('\0')
    }
}
