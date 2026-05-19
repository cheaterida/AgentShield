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


impl Default for ProbeEvent {
    fn default() -> Self {
        Self {
            pid: 0,
            tid: 0,
            uid: 0,
            comm: [0u8; 16],
            syscall: [0u8; 16],
            filename: [0u8; 256],
            argv: [0u8; 256],
            retval: 0,
        }
    }
}

impl ProbeEvent {

    pub fn syscall_str(&self) -> &str {
        core::str::from_utf8(&self.syscall)
            .unwrap_or("")
            .trim_end_matches('\0')
    }

    pub fn filename_str(&self) -> &str {
        core::str::from_utf8(&self.filename)
            .unwrap_or("")
            .trim_end_matches('\0')
    }

    pub fn comm_str(&self) -> &str {
        core::str::from_utf8(&self.comm)
            .unwrap_or("")
            .trim_end_matches('\0')
    }

    pub fn set_syscall(&mut self, s: &str) {
        let bytes = s.as_bytes();
        let len = bytes.len().min(15);
        self.syscall[..len].copy_from_slice(&bytes[..len]);
        self.syscall[len] = 0;
    }

    pub fn set_filename(&mut self, s: &str) {
        let bytes = s.as_bytes();
        let len = bytes.len().min(255);
        self.filename[..len].copy_from_slice(&bytes[..len]);
        self.filename[len] = 0;
    }

    pub fn set_comm(&mut self, s: &str) {
        let bytes = s.as_bytes();
        let len = bytes.len().min(15);
        self.comm[..len].copy_from_slice(&bytes[..len]);
        self.comm[len] = 0;
    }
}
