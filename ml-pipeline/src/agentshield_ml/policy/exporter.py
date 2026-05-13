"""Export trained models as versioned PolicyBundles for distribution to agents."""

import io
import json
import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

import torch

from ..embedding.encoder import ContrastiveAutoencoder
from ..embedding.trainer import CAETrainer
from ..embedding.config import EmbeddingConfig
from ..gnn.model import GATAnomalyDetector
from ..gnn.svdd_trainer import SVDDTrainer

logger = logging.getLogger(__name__)


class PolicyBundle:
    """A versioned, distributable ML policy containing trained models and metadata."""

    def __init__(
        self,
        family_group_id: str,
        version: str,
        policy_type: str = "gnn_policy",
    ):
        self.family_group_id = family_group_id
        self.version = version
        self.policy_type = policy_type
        self.training_events_count: int = 0
        self.created_at: str = datetime.now(timezone.utc).isoformat()

        # serialized model bytes
        self.encoder_bytes: Optional[bytes] = None   # TorchScript encoder
        self.gnn_state_dict: Optional[bytes] = None  # GNN model weights
        self.svdd_center: Optional[list[float]] = None  # 150D center
        self.r_max: float = 1.0

        # thresholds
        self.threshold_medium: float = 0.30
        self.threshold_high: float = 0.60
        self.threshold_critical: float = 0.80

        # metadata
        self.embedding_config: Optional[dict] = None
        self.node_labels: list[str] = []

    def to_dict(self) -> dict:
        return {
            "family_group_id": self.family_group_id,
            "version": self.version,
            "type": self.policy_type,
            "training_events_count": self.training_events_count,
            "created_at": self.created_at,
            "svdd_center": self.svdd_center,
            "r_max": self.r_max,
            "thresholds": {
                "medium": self.threshold_medium,
                "high": self.threshold_high,
                "critical": self.threshold_critical,
            },
            "embedding_config": self.embedding_config,
        }

    def to_json(self) -> str:
        return json.dumps(self.to_dict(), ensure_ascii=False, indent=2)


class PolicyExporter:
    """Exports trained CAE + GNN models as PolicyBundle objects.

    Two export modes:
      1. Full export: saves all model bytes + metadata to a directory
      2. Lightweight export: saves only TorchScript encoder + SVDD center (for agent-side inference)
    """

    def __init__(self, output_dir: str | Path = "./policies"):
        self.output_dir = Path(output_dir)
        self.output_dir.mkdir(parents=True, exist_ok=True)

    def export_full(
        self,
        family_group_id: str,
        version: str,
        cae_trainer: CAETrainer,
        svdd_trainer: SVDDTrainer,
        node_labels: Optional[list[str]] = None,
    ) -> PolicyBundle:
        """Full export: CAE encoder (TorchScript) + GNN weights + SVDD center + metadata."""
        bundle = PolicyBundle(family_group_id, version, "gnn_policy")
        bundle.training_events_count = cae_trainer._train_step
        bundle.node_labels = node_labels or []

        if svdd_trainer.center is not None:
            bundle.svdd_center = svdd_trainer.center.cpu().tolist()
        bundle.r_max = svdd_trainer.r_max

        # thresholds from config
        cfg = cae_trainer.config
        bundle.embedding_config = {
            "latent_dim": cfg.latent_dim,
            "d_model": cfg.d_model,
            "max_seq_len": cfg.max_seq_len,
            "action_dim": cfg.action_dim,
            "resource_dim": cfg.resource_dim,
        }

        # export TorchScript encoder
        encoder_path = self.output_dir / f"{family_group_id}_{version}_encoder.pt"
        cae_trainer.export_torchscript(encoder_path)
        with open(encoder_path, "rb") as f:
            bundle.encoder_bytes = f.read()

        # save GNN state dict
        gnn_path = self.output_dir / f"{family_group_id}_{version}_gnn.pt"
        svdd_trainer.save(gnn_path)
        with open(gnn_path, "rb") as f:
            bundle.gnn_state_dict = f.read()

        # save bundle metadata
        meta_path = self.output_dir / f"{family_group_id}_{version}_bundle.json"
        meta_path.write_text(bundle.to_json())

        logger.info(
            "Exported policy %s v%s: %d training events, r_max=%.4f",
            family_group_id, version, bundle.training_events_count, bundle.r_max,
        )
        return bundle

    def export_lightweight(
        self,
        family_group_id: str,
        version: str,
        cae_trainer: CAETrainer,
        svdd_trainer: SVDDTrainer,
    ) -> PolicyBundle:
        """Lightweight export for agent-side distribution (encoder + center only)."""
        bundle = PolicyBundle(family_group_id, version, "gnn_policy_light")
        bundle.training_events_count = cae_trainer._train_step

        if svdd_trainer.center is not None:
            bundle.svdd_center = svdd_trainer.center.cpu().tolist()
        bundle.r_max = svdd_trainer.r_max

        # only export TorchScript encoder
        encoder_path = self.output_dir / f"{family_group_id}_{version}_encoder.pt"
        cae_trainer.export_torchscript(encoder_path)
        with open(encoder_path, "rb") as f:
            bundle.encoder_bytes = f.read()

        logger.info(
            "Exported lightweight policy %s v%s for agent distribution",
            family_group_id, version,
        )
        return bundle

    @staticmethod
    def get_next_version(family_group_id: str, existing_versions: list[str]) -> str:
        """Compute the next semver-like version string."""
        prefix = f"{family_group_id}-v"
        if not existing_versions:
            return f"{prefix}1.0.0"

        # find the latest version for this family group
        relevant = [v for v in existing_versions if v.startswith(prefix)]
        if not relevant:
            return f"{prefix}1.0.0"

        # parse major.minor.patch
        latest = sorted(relevant)[-1]
        parts = latest[len(prefix):].split(".")
        major, minor, patch = int(parts[0]), int(parts[1]), int(parts[2])

        # bump patch for new export
        return f"{prefix}{major}.{minor}.{patch + 1}"
