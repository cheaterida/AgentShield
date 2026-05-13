#!/bin/sh
# AgentShield Agent Runtime entrypoint
# Loads eBPF probes (if available) then starts the agent runtime.

set -e

AGENTSHIELD_EBPF_OBJECT="${AGENTSHIELD_EBPF_OBJECT:-/usr/local/lib/agentshield/ebpf/agentshield-ebpf.o}"
AGENTSHIELD_AGENT_ID="${AGENTSHIELD_AGENT_ID:-dev-agent-001}"
AGENTSHIELD_FAMILY_GROUP_ID="${AGENTSHIELD_FAMILY_GROUP_ID:-dev-fg}"
AGENTSHIELD_MGMT_ADDR="${AGENTSHIELD_MGMT_ADDR:-http://management-server:8080}"

echo "=== AgentShield Agent Runtime ==="
echo "Agent ID:       ${AGENTSHIELD_AGENT_ID}"
echo "Family Group:   ${AGENTSHIELD_FAMILY_GROUP_ID}"
echo "Mgmt Server:    ${AGENTSHIELD_MGMT_ADDR}"
echo "eBPF Object:    ${AGENTSHIELD_EBPF_OBJECT}"

# Verify eBPF object exists (non-fatal — runtime can run with demo events)
if [ -f "${AGENTSHIELD_EBPF_OBJECT}" ]; then
    echo "eBPF probe object found"
else
    echo "WARNING: eBPF probe object not found at ${AGENTSHIELD_EBPF_OBJECT}"
    echo "  Agent runtime will generate demo events instead."
    echo "  For production eBPF monitoring, mount the probe object or build with --profile ebpf"
fi

# Start agent runtime (passes all AGENTSHIELD_* env vars)
exec /usr/local/bin/agent-runtime
