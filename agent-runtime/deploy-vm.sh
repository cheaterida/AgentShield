#!/usr/bin/env bash
# Deploy agent-runtime to hermes VM via Tailscale.
# Usage: ./deploy-vm.sh [vm_user] [vm_ip]
set -euo pipefail
cd "$(dirname "$0")"

VM_USER="${1:-root}"
VM_IP="${2:-100.68.106.60}"
BINARY="bin/agent-runtime"

echo "=== Deploying agent-runtime to ${VM_USER}@${VM_IP} ==="

# 1. Check binary exists
if [ ! -f "$BINARY" ]; then
    echo "Binary not found. Building..."
    bash build-linux.sh
fi

echo "--- Copying files to VM ---"

# 2. Copy binary
scp "$BINARY" "${VM_USER}@${VM_IP}:/tmp/agent-runtime"

# 3. Copy env and service files
scp env.vm "${VM_USER}@${VM_IP}:/tmp/agentshield-env"
scp agent-runtime.service "${VM_USER}@${VM_IP}:/tmp/agent-runtime.service"

# 4. Install on VM
ssh "${VM_USER}@${VM_IP}" bash <<'REMOTE'
set -euo pipefail

echo "--- Installing agent-runtime ---"

# Stop old service if running
systemctl stop agent-runtime 2>/dev/null || true

# Install binary
mv /tmp/agent-runtime /usr/local/bin/agent-runtime
chmod +x /usr/local/bin/agent-runtime

# Install config
mkdir -p /etc/agentshield /var/lib/agentshield/policies
mv /tmp/agentshield-env /etc/agentshield/env
chmod 600 /etc/agentshield/env

# Install systemd service
mv /tmp/agent-runtime.service /etc/systemd/system/agent-runtime.service
systemctl daemon-reload
systemctl enable agent-runtime

echo "--- Starting agent-runtime ---"
systemctl start agent-runtime
sleep 2
systemctl status agent-runtime --no-pager

echo ""
echo "=== Done! Check logs: journalctl -u agent-runtime -f ==="
REMOTE

echo "=== Deployment complete ==="