#![no_std]
#![no_main]
#![allow(linker_messages)]

use aya_ebpf::{
    helpers::bpf_probe_read_user_str_bytes,
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

    event.tid = 0;
    event.uid = 0;
    event.retval = 0;

    let filename: *const u8 = unsafe { ctx.read_at(16)? };
    let fbytes = unsafe { bpf_probe_read_user_str_bytes(filename, &mut event.filename)? };
    // ensure null-termination for safety
    if fbytes.len() < 256 {
        event.filename[fbytes.len()] = 0;
    }

    event.pid = unsafe { ctx.read_at::<u32>(4)? } as u32;
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

    event.tid = 0;
    event.uid = 0;
    event.retval = 0;

    let filename: *const u8 = unsafe { ctx.read_at(16)? };
    let fbytes = unsafe { bpf_probe_read_user_str_bytes(filename, &mut event.filename)? };
    if fbytes.len() < 256 {
        event.filename[fbytes.len()] = 0;
    }

    event.pid = unsafe { ctx.read_at::<u32>(4)? } as u32;
    event.comm = bpf_get_current_comm().unwrap_or([0u8; 16]);
    str_to_bytes16("execve", &mut event.syscall);
    zero_bytes256(&mut event.argv);

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

    event.pid = match unsafe { ctx.read_at::<u32>(4) } {
        Ok(v) => v as u32,
        Err(_) => return 1,
    };
    event.tid = 0;
    event.uid = 0;
    event.retval = 0;
    event.comm = bpf_get_current_comm().unwrap_or([0u8; 16]);
    str_to_bytes16("connect", &mut event.syscall);
    zero_bytes256(&mut event.filename);
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

    event.pid = match unsafe { ctx.read_at::<u32>(4) } {
        Ok(v) => v as u32,
        Err(_) => return 1,
    };
    event.tid = 0;
    event.uid = 0;
    event.retval = 0;
    event.comm = bpf_get_current_comm().unwrap_or([0u8; 16]);
    str_to_bytes16("bind", &mut event.syscall);
    zero_bytes256(&mut event.filename);
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

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo<'_>) -> ! {
    loop {}
}
