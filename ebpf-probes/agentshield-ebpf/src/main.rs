#![no_std]
#![no_main]

use aya_ebpf::{
    helpers::bpf_probe_read_user_str,
    macros::{map, tracepoint},
    maps::PerfEventArray,
    programs::TracePointContext,
};

use agentshield_ebpf_common::ProbeEvent;

#[map]
static EVENTS: PerfEventArray<ProbeEvent> = PerfEventArray::new();

/// Tracepoint: sys_enter_openat — file open / access
#[tracepoint]
pub fn agentshield_sys_enter_openat(ctx: TracePointContext) -> u32 {
    match try_openat(&ctx) {
        Ok(()) => 0,
        Err(_) => 1,
    }
}

fn try_openat(ctx: &TracePointContext) -> Result<(), i64> {
    let filename: *const u8 = unsafe { ctx.read_at(16)? }; // arg2: filename ptr
    let buf = unsafe { bpf_probe_read_user_str(filename, &mut [0u8; 256])? };
    let fname = core::str::from_utf8(&buf).unwrap_or("<invalid>");

    let pid = unsafe { ctx.read_at::<u32>(4)? } as u32; // common_pid
    let comm_bytes = bpf_get_current_comm()?;
    let comm = core::str::from_utf8(&comm_bytes).unwrap_or("<unknown>");

    let event = ProbeEvent {
        pid,
        tid: 0,
        uid: 0,
        comm: copy_str(comm),
        syscall: "openat",
        filename: copy_str(fname),
        argv: ['\0'; 256],
        retval: 0,
    };

    EVENTS.output(ctx, &event, 0);
    Ok(())
}

/// Tracepoint: sys_enter_execve — process execution
#[tracepoint]
pub fn agentshield_sys_enter_execve(ctx: TracePointContext) -> u32 {
    match try_execve(&ctx) {
        Ok(()) => 0,
        Err(_) => 1,
    }
}

fn try_execve(ctx: &TracePointContext) -> Result<(), i64> {
    let filename: *const u8 = unsafe { ctx.read_at(16)? };
    let buf = unsafe { bpf_probe_read_user_str(filename, &mut [0u8; 256])? };
    let fname = core::str::from_utf8(&buf).unwrap_or("<invalid>");

    let pid = unsafe { ctx.read_at::<u32>(4)? } as u32;
    let comm_bytes = bpf_get_current_comm()?;
    let comm = core::str::from_utf8(&comm_bytes).unwrap_or("<unknown>");

    let event = ProbeEvent {
        pid,
        tid: 0,
        uid: 0,
        comm: copy_str(comm),
        syscall: "execve",
        filename: copy_str(fname),
        argv: ['\0'; 256],
        retval: 0,
    };

    EVENTS.output(ctx, &event, 0);
    Ok(())
}

/// Tracepoint: sys_enter_connect — network outbound connection
#[tracepoint]
pub fn agentshield_sys_enter_connect(ctx: TracePointContext) -> u32 {
    let pid = match unsafe { ctx.read_at::<u32>(4) } {
        Ok(v) => v as u32,
        Err(_) => return 1,
    };
    let comm_bytes = bpf_get_current_comm().unwrap_or([0u8; 16]);
    let comm = core::str::from_utf8(&comm_bytes).unwrap_or("<unknown>");

    let event = ProbeEvent {
        pid,
        tid: 0,
        uid: 0,
        comm: copy_str(comm),
        syscall: "connect",
        filename: ['\0'; 256],
        argv: ['\0'; 256],
        retval: 0,
    };

    EVENTS.output(&ctx, &event, 0);
    0
}

/// Tracepoint: sys_enter_bind — socket bind (listening)
#[tracepoint]
pub fn agentshield_sys_enter_bind(ctx: TracePointContext) -> u32 {
    let pid = match unsafe { ctx.read_at::<u32>(4) } {
        Ok(v) => v as u32,
        Err(_) => return 1,
    };
    let comm_bytes = bpf_get_current_comm().unwrap_or([0u8; 16]);
    let comm = core::str::from_utf8(&comm_bytes).unwrap_or("<unknown>");

    let event = ProbeEvent {
        pid,
        tid: 0,
        uid: 0,
        comm: copy_str(comm),
        syscall: "bind",
        filename: ['\0'; 256],
        argv: ['\0'; 256],
        retval: 0,
    };

    EVENTS.output(&ctx, &event, 0);
    0
}

// ── helpers ──

fn copy_str(s: &str) -> [u8; 256] {
    let mut buf = [0u8; 256];
    let bytes = s.as_bytes();
    let len = if bytes.len() > 255 { 255 } else { bytes.len() };
    buf[..len].copy_from_slice(&bytes[..len]);
    buf
}

fn bpf_get_current_comm() -> Result<[u8; 16], i64> {
    let mut comm = [0u8; 16];
    let ret = unsafe {
        aya_ebpf::helpers::bpf_get_current_comm(&mut comm as *mut _ as *mut u8, 16)
    };
    if ret == 0 {
        Ok(comm)
    } else {
        Err(-1)
    }
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo<'_>) -> ! {
    loop {}
}
