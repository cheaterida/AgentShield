#!/usr/bin/env bash
# ── AgentShield Span Bridge quick-start ──
# Usage: ./run.sh [--once]
#
# --once   Run a single poll cycle and exit (for testing)
# (no arg) Run the persistent polling loop

set -euo pipefail
cd "$(dirname "$0")"

# ── Load .env if present ──
if [ -f .env ]; then
  set -a; source .env; set +a
fi

# ── Ensure venv exists ──
if [ ! -d venv ]; then
  python3 -m venv venv
  venv/bin/pip install -r requirements.txt
fi

if [ "${1:-}" = "--once" ]; then
  venv/bin/python -c "
from langtrace_bridge import Config, SpanBridge
cfg = Config()
print(f'[once] {cfg.dump()}')
bridge = SpanBridge(cfg)
from datetime import datetime, timezone
ck = datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')
new_ck, count = bridge.run_once(ck)
print(f'[once] pushed {count} events, checkpoint={new_ck}')
"
else
  venv/bin/python langtrace_bridge.py
fi
