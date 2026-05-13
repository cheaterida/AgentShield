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

#[tracepoint]
pub fn agentshield_sys_enter_openat(ctx: TracePointContext) -> u32 {
    match try_openat(&ctx) {
        Ok(()) => 0,
        Err(_) => 1,
    }
}

fn try_openat(ctx: &TracePointContext) -> Result<(), i64> {
    let filename: *const u8 = unsafe { ctx.read_at(16)? };
    let buf = unsafe { bpf_probe_read_user_str(filename, &mut [0u8; 256])? };
    let fname = core::str::from_utf8(&buf).unwrap_or("<invalid>");

    let pid = unsafe { ctx.read_at::<u32>(4)? } as u32;
    let comm = bpf_get_current_comm().unwrap_or([0u8; 16]);

    let event = ProbeEvent {
        pid,
        tid: 0,
        uid: 0,
        comm,
        syscall: str_to_bytes16("openat"),
        filename: str_to_bytes256(fname),
        argv: [0u8; 256],
        retval: 0,
    };

    EVENTS.output(ctx, &event, 0);
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
    let filename: *const u8 = unsafe { ctx.read_at(16)? };
    let buf = unsafe { bpf_probe_read_user_str(filename, &mut [0u8; 256])? };
    let fname = core::str::from_utf8(&buf).unwrap_or("<invalid>");

    let pid = unsafe { ctx.read_at::<u32>(4)? } as u32;
    let comm = bpf_get_current_comm().unwrap_or([0u8; 16]);

    let event = ProbeEvent {
        pid,
        tid: 0,
        uid: 0,
        comm,
        syscall: str_to_bytes16("execve"),
        filename: str_to_bytes256(fname),
        argv: [0u8; 256],
        retval: 0,
    };

    EVENTS.output(ctx, &event, 0);
    Ok(())
}

#[tracepoint]
pub fn agentshield_sys_enter_connect(ctx: TracePointContext) -> u32 {
    let pid = match unsafe { ctx.read_at::<u32>(4) } {
        Ok(v) => v as u32,
        Err(_) => return 1,
    };
    let comm = bpf_get_current_comm().unwrap_or([0u8; 16]);

    let event = ProbeEvent {
        pid,
        tid: 0,
        uid: 0,
        comm,
        syscall: str_to_bytes16("connect"),
        filename: [0u8; 256],
        argv: [0u8; 256],
        retval: 0,
    };

    EVENTS.output(&ctx, &event, 0);
    0
}

#[tracepoint]
pub fn agentshield_sys_enter_bind(ctx: TracePointContext) -> u32 {
    let pid = match unsafe { ctx.read_at::<u32>(4) } {
        Ok(v) => v as u32,
        Err(_) => return 1,
    };
    let comm = bpf_get_current_comm().unwrap_or([0u8; 16]);

    let event = ProbeEvent {
        pid,
        tid: 0,
        uid: 0,
        comm,
        syscall: str_to_bytes16("bind"),
        filename: [0u8; 256],
        argv: [0u8; 256],
        retval: 0,
    };

    EVENTS.output(&ctx, &event, 0);
    0
}

// ── helpers ──

fn bpf_get_current_comm() -> Result<[u8; 16], i64> {
    aya_ebpf::helpers::bpf_get_current_comm()
}

fn str_to_bytes16(s: &str) -> [u8; 16] {
    let mut buf = [0u8; 16];
    let bytes = s.as_bytes();
    let len = bytes.len().min(15);
    buf[..len].copy_from_slice(&bytes[..len]);
    buf
}

fn str_to_bytes256(s: &str) -> [u8; 256] {
    let mut buf = [0u8; 256];
    let bytes = s.as_bytes();
    let len = bytes.len().min(255);
    buf[..len].copy_from_slice(&bytes[..len]);
    buf
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo<'_>) -> ! {
    loop {}
}
