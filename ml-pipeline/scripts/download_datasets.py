#!/usr/bin/env python3
"""Download open-source intrusion-detection datasets for AgentShield training.

Usage:
  python scripts/download_datasets.py --dataset adfa-ld        # ~15 MB
  python scripts/download_datasets.py --dataset lid-ds         # ~30 GB (optional)
  python scripts/download_datasets.py --dataset all            # both
"""

import argparse
import sys
from pathlib import Path

# Ensure the ml-pipeline package is importable
_project_root = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_project_root / "src"))


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Download AgentShield training datasets"
    )
    parser.add_argument(
        "--dataset",
        choices=["adfa-ld", "lid-ds", "all"],
        default="adfa-ld",
        help="Which dataset to download (default: adfa-ld)",
    )
    parser.add_argument(
        "--data-dir",
        default=str(_project_root / "data"),
        help="Directory to store downloaded datasets",
    )
    args = parser.parse_args()

    data_dir = Path(args.data_dir)
    data_dir.mkdir(parents=True, exist_ok=True)

    if args.dataset in ("adfa-ld", "all"):
        from agentshield_ml.data.datasets import download_adfa_ld
        path = download_adfa_ld(str(data_dir))
        print(f"ADFA-LD ready at: {path}")

        from agentshield_ml.data.datasets import list_adfa_traces
        counts = list_adfa_traces(str(data_dir))
        print(f"Traces loaded: {counts}")

    if args.dataset in ("lid-ds", "all"):
        print(
            "LID-DS download not yet automated. "
            "Visit https://github.com/LID-DS/LID-DS "
            "and follow the manual download instructions."
        )


if __name__ == "__main__":
    main()
