# ── AgentShield Agent Runtime Docker Image ──
# Multi-stage build: Rust agent + eBPF bytecode → minimal Alpine runtime
#
# Build:  docker build -t agentshield/agent:latest -f agent-runtime/Dockerfile .
# Run:    docker run -d --pid=host --privileged \
#           -e AGENTSHIELD_AGENT_ID="prod-agent-001" \
#           -e AGENTSHIELD_FAMILY_GROUP_ID="prod-fg" \
#           -e AGENTSHIELD_MGMT_ADDR="https://mgmt.example.com:8443" \
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

# ── Stage 2: eBPF bytecode builder ──
FROM rust:1.80-bookworm AS ebpf-builder
RUN apt-get update && apt-get install -y libssl-dev pkg-config && \
    rustup target add bpfel-unknown-none && \
    cargo install bpf-linker
WORKDIR /src
COPY Cargo.toml Cargo.lock ./
COPY ebpf-probes/agentshield-ebpf/Cargo.toml ./ebpf-probes/agentshield-ebpf/
COPY ebpf-probes/agentshield-ebpf/src/ ./ebpf-probes/agentshield-ebpf/src/
COPY ebpf-probes/agentshield-ebpf-common/Cargo.toml ./ebpf-probes/agentshield-ebpf-common/
COPY ebpf-probes/agentshield-ebpf-common/src/ ./ebpf-probes/agentshield-ebpf-common/src/
RUN cargo build --release --target bpfel-unknown-none -p agentshield-ebpf

# ── Stage 3: Minimal runtime ──
FROM alpine:3.20
RUN apk add --no-cache ca-certificates libgcc && \
    addgroup -S agentshield && adduser -S agentshield -G agentshield

# Copy Rust binary
COPY --from=rust-builder /src/target/x86_64-unknown-linux-musl/release/agent-runtime /usr/local/bin/agent-runtime

# Copy eBPF bytecode
COPY --from=ebpf-builder /src/target/bpfel-unknown-none/release/agentshield-ebpf /usr/local/lib/agentshield/ebpf/agentshield-ebpf.o

# Copy entrypoint
COPY agent-runtime/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Policy cache directory
RUN mkdir -p /etc/agentshield/policy-cache && \
    chown agentshield:agentshield /etc/agentshield/policy-cache

USER agentshield
ENTRYPOINT ["/entrypoint.sh"]
