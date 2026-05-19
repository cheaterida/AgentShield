# ── AgentShield Agent Runtime Docker Image ──
# Binary must be pre-built: bash agent-runtime/build-linux.sh
# Build:  docker build -t agentshield/agent:latest -f agent-runtime/Dockerfile .
# Run:    docker run -d --pid=host --privileged \
#           -e AGENTSHIELD_AGENT_ID="worker-01" \
#           -e AGENTSHIELD_FAMILY_GROUP_ID="fg-001" \
#           -e AGENTSHIELD_MGMT_ADDR="http://<mgmt-ip>:8080" \
#           agentshield/agent:latest
FROM scratch
COPY agent-runtime/bin/agent-runtime /agent-runtime
ENTRYPOINT ["/agent-runtime"]