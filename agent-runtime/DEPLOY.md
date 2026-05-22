# AgentShield Agent Runtime — VM 部署指南

## 前置条件

- Linux x86_64 VM（Ubuntu 22.04+ / Debian 12+ / RHEL 9+）
- management-server 已部署并可访问（HTTP）
- （可选）Hermes AI agent 可执行文件
- root 或 sudo 权限

## 1. 二进制部署

```bash
# 从构建机复制到 VM
scp agent-runtime/bin/agent-runtime root@<vm-ip>:/usr/local/bin/agent-runtime
chmod +x /usr/local/bin/agent-runtime
```

本二进制为静态 musl 编译，无运行时依赖，可直接执行。

## 2. 目录准备

```bash
mkdir -p /var/lib/agentshield/checkpoints   # 快照存储
mkdir -p /var/lib/agentshield/policies      # OPA 策略缓存
mkdir -p /workspace                         # Hermes 工作目录（被 inotify 监控）
```

## 3. 环境变量配置

创建 `/etc/agentshield/env`：

```bash
# ── 必填 ──
AGENTSHIELD_AGENT_ID="prod-agent-001"
AGENTSHIELD_FAMILY_GROUP_ID="prod-fg"
AGENTSHIELD_DISPLAY_NAME="Production Agent"
AGENTSHIELD_MGMT_ADDR="http://<management-server-ip>:8080"

# ── Checkpoint（Track A） ──
AGENTSHIELD_CHECKPOINT_ENABLED=true
AGENTSHIELD_CHECKPOINT_DIR=/var/lib/agentshield/checkpoints
AGENTSHIELD_CHECKPOINT_MAX_COUNT=50
AGENTSHIELD_WORKSPACE_DIR=/workspace
AGENTSHIELD_CHECKPOINT_INTERVAL_STEPS=1

# ── Hermes agent（可选，不设则不启动 AI agent 进程） ──
AGENTSHIELD_HERMES_BINARY=/usr/local/bin/hermes

# ── eBPF 探针（可选） ──
AGENTSHIELD_PROBE_ENABLED=false
# AGENTSHIELD_EBPF_OBJECT=/usr/local/lib/agentshield/ebpf/agentshield-ebpf.o

# ── 日志 ──
RUST_LOG=info
```

## 4. systemd 服务

创建 `/etc/systemd/system/agent-runtime.service`：

```ini
[Unit]
Description=AgentShield Agent Runtime
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/agentshield/env
ExecStart=/usr/local/bin/agent-runtime
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=agent-runtime

# 安全加固
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/var/lib/agentshield /workspace
ReadOnlyPaths=/usr/local/bin

[Install]
WantedBy=multi-user.target
```

启动：

```bash
systemctl daemon-reload
systemctl enable --now agent-runtime
systemctl status agent-runtime
journalctl -u agent-runtime -f
```

## 5. Hermes Agent 要求

Hermes 是被 agent-runtime 监管的 AI agent 程序。要求：

- 任意可执行文件（二进制 / 脚本 / Python 程序）
- 工作目录为 `$AGENTSHIELD_WORKSPACE_DIR`（/workspace）
- 所有文件读写应在工作目录内，以便 checkpoint 追踪
- stdout/stderr 会被 agent-runtime 收集并写入日志

最小示例（`/usr/local/bin/hermes`）：

```python
#!/usr/bin/env python3
"""最小 Hermes agent — 在工作区读写文件，执行 AI 任务。"""
import os, time
os.chdir("/workspace")
while True:
    with open("output.txt", "a") as f:
        f.write(f"step at {time.time()}\n")
    time.sleep(60)
```

## 6. 验证

### 6.1 启动检查

```bash
journalctl -u agent-runtime | grep -E "checkpoint|hermes"
```

预期输出：
```
checkpoint manager initialized
hermes agent supervised
agent-runtime running
```

### 6.2 手动创建 checkpoint（通过 management-server API）

```bash
# 向 agent 下发创建快照指令
curl -X POST http://<mgmt-ip>:8080/api/v1/agents/heartbeat \
  -H "Content-Type: application/json" \
  -d '{"agent_id": "prod-agent-001", "suggested_action": "create_checkpoint"}'
```

检查 VM 上：
```bash
ls /var/lib/agentshield/checkpoints/
# 应出现 hb-<timestamp> 目录
```

### 6.3 端到端回滚验证

```bash
# 1. 在工作区创建文件
echo "important data" > /workspace/data.txt

# 2. 通过 API 创建 checkpoint
curl ... -d '{"suggested_action": "create_checkpoint"}'

# 3. 模拟异常：损坏文件
echo "MALICIOUS CONTENT" > /workspace/data.txt
touch /workspace/virus.sh

# 4. 通过 API 触发回滚（使用上面产生的 checkpoint_id）
curl ... -d '{"suggested_action": "rollback_to:hb-<timestamp>"}'

# 5. 验证恢复
cat /workspace/data.txt
# → "important data"
ls /workspace/virus.sh
# → 文件不存在
```

## 7. 故障排查

| 症状 | 可能原因 | 解决 |
|------|---------|------|
| `checkpoint manager init failed` | checkpoint 目录无写权限 | `chmod 755 /var/lib/agentshield/checkpoints` |
| `hermes start failed` | Hermes 二进制不存在或不可执行 | `chmod +x /usr/local/bin/hermes` |
| `checkpoint disabled by config` | `AGENTSHIELD_CHECKPOINT_ENABLED` 未设为 true | 检查 `/etc/agentshield/env` |
| checkpoint 列表为空 | 尚未创建过 checkpoint | 手动触发 `create_checkpoint` |
| 回滚后文件未恢复 | 回滚前的文件未被 checkpoint 记录 | 确保在修改前已创建 checkpoint |
| 二进制无法启动 | musl 兼容性问题 | `ldd /usr/local/bin/agent-runtime` 应显示 `not a dynamic executable`（正常） |
