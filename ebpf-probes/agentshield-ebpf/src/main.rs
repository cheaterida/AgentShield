#![no_std]
#![no_main]
#![allow(linker_messages)]

use aya_ebpf::{
    helpers::{
        bpf_get_current_pid_tgid,
        bpf_get_current_uid_gid,
        bpf_probe_read_user_str_bytes,
    },
    helpers::gen::bpf_probe_read_user,
    macros::{map, tracepoint},
    maps::{PerCpuArray, PerfEventArray},
    programs::TracePointContext,
};

use agentshield_ebpf_common::ProbeEvent;

#[map]
static EVENTS: PerfEventArray<ProbeEvent> = PerfEventArray::new(0);

/// Per-CPU buffer to avoid exceeding the BPF 512-byte stack limit.
#[repr(C)]
struct EventBuf {
    event: ProbeEvent,
}

#[map]
static BUF: PerCpuArray<EventBuf> = PerCpuArray::with_max_entries(1, 0);

#[tracepoint]
pub fn agentshield_sys_enter_openat(ctx: TracePointContext) -> u32 {
    match try_openat(&ctx) {
        Ok(()) => 0,
        Err(_) => 1,
    }
}

fn try_openat(ctx: &TracePointContext) -> Result<(), i64> {
    let buf = BUF.get_ptr_mut(0).ok_or(0)?;
    let event = unsafe { &mut (*buf).event };
    event.magic = 0xE5;

    let pid_tgid = bpf_get_current_pid_tgid();
    let uid_gid = bpf_get_current_uid_gid();
    event.pid = (pid_tgid >> 32) as u32;
    event.tid = pid_tgid as u32;
    event.uid = uid_gid as u32;
    event.retval = 0;

    let filename: *const u8 = unsafe { ctx.read_at(16)? };
    let fbytes = unsafe { bpf_probe_read_user_str_bytes(filename, &mut event.filename)? };
    if fbytes.len() < 256 {
        event.filename[fbytes.len()] = 0;
    }
    event.comm = bpf_get_current_comm().unwrap_or([0u8; 16]);
    str_to_bytes16("openat", &mut event.syscall);
    zero_bytes256(&mut event.argv);

    EVENTS.output(ctx, event, 0);
    Ok(())
}

#[tracepoint]
pub fn agentshield_sys_enter_execve(ctx: TracePointContext) -> u32 {
    match try_execve(&ctx) {
        Ok(()) => 0,
        Err(_) => 1,
    }
}

fn try_execve(ctx: &TracePointContext) -> Result<(), i64> {
    let buf = BUF.get_ptr_mut(0).ok_or(0)?;
    let event = unsafe { &mut (*buf).event };
    event.magic = 0xE5;

    let pid_tgid = bpf_get_current_pid_tgid();
    let uid_gid = bpf_get_current_uid_gid();
    event.pid = (pid_tgid >> 32) as u32;
    event.tid = pid_tgid as u32;
    event.uid = uid_gid as u32;
    event.retval = 0;

    let filename: *const u8 = unsafe { ctx.read_at(16)? };
    let fbytes = unsafe { bpf_probe_read_user_str_bytes(filename, &mut event.filename)? };
    if fbytes.len() < 256 {
        event.filename[fbytes.len()] = 0;
    }

    event.comm = bpf_get_current_comm().unwrap_or([0u8; 16]);
    str_to_bytes16("execve", &mut event.syscall);

    // Capture argv[0] from userspace (Task 2.2).
    // sys_enter_execve arg layout after common header:
    //   offset 16 = const char *filename, offset 24 = const char *const *argv
    // Debug coding via retval:
    //   0      = argv successfully captured (default, set above)
    //   -10    = ctx.read_at(24) returned Err (unexpected)
    //   -11    = bpf_probe_read_user failed to read argv[0] pointer
    //   -12    = bpf_probe_read_user_str_bytes failed on argv[0] string
    event.argv[0] = 0;
    match unsafe { ctx.read_at::<*const *const u8>(24) } {
        Ok(argv_ptr) if !argv_ptr.is_null() => {
            let mut arg0: *const u8 = core::ptr::null();
            let ret = unsafe {
                bpf_probe_read_user(
                    &mut arg0 as *mut *const u8 as *mut core::ffi::c_void,
                    core::mem::size_of::<*const u8>() as u32,
                    argv_ptr as *const core::ffi::c_void,
                )
            };
            if ret == 0 {
                match unsafe { bpf_probe_read_user_str_bytes(arg0, &mut event.argv) } {
                    Ok(arg_bytes) => {
                        if arg_bytes.len() < 256 {
                            event.argv[arg_bytes.len()] = 0;
                        }
                    }
                    Err(_) => {
                        event.retval = -12;
                    }
                }
            } else {
                event.retval = -11;
            }
        }
        Ok(_) => {
            // argv is NULL — valid (e.g. execve("/bin/sh", NULL, NULL)).
            // Leave argv as empty string.
        }
        Err(_) => {
            event.retval = -10;
        }
    }

    EVENTS.output(ctx, event, 0);
    Ok(())
}

#[tracepoint]
pub fn agentshield_sys_enter_connect(ctx: TracePointContext) -> u32 {
    let buf = match BUF.get_ptr_mut(0) {
        Some(p) => p,
        None => return 1,
    };
    let event = unsafe { &mut (*buf).event };
    event.magic = 0xE5;

    let pid_tgid = bpf_get_current_pid_tgid();
    let uid_gid = bpf_get_current_uid_gid();
    event.pid = (pid_tgid >> 32) as u32;
    event.tid = pid_tgid as u32;
    event.uid = uid_gid as u32;
    event.retval = 0;
    event.comm = bpf_get_current_comm().unwrap_or([0u8; 16]);
    str_to_bytes16("connect", &mut event.syscall);
    capture_sockaddr(&ctx, event);
    zero_bytes256(&mut event.argv);

    EVENTS.output(&ctx, event, 0);
    0
}

#[tracepoint]
pub fn agentshield_sys_enter_bind(ctx: TracePointContext) -> u32 {
    let buf = match BUF.get_ptr_mut(0) {
        Some(p) => p,
        None => return 1,
    };
    let event = unsafe { &mut (*buf).event };
    event.magic = 0xE5;

    let pid_tgid = bpf_get_current_pid_tgid();
    let uid_gid = bpf_get_current_uid_gid();
    event.pid = (pid_tgid >> 32) as u32;
    event.tid = pid_tgid as u32;
    event.uid = uid_gid as u32;
    event.retval = 0;
    event.comm = bpf_get_current_comm().unwrap_or([0u8; 16]);
    str_to_bytes16("bind", &mut event.syscall);
    capture_sockaddr(&ctx, event);
    zero_bytes256(&mut event.argv);

    EVENTS.output(&ctx, event, 0);
    0
}

// ── helpers ──

fn bpf_get_current_comm() -> Result<[u8; 16], i64> {
    aya_ebpf::helpers::bpf_get_current_comm()
}

fn str_to_bytes16(s: &str, out: &mut [u8; 16]) {
    let bytes = s.as_bytes();
    let len = bytes.len().min(15);
    out[..len].copy_from_slice(&bytes[..len]);
    out[len] = 0; // null-terminate (len < 16 always, so index 15 at worst)
}

fn zero_bytes256(buf: &mut [u8; 256]) {
    // Null-terminate the first byte; consumer reads as empty C string.
    // The PerCpuArray is pre-zeroed by the kernel, so stale bytes after
    // the null don't matter on second and subsequent uses either.
    buf[0] = 0;
}

fn str_to_bytes256(s: &str, out: &mut [u8; 256]) {
    let bytes = s.as_bytes();
    let len = bytes.len().min(255);
    out[..len].copy_from_slice(&bytes[..len]);
    out[len] = 0;
}

fn write_u8_dec(buf: &mut [u8], pos: usize, val: u8) -> usize {
    if val >= 200 {
        buf[pos] = b'2';
        let rem = val - 200;
        buf[pos + 1] = (rem / 10) + b'0';
        buf[pos + 2] = (rem % 10) + b'0';
        3
    } else if val >= 100 {
        buf[pos] = b'1';
        let rem = val - 100;
        buf[pos + 1] = (rem / 10) + b'0';
        buf[pos + 2] = (rem % 10) + b'0';
        3
    } else if val >= 10 {
        buf[pos] = (val / 10) + b'0';
        buf[pos + 1] = (val % 10) + b'0';
        2
    } else {
        buf[pos] = val + b'0';
        1
    }
}

fn write_port(buf: &mut [u8], pos: usize, port: u16) -> usize {
    if port >= 10000 {
        buf[pos]     = (port / 10000) as u8 + b'0';
        let rem = port % 10000;
        buf[pos + 1] = (rem / 1000) as u8 + b'0';
        let rem = rem % 1000;
        buf[pos + 2] = (rem / 100) as u8 + b'0';
        let rem = rem % 100;
        buf[pos + 3] = (rem / 10) as u8 + b'0';
        buf[pos + 4] = (rem % 10) as u8 + b'0';
        5
    } else if port >= 1000 {
        buf[pos]     = (port / 1000) as u8 + b'0';
        let rem = port % 1000;
        buf[pos + 1] = (rem / 100) as u8 + b'0';
        let rem = rem % 100;
        buf[pos + 2] = (rem / 10) as u8 + b'0';
        buf[pos + 3] = (rem % 10) as u8 + b'0';
        4
    } else if port >= 100 {
        buf[pos]     = (port / 100) as u8 + b'0';
        let rem = port % 100;
        buf[pos + 1] = (rem / 10) as u8 + b'0';
        buf[pos + 2] = (rem % 10) as u8 + b'0';
        3
    } else if port >= 10 {
        buf[pos]     = (port / 10) as u8 + b'0';
        buf[pos + 1] = (port % 10) as u8 + b'0';
        2
    } else {
        buf[pos] = port as u8 + b'0';
        1
    }
}

fn format_ipv4_port(filename: &mut [u8; 256], b0: u8, b1: u8, b2: u8, b3: u8, port: u16) {
    let mut pos: usize = 0;
    pos += write_u8_dec(filename, pos, b0);
    filename[pos] = b'.';
    pos += 1;
    pos += write_u8_dec(filename, pos, b1);
    filename[pos] = b'.';
    pos += 1;
    pos += write_u8_dec(filename, pos, b2);
    filename[pos] = b'.';
    pos += 1;
    pos += write_u8_dec(filename, pos, b3);
    filename[pos] = b':';
    pos += 1;
    pos += write_port(filename, pos, port);
    filename[pos] = 0;
}

fn capture_sockaddr(ctx: &TracePointContext, event: &mut ProbeEvent) {
    let addr_ptr: *const u8 = match unsafe { ctx.read_at::<*const u8>(24) } {
        Ok(ptr) if !ptr.is_null() => ptr,
        _ => {
            str_to_bytes256("(unknown-address)", &mut event.filename);
            event.retval = -20;
            return;
        }
    };

    let mut family: u16 = 0;
    let ret = unsafe {
        bpf_probe_read_user(
            &mut family as *mut u16 as *mut core::ffi::c_void,
            2u32,
            addr_ptr as *const core::ffi::c_void,
        )
    };
    if ret != 0 {
        str_to_bytes256("(unknown-address)", &mut event.filename);
        event.retval = -21;
        return;
    }

    match family {
        2u16 => {
            let mut sin: [u8; 16] = [0u8; 16];
            let ret = unsafe {
                bpf_probe_read_user(
                    &mut sin as *mut u8 as *mut core::ffi::c_void,
                    16u32,
                    addr_ptr as *const core::ffi::c_void,
                )
            };
            if ret != 0 {
                str_to_bytes256("(unknown-address)", &mut event.filename);
                event.retval = -23;
                return;
            }
            let port = u16::from_be(unsafe {
                core::ptr::read_unaligned(sin.as_ptr().add(2) as *const u16)
            });
            format_ipv4_port(
                &mut event.filename,
                sin[4], sin[5], sin[6], sin[7],
                port,
            );
            event.retval = 0;
        }
        10u16 => {
            str_to_bytes256("[IPv6]", &mut event.filename);
            event.retval = 0;
        }
        _ => {
            str_to_bytes256("(unknown-address)", &mut event.filename);
            event.retval = -22;
        }
    }
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo<'_>) -> ! {
    loop {}
}
