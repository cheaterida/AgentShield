# ── AgentShield Agent Runtime Docker Image ──
# Two-stage build: Rust agent (musl static) → minimal Alpine runtime
# eBPF bytecode is built separately (requires kernel-specific bpf-linker).
# Without eBPF, agent-runtime runs in demo mode — still useful for testing.
#
# Build:  docker build -t agentshield/agent:latest -f agent-runtime/Dockerfile .
# Run:    docker run -d --pid=host --privileged \
#           -e AGENTSHIELD_AGENT_ID="worker-01" \
#           -e AGENTSHIELD_FAMILY_GROUP_ID="fg-001" \
#           -e AGENTSHIELD_MGMT_ADDR="http://<mgmt-ip>:8080" \
#           agentshield/agent:latest

# ── Stage 1: Rust builder for agent-runtime ──
FROM rust:1.80-bookworm AS rust-builder
RUN apt-get update && apt-get install -y musl-tools && \
    rustup target add x86_64-unknown-linux-musl
WORKDIR /src
COPY Cargo.toml Cargo.lock ./
COPY agent-runtime/Cargo.toml ./agent-runtime/
COPY agent-runtime/src/ ./agent-runtime/src/
COPY ebpf-probes/agentshield-ebpf/Cargo.toml ./ebpf-probes/agentshield-ebpf/
COPY ebpf-probes/agentshield-ebpf/src/ ./ebpf-probes/agentshield-ebpf/src/
COPY ebpf-probes/agentshield-ebpf-common/Cargo.toml ./ebpf-probes/agentshield-ebpf-common/
COPY ebpf-probes/agentshield-ebpf-common/src/ ./ebpf-probes/agentshield-ebpf-common/src/
COPY ebpf-probes/agentshield-loader/Cargo.toml ./ebpf-probes/agentshield-loader/
COPY ebpf-probes/agentshield-loader/src/ ./ebpf-probes/agentshield-loader/src/
RUN cargo build --release --target x86_64-unknown-linux-musl -p agent-runtime

# ── Stage 2: Minimal runtime ──
FROM alpine:3.20
RUN apk add --no-cache ca-certificates libgcc && \
    addgroup -S agentshield && adduser -S agentshield -G agentshield

COPY --from=rust-builder /src/target/x86_64-unknown-linux-musl/release/agent-runtime /usr/local/bin/agent-runtime
COPY agent-runtime/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

RUN mkdir -p /etc/agentshield/policy-cache /usr/local/lib/agentshield/ebpf && \
    chown -R agentshield:agentshield /etc/agentshield

USER agentshield
ENTRYPOINT ["/entrypoint.sh"]
