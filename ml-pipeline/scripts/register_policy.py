#!/usr/bin/env python3
"""Register a trained GNN SVDD checkpoint as a PolicyBundle with management-server.

Usage:
  python scripts/register_policy.py \\
      --server http://localhost:8090 \\
      --checkpoint ./models/gnn_svdd.pt \\
      --family-group default
"""

import argparse
import sys
from datetime import datetime, timezone
from pathlib import Path

import requests


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Register trained GNN model as PolicyBundle"
    )
    parser.add_argument(
        "--server", default="http://localhost:8090",
        help="Management-server base URL",
    )
    parser.add_argument(
        "--checkpoint", default="./models/gnn_svdd.pt",
        help="Path to trained GNN checkpoint",
    )
    parser.add_argument(
        "--family-group", default="default",
        help="Family group ID",
    )
    parser.add_argument(
        "--activate", action="store_true", default=True,
        help="Activate after registration",
    )
    args = parser.parse_args()

    server = args.server.rstrip("/")
    ckpt_path = Path(args.checkpoint)

    if not ckpt_path.exists():
        print(f"ERROR: checkpoint not found: {ckpt_path}")
        sys.exit(1)

    # Read checkpoint
    with open(ckpt_path, "rb") as f:
        payload = f.read()

    version = datetime.now(timezone.utc).strftime("gnn-%Y%m%d-%H%M%S")

    # Create PolicyBundle
    bundle = {
        "version": version,
        "policy_type": "gnn_policy",
        "payload": payload.hex(),  # JSON-safe hex encoding
        "digest": f"sha256:{hash(payload) & 0xFFFFFFFFFFFFFFFF:016x}",
        "metadata": {
            "family_group_id": args.family_group,
            "checkpoint_path": str(ckpt_path.resolve()),
            "checkpoint_size": str(len(payload)),
            "source": "adfa-ld-gnn-svdd",
        },
    }

    print(f"Registering policy bundle v{version} ({len(payload)} bytes)...")

    # 1. Create the bundle
    resp = requests.post(
        f"{server}/api/v1/policies/bundles",
        json=bundle,
        timeout=30,
    )
    if resp.status_code != 201:
        print(f"ERROR: create bundle failed: {resp.status_code} {resp.text}")
        sys.exit(1)
    print(f"  Created: {resp.status_code}")

    # 2. Activate
    if args.activate:
        resp = requests.put(
            f"{server}/api/v1/policies/bundles/{version}/activate",
            timeout=30,
        )
        if resp.status_code != 200:
            print(f"ERROR: activate failed: {resp.status_code} {resp.text}")
            sys.exit(1)
        print(f"  Activated: {resp.json()['activated']}")

    print(f"\nPolicy registered and activated. Version: {version}")
    print(f"Verify: curl {server}/api/v1/policies/bundles | python -m json.tool")


if __name__ == "__main__":
    main()
