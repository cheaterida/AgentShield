# AgentShield QuickStart

## 架构一览

```
Windows 主机 (100.123.70.98)                Linux VM (100.68.106.60)
─────────────────────────────────────       ─────────────────────────────
management-server :8080 (Go)                agent-runtime (systemd)
serve-web.py :8081 (Python, Trace API)       ├─ eBPF 探针 (4 tracepoints)
OPA :8181 (Docker)                          ├─ Supervisor (管理 Hermes)
Redis :6379 (Docker)                        ├─ Hermes AI Agent
ClickHouse :8123 (本地)                      └─ agentshield_tracer.py
React SPA :5173 (Vite)
```

两条数据链路：
- **链路 A (LLM 观测)**: Hermes → tracer SDK → serve-web.py → ClickHouse → TracesPage
- **链路 B (安全审计)**: eBPF 探针 → agent-runtime → management-server → OPA 策略评估 → SecurityEvents

---

## 前置条件

- Docker Desktop 运行中
- Go 1.25+（Windows）
- Node.js 18+（Windows）
- Python 3.11+（Windows）
- ClickHouse 已安装并运行在 localhost:8123（Windows）
- Tailscale 连接 Windows 主机 ↔ Linux VM

---

## 一、服务端启动（Windows 主机）

### 1.1 初始化 ClickHouse 表

```powershell
curl -X POST 'http://localhost:8123/' --data-binary @observability/schemas/clickhouse_spans.sql
```

### 1.2 启动 Docker 服务（OPA + Redis）

```powershell
cd C:\Users\Acer\Desktop\AgentShield

# 启动 OPA + Redis（management-server 我们直接跑 Go，方便调试）
docker compose -f deployments/docker-compose.yml --profile policy up -d opa redis
```

验证：
```powershell
curl -s http://localhost:8181/v1/data/agentshield/audit | python -m json.tool
# 应返回 OPA 策略规则

docker ps --filter "name=redis" --format "{{.Status}}"
# 应显示 "Up X seconds (healthy)"
```

### 1.3 启动 Go 管理服务器（终端 1）

```powershell
cd C:\Users\Acer\Desktop\AgentShield\management-server

$env:AGENTSHIELD_HTTP_ADDR = ":8080"
$env:AGENTSHIELD_DB_DRIVER = "sqlite"
$env:AGENTSHIELD_SQLITE_PATH = "./data/agentshield.db"
$env:AGENTSHIELD_OPA_BASE_URL = "http://localhost:8181"
$env:AGENTSHIELD_REDIS_ADDR = "localhost:6379"
$env:AGENTSHIELD_LOG_LEVEL = "info"

go run cmd/server/main.go
```

看到 `management-server HTTP listening addr=:8080` 即启动成功。

### 1.4 启动 Python 代理（终端 2）

```powershell
cd C:\Users\Acer\Desktop\AgentShield\management-server

$env:CLICKHOUSE_HOST = "localhost"
$env:CLICKHOUSE_PORT = "8123"

python serve-web.py 8081
```

这个进程同时提供：
- `http://localhost:8081/` → React SPA（生产构建）或代理到 Vite
- `http://localhost:8081/api/v1/spans` → ClickHouse span 写入
- `http://localhost:8081/api/v1/traces` → ClickHouse trace 查询
- 其他 `/api/*` → 透明代理到 Go :8080

### 1.5 启动前端开发服务器（终端 3）

```powershell
cd C:\Users\Acer\Desktop\AgentShield\management-server\web
npm run dev
```

浏览器打开 `http://localhost:5173`。

> **注意**：Vite dev server 的 `/api` 请求需要在 `vite.config.ts` 中配置代理到 `http://localhost:8081`。如果前端 API 调用失败，检查 proxy 配置。

---

## 二、VM 端启动（Linux VM）

### 2.1 构建 agent-runtime 二进制（在 Windows 主机上）

```powershell
cd C:\Users\Acer\Desktop\AgentShield
bash agent-runtime/build-linux.sh
```

首次构建 ~15-20min（Docker 镜像 ~5min + eBPF 编译 ~3min + Rust 编译 ~10min）。
后续增量构建 ~2-5min。

产物：`agent-runtime/bin/agent-runtime`（static musl x86_64）

### 2.2 部署到 VM

```powershell
# 确认 env.vm 中配置正确后再部署
bash agent-runtime/deploy-vm.sh root 100.68.106.60
```

这个脚本自动完成：
1. `scp` 二进制、env 文件、systemd unit 到 VM
2. 安装到 `/usr/local/bin/agent-runtime`、`/etc/agentshield/env`
3. `systemctl daemon-reload && systemctl enable agent-runtime`
4. `systemctl start agent-runtime`

### 2.3 验证 VM 端运行状态

```bash
# SSH 到 VM
ssh root@100.68.106.60

# 查看服务状态
systemctl status agent-runtime

# 实时日志
journalctl -u agent-runtime -f

# 应看到：
#   "eBPF probes configured (linux real mode)"
#   "policy cache ready"
#   "agent-runtime running"
```

### 2.4 确认 Hermes Agent 接入 Tracer

在 Hermes 的启动代码中集成 SDK：

```python
# 将 sdk/python/agentshield_tracer.py 放到 VM 上 Hermes 可 import 的位置
from agentshield_tracer import init_tracer, wrap_openai

init_tracer(
    agent_id="vm-agent-001",
    family_group_id="default",
    management_url="http://100.123.70.98:8081"  # Windows 主机 Tailscale IP
)

# 包裹 OpenAI client，所有 LLM 调用自动埋点
client = wrap_openai(openai.OpenAI())
```

---

## 三、验证全链路

### 3.1 确认 agent 心跳

浏览器打开 `http://localhost:5173` → Dashboard 页面 → 应看到 `vm-agent-001` 在线，心跳时间在 10s 内。

### 3.2 确认安全事件（链路 B）

触发 Hermes 执行一次任务（发送消息），然后：
- Dashboard → SecurityEventsPage → 应有新事件出现
- 事件应有正确的 risk_level（low / medium / high / critical）

### 3.3 确认 LLM Trace（链路 A）

Hermes 调用 LLM 后：
- Dashboard → TracesPage → 应看到 trace 记录
- 展开 trace 可看到 prompt / completion 详情

---

## 四、常用运维命令

### 服务端

```powershell
# 查看 Docker 服务状态
docker compose -f deployments/docker-compose.yml ps

# 重启 OPA（策略更新后）
docker restart opa

# 清空 SQLite 数据库重新开始
docker exec management-server rm -f /data/agentshield.db
# 如果用 Go 直接跑: Remove-Item management-server/data/agentshield.db

# 查看 Redis 缓存
docker exec redis redis-cli KEYS "*"

# Go 管理服务器编译（不用 docker）
cd management-server; go build -o bin/server.exe cmd/server/main.go; .\bin\server.exe
```

### VM 端

```bash
# SSH 到 VM
ssh root@100.68.106.60

# 查看实时日志
journalctl -u agent-runtime -f

# 重启 agent-runtime
systemctl restart agent-runtime

# 停止 agent-runtime
systemctl stop agent-runtime

# 查看最近 100 条日志
journalctl -u agent-runtime -n 100 --no-pager

# 检查 eBPF 探针是否加载
journalctl -u agent-runtime | grep "ebpf"

# 确认进程过滤生效
journalctl -u agent-runtime | grep "eBPF events in last"
```

### 更新 VM 端部署

```powershell
# 1. 重新构建
bash agent-runtime/build-linux.sh

# 2. 重新部署
bash agent-runtime/deploy-vm.sh root 100.68.106.60

# 3. SSH 验证
ssh root@100.68.106.60 'systemctl status agent-runtime'
```

---

## 五、配置文件速查

### 服务端环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AGENTSHIELD_HTTP_ADDR` | `:8080` | Go 管理服务器监听地址 |
| `AGENTSHIELD_DB_DRIVER` | `sqlite` | 数据库驱动 |
| `AGENTSHIELD_SQLITE_PATH` | `./data/agentshield.db` | SQLite 文件路径 |
| `AGENTSHIELD_OPA_BASE_URL` | `http://localhost:8181` | OPA 策略引擎地址 |
| `AGENTSHIELD_REDIS_ADDR` | `localhost:6379` | Redis 缓存地址 |
| `AGENTSHIELD_LOG_LEVEL` | `info` | 日志级别 |

### VM 端环境变量（`/etc/agentshield/env`）

| 变量 | 说明 |
|------|------|
| `AGENTSHIELD_AGENT_ID` | Agent 唯一标识 |
| `AGENTSHIELD_MGMT_ADDR` | 管理服务器地址（Windows 主机 Tailscale IP） |
| `AGENTSHIELD_PROBE_COMM_ALLOWLIST` | eBPF 进程白名单（逗号分隔） |
| `AGENTSHIELD_HEARTBEAT_INTERVAL_SECS` | 心跳间隔 |
| `RUST_LOG` | 日志级别 |

---

## 六、关键文件路径

| 文件 | 路径 |
|------|------|
| OPA 审计策略 | `security-policy/policies/agentshield/audit.rego` |
| OPA 公共常量 | `security-policy/policies/agentshield/common.rego` |
| Docker Compose | `deployments/docker-compose.yml` |
| VM 环境配置 | `agent-runtime/env.vm` |
| 构建脚本 | `agent-runtime/build-linux.sh` |
| 部署脚本 | `agent-runtime/deploy-vm.sh` |
| systemd unit | `agent-runtime/agent-runtime.service` |
| ClickHouse schema | `observability/schemas/clickhouse_spans.sql` |
| Python tracer SDK | `sdk/python/agentshield_tracer.py` |
