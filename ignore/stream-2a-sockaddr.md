# Stream 2A: eBPF connect/bind sockaddr 捕获

> **终端标识**: `stream-2a-sockaddr`
> **优先级**: 🔴 Critical
> **依赖**: Task 2.6（魔数验证）已完成 — `ProbeEvent` 结构体头部已有 `magic: u32` 字段
> **预计工时**: 2-3h

---

## 前置状态确认

Task 2.6 已合入，当前代码状态：
- `ProbeEvent` 第一个字段为 `pub magic: u32`（偏移 0）
- 所有 4 个 eBPF handler 已设置 `event.magic = 0xE5;`
- `probe_manager.rs` 已添加魔数校验逻辑

**你需要操作的 handler 是 `connect`（第 141 行）和 `bind`（第 165 行），它们已经包含：
- `event.magic = 0xE5;`
- `bpf_get_current_pid_tgid()` / `bpf_get_current_uid_gid()` 调用
- `zero_bytes256(&mut event.filename);` ← **这是你要替换的部分**

---

## 任务清单

### Task 2.3 — connect/bind 捕获 sockaddr 地址

**文件**: `ebpf-probes/agentshield-ebpf/src/main.rs`

**问题**: `connect` 和 `bind` handler 的 `filename` 始终为零 — 未捕获目标地址。

**syscall 参数布局**（64-bit tracepoint）:

```
sys_enter_connect(int sockfd, const struct sockaddr *addr, socklen_t addrlen):
  offset 16 = sockfd (unsigned long)
  offset 24 = addr (struct sockaddr *)
  offset 32 = addrlen (socklen_t / unsigned long)

sys_enter_bind(int sockfd, const struct sockaddr *addr, socklen_t addrlen):
  offset 16 = sockfd (unsigned long)
  offset 24 = addr (struct sockaddr *)
  offset 32 = addrlen (socklen_t / unsigned long)
```

**核心挑战**: 从 userspace 读取 `struct sockaddr` → 判断地址族 → 格式化为 IP:Port。

**sockaddr 结构体**:
```c
struct sockaddr {
    unsigned short sa_family;  // 2 bytes
    char sa_data[14];          // 14 bytes
};

struct sockaddr_in {           // AF_INET (2)
    unsigned short sin_family; // 2 bytes, = 2
    unsigned short sin_port;   // 2 bytes (network byte order)
    struct in_addr sin_addr;   // 4 bytes
    char sin_zero[8];          // 8 bytes
};
// Total: 16 bytes

struct sockaddr_in6 {          // AF_INET6 (10)
    unsigned short sin6_family; // 2 bytes, = 10
    unsigned short sin6_port;   // 2 bytes
    // ... 24 bytes total
};
```

**修复步骤**:

1. **读取 `addr` 指针**:
   ```rust
   // 从 tracepoint context 读取 struct sockaddr* 指针
   let addr_ptr: *const u8 = match unsafe { ctx.read_at::<*const u8>(24) } {
       Ok(ptr) => ptr,
       Err(_) => {
           event.retval = -20;  // debug: 无法读取 addr 指针
           // 跳过地址读取，继续输出事件
           // (不要 ? 传播错误，connect/bind handler 没有返回 Result)
       }
   };
   ```

2. **读取 `sa_family`**（前 2 字节）:
   ```rust
   let mut family: u16 = 0;
   let ret = unsafe {
       bpf_probe_read_user(
           &mut family as *mut u16 as *mut core::ffi::c_void,
           2u32,
           addr_ptr as *const core::ffi::c_void,
       )
   };
   ```

3. **按地址族分支处理**:
   - `AF_INET (2)`: 读取完整 `sockaddr_in`（16 字节），提取 IP + Port
   - `AF_INET6 (10)`: 标记 `[IPv6]`（256 字节不够格式化完整 IPv6 地址）
   - 其他 / 读取失败: 写 `"(unknown-address)"`

4. **格式化 IP:Port**（AF_INET 情况）:
   ```rust
   // sockaddr_in 布局: family(2) + port(2) + addr(4) + zero(8)
   let mut sin_bytes: [u8; 16] = [0; 16];
   // 读取完整 16 字节
   let ret = unsafe { bpf_probe_read_user(
       &mut sin_bytes as *mut u8 as *mut core::ffi::c_void,
       16u32,
       addr_ptr as *const core::ffi::c_void,
   )};
   if ret == 0 {
       let port = u16::from_be(unsafe {
           core::ptr::read_unaligned(sin_bytes.as_ptr().add(2) as *const u16)
       });
       let ip_b0 = sin_bytes[4];
       let ip_b1 = sin_bytes[5];
       let ip_b2 = sin_bytes[6];
       let ip_b3 = sin_bytes[7];
       // 格式化: "XXX.XXX.XXX.XXX:PPPPP\0"
       // 使用简单逐位格式化（BPF verifier 禁止循环，展开即可）
   }
   ```

5. **IP 格式化辅助函数**（BPF 兼容 — 禁止循环，手动展开）:
   ```rust
   fn write_ip_port(buf: &mut [u8; 256], b0: u8, b1: u8, b2: u8, b3: u8, port: u16) {
       // 手动展开，避免 BPF verifier 拒绝循环
       // 为简单起见，只填充前 22 字节左右，其余留给原有数据
       // 可复用项目已有的 u8_to_dec_str 逻辑
   }
   ```

   **重要**: BPF verifier 对循环极其严格。格式化 IP 地址必须手动展开每个数字位。建议实现：
   ```rust
   // 写 u8 的十进制表示到 buf[pos..]，返回写入的字节数
   fn write_u8_dec(buf: &mut [u8], pos: usize, val: u8) -> usize
   // 通过 if/else 展开处理 0-99, 100-255
   ```

6. **错误回退**: 任何步骤失败 → 写入 `"(unknown-address)"` 到 `filename`。

---

## 调试信息（event.retval 扩展编码）

| retval | 含义 |
|--------|------|
| `0` | 地址成功捕获 |
| `-20` | `ctx.read_at(24)` 失败 — 无法读取 addr 指针 |
| `-21` | `bpf_probe_read_user` 读取 sa_family 失败 |
| `-22` | `sa_family` 不是 AF_INET 或 AF_INET6 |
| `-23` | 读取 sockaddr_in 失败 |

---

## 协作约束

### 禁止事项
- ❌ 禁止修改 `ProbeEvent` 结构体字段顺序
- ❌ 禁止在 eBPF 代码中使用 for/while 循环（BPF verifier 拒绝）
- ❌ 禁止在 handler 中分配超过 512 字节栈空间（已有 `BUF` PerCpuArray，继续复用）
- ❌ 禁止修改 openat 和 execve handler
- ❌ 禁止修改 `action` 字符串映射（connect → `network_connect`, bind → `socket_create`）

### 允许事项
- ✅ 替换 `zero_bytes256(&mut event.filename)` 为实际地址捕获
- ✅ 修改 connect 和 bind handler 内部逻辑
- ✅ 添加新的辅助函数（需放在 `// ── helpers ──` 区域）

---

## 编译验证

**前提**: Docker Desktop 运行中。

### Cargo 缓存加速

`-v "agent-shield-cargo:/usr/local/cargo"` 将编译工具链和依赖缓存于 Docker volume 中：

| 数据 | 缓存位置 | 首次 | 后续 |
|------|---------|------|------|
| nightly rustc + rust-src | volume | ~5 min | 0s（复用） |
| bpf-linker 编译产物 | volume | ~5 min | 0s（复用） |
| crate 依赖下载 | volume | ~2 min | 0s（复用） |
| aya-ebpf 编译产物 | volume | ~1 min | 0s（复用） |
| LLVM dev 包 | 容器内（不缓存） | ~30s | ~30s |
| **总计** | | **~15 min** | **~40s** |

如果 volume 已存在（被其他终端先初始化过），你只需等待 ~40 秒。三个终端并发编译时，cargo 文件锁自动排队，不会冲突。

```powershell
docker run --rm `
  -v "C:\Users\Acer\Desktop\AgentShield:/build" `
  -v "agent-shield-cargo:/usr/local/cargo" `
  -w /build `
  rust:1.91-bookworm sh -c "
    apt-get update -qq && apt-get install -y -qq llvm-dev 2>&1 | tail -1
    cargo +nightly build -p agentshield-ebpf --target bpfel-unknown-none --release 2>&1
    echo '=== Bytecode ==='
    ls -lh /build/target/bpfel-unknown-none/release/agentshield-ebpf
    file /build/target/bpfel-unknown-none/release/agentshield-ebpf
    strings /build/target/bpfel-unknown-none/release/agentshield-ebpf | grep agentshield_sys_enter
"
```

**验证标准**:
- [ ] `cargo build` 零错误零警告
- [ ] 字节码文件非空（8KB+）
- [ ] `strings` 输出包含所有 4 个 tracepoint 符号
- [ ] `strings` 中包含 `"(unknown-address)"` 回退字符串

---

## 设计参考

已有的 `bpf_probe_read_user` 调用模式（来自 execve handler Task 2.2）:
```rust
let ret = unsafe {
    bpf_probe_read_user(
        &mut arg0 as *mut *const u8 as *mut core::ffi::c_void,
        core::mem::size_of::<*const u8>() as u32,
        argv_ptr as *const core::ffi::c_void,
    )
};
```

导入已存在于 main.rs 第 11 行:
```rust
use aya_ebpf::helpers::gen::bpf_probe_read_user;
```
