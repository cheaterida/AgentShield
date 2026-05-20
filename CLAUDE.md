# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.


Behavioral guidelines to reduce common LLM coding mistakes. Merge with project-specific instructions as needed.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

---

**These guidelines are working if:** fewer unnecessary changes in diffs, fewer rewrites due to overcomplication, and clarifying questions come before implementation rather than after mistakes.
## Build & Test Commands

```bash
# Protobuf generation (requires buf CLI)
make proto-gen          # generate Go stubs from shared/proto/
make proto-lint         # lint proto files

# Go services (independent go.mod per service)
make build-management   # management-server -> management-server/bin/
make build-gateway      # gateway -> gateway/bin/
make build-error-handler # error-handler -> error-handler/bin/
make test-management    # go test -race -count=1 ./... (management-server)
make lint-go            # go vet across all three Go modules

# Rust workspace (root Cargo.toml)
make build-agent-runtime # cargo build -p agent-runtime --release
make build-ebpf          # cargo build -p agentshield-ebpf --target bpfel-unknown-none (requires bpf-linker + Linux headers)
make test-agent-runtime  # cargo test -p agent-runtime
make lint-rust           # cargo clippy --workspace

# Frontend (management-server/web/)
make build-frontend      # npm ci && npm run build
make dev-frontend        # npm run dev (Vite dev server)

# Combined
make test                # test-management + test-agent-runtime
make lint                # proto-lint + lint-go + lint-rust
make check               # proto-gen + build-management + build-frontend

# Docker Compose (deployments/docker-compose.yml, 7 profiles)
make docker-up           # core profile: management-server + OPA
make docker-up-full      # full profile: envoy + gateway + management-server + OPA + ml-pipeline + bridge
make docker-down         # compose down -v

# Cross-compile agent-runtime for Linux (musl static + eBPF bytecode, requires Docker)
bash agent-runtime/build-linux.sh

# Python ML pipeline
cd ml-pipeline && pip install -e ".[dev]"   # install with dev deps (ruff, pytest)
cd ml-pipeline && pytest                     # run tests
cd ml-pipeline && ruff check                 # lint
cd ml-pipeline && uvicorn agentshield_ml.api.main:app --port 8090  # run inference API
```

## Architecture

AgentShield is a closed-loop AI agent governance platform for SMEs. Four control chains:

1. **Request chain**: External webhook/API → `gateway` (HMAC auth, rate limit, relay) → `management-server`
2. **Verification chain**: Agent action → `ebpf-probes` (kernel tracepoints) → `agent-runtime` (event buffer → batch upload) → `management-server`
3. **Supervision chain**: `management-server` receives events → Risk Engine (rules + EMA + optional ML/GNN) → OPA policy evaluation → alert generation → WebSocket push to React SPA
4. **Disposition chain**: `error-handler` polls open alerts → severity dispatch (low aggregate / medium rollback / high degrade / critical circuit-break)

Data flows between components via REST (primary), WebSocket (real-time push), and planned gRPC streaming.

## Repository Layout

| Directory | Language | Role |
|-----------|----------|------|
| `management-server/` | Go 1.25 | Central management: HTTP API, gRPC, risk engine, OPA client, SQLite store, React SPA |
| `agent-runtime/` | Rust (Tokio) | End-side daemon: heartbeat, event buffer/upload, eBPF probe mgmt, policy cache, process supervisor |
| `ebpf-probes/` | Rust/Aya (`#![no_std]`) | Kernel tracepoints (openat, execve, connect, bind), plus standalone loader |
| `gateway/` | Go 1.22 | Edge gateway: Envoy TLS termination, HMAC webhook auth, per-tenant rate limiting |
| `error-handler/` | Go 1.22 | Tiered incident response: aggregate/rollback/degrade/circuit-break |
| `ml-pipeline/` | Python 3.11+ | FastAPI inference (CAE embeddings, GNN anomaly detection via PyTorch + DGL) |
| `bridge/` | Python | Polls ClickHouse `agentshield.spans`, converts spans to audit events |
| `sdk/` | Python | Client tracer: `trace_llm_call()`, `wrap_openai()`, span flush to management-server |
| `shared/` | Protobuf | gRPC service definitions + generated Go stubs (buf) |
| `security-policy/` | Rego + Cypher | OPA policies (authz + audit), Neo4j graph schema placeholder |
| `kernel-hardening/` | SELinux | MAC type enforcement policy |
| `deployments/` | YAML | Docker Compose (7 profiles, 8 services), env files |
| `scripts/` | Shell | Dev/CI helper scripts |

## Key Design Decisions

- **Rust workspace** at repo root (`Cargo.toml`): members are `agent-runtime`, `ebpf-probes/agentshield-ebpf`, `ebpf-probes/agentshield-ebpf-common`, `ebpf-probes/agentshield-loader`. Shared deps via `[workspace.dependencies]`.
- **Go modules are independent** — each has its own `go.mod` with module path `agentshield.dev/agentshield/<name>`. No Go workspace; build each separately.
- **Risk scoring** uses a hybrid model: rule-based scores (sensitive path 0.5, write 0.2, network 0.3) → EMA smoothing (alpha=0.3) → optional ML hybrid (weight ramps 0.1→0.7 based on training data volume) → threshold alerts (medium 0.3, high 0.6, critical 0.8).
- **eBPF probe events** share a `#![no_std]` `ProbeEvent` struct via `agentshield-ebpf-common` crate, used by both kernel-side BPF and user-space agent-runtime.
- **Store interface** (`management-server/internal/store/store.go`) has SQLite and in-memory backends; PostgreSQL is planned. Switch via `AGENTSHIELD_DB_DRIVER` env var.
- **Config is env-var driven** across all components (`AGENTSHIELD_*` prefix). No config files at runtime.
