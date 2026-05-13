"""Deep SVDD trainer for one-class graph anomaly detection.

Trains the GAT model to map normal CFG graphs close to a learned hypersphere center.
Anomalies are detected by their distance from this center at inference time.
"""

import math
from pathlib import Path
from typing import Optional

import torch
import torch.nn.functional as F
from torch.optim import AdamW
from torch.optim.lr_scheduler import CosineAnnealingLR

from .model import GATAnomalyDetector


class SVDDTrainer:
    """Deep Support Vector Data Description trainer for GNN-based CFG analysis.

    After training:
      - The model encodes normal CFGs near center c.
      - anomaly_score = min(1.0, distance / r_max).
    """

    def __init__(
        self,
        model: GATAnomalyDetector,
        device: str = "cpu",
        nu: float = 0.1,       # SVDD upper bound on outlier fraction
        lr: float = 1e-3,
    ):
        self.model = model.to(device)
        self.device = device
        self.nu = nu

        self.center: Optional[torch.Tensor] = None  # [out_dim]
        self.r_max: float = 1.0  # normalizing radius (95th percentile)

        self.optimizer = AdamW(model.parameters(), lr=lr, weight_decay=1e-5)
        self.scheduler = CosineAnnealingLR(self.optimizer, T_max=50)

        self._train_step = 0

    def initialize_center(self, dataloader) -> None:
        """Set the SVDD center c as the mean graph embedding of the initial data."""
        self.model.eval()
        embeddings = []

        with torch.no_grad():
            for batch in dataloader:
                g = batch["graph"].to(self.device)
                nf = batch["node_feats"].to(self.device)
                ef = batch.get("edge_feats")
                if ef is not None:
                    ef = ef.to(self.device)

                emb, _, _ = self.model(g, nf, ef)
                embeddings.append(emb.detach().cpu())

        all_emb = torch.cat(embeddings, dim=0)
        self.center = all_emb.mean(dim=0).to(self.device)

        # compute initial r_max from distances
        distances = torch.norm(all_emb - self.center.cpu(), dim=1)
        self.r_max = float(torch.quantile(distances, 0.95).item())
        if self.r_max < 1e-6:
            self.r_max = 1.0

    def train_step(self, batch: dict) -> dict[str, float]:
        """Single SVDD training step."""
        self.model.train()
        self.optimizer.zero_grad()

        g = batch["graph"].to(self.device)
        nf = batch["node_feats"].to(self.device)
        ef = batch.get("edge_feats")
        if ef is not None:
            ef = ef.to(self.device)

        emb, _, _ = self.model(g, nf, ef)

        # SVDD loss: pull embeddings toward center c
        assert self.center is not None, "Must call initialize_center() before training"
        distances = torch.norm(emb - self.center.unsqueeze(0), dim=1)

        # Hinge-like loss: penalize distances, with nu controlling outlier tolerance
        loss = distances.pow(2).mean()

        # Optional: add small penalty on weight norms
        w_norm = torch.tensor(0.0, device=self.device)
        for p in self.model.parameters():
            w_norm += p.pow(2).sum()
        loss = loss + 1e-6 * w_norm

        loss.backward()
        torch.nn.utils.clip_grad_norm_(self.model.parameters(), 1.0)
        self.optimizer.step()
        self.scheduler.step()
        self._train_step += 1

        return {
            "loss_svdd": loss.item(),
            "mean_distance": distances.mean().item(),
            "max_distance": distances.max().item(),
            "r_max": self.r_max,
        }

    def fit(
        self,
        dataloader,
        epochs: int = 30,
        callbacks: Optional[list] = None,
    ) -> list[dict[str, float]]:
        """Full training loop."""
        history: list[dict[str, float]] = []

        for epoch in range(epochs):
            epoch_stats = {"loss_svdd": 0.0, "mean_distance": 0.0, "max_distance": 0.0}
            n_batches = 0

            for batch in dataloader:
                stats = self.train_step(batch)
                for k in epoch_stats:
                    epoch_stats[k] += stats[k]
                n_batches += 1

            for k in epoch_stats:
                epoch_stats[k] /= max(n_batches, 1)
            epoch_stats["epoch"] = float(epoch)
            history.append(epoch_stats)

            if callbacks:
                for cb in callbacks:
                    cb(epoch, epoch_stats, self.model)

        # update r_max from final data
        self._update_radius(dataloader)

        return history

    def _update_radius(self, dataloader) -> None:
        """Recompute r_max as 95th percentile of distances."""
        self.model.eval()
        all_distances = []

        with torch.no_grad():
            for batch in dataloader:
                g = batch["graph"].to(self.device)
                nf = batch["node_feats"].to(self.device)
                ef = batch.get("edge_feats")
                if ef is not None:
                    ef = ef.to(self.device)

                emb, _, _ = self.model(g, nf, ef)
                dist = torch.norm(emb - self.center.unsqueeze(0), dim=1)
                all_distances.append(dist.cpu())

        if all_distances:
            d = torch.cat(all_distances)
            r = float(torch.quantile(d, 0.95).item())
            if r > 1e-6:
                self.r_max = r

    def save(self, path: str | Path) -> None:
        path = Path(path)
        path.parent.mkdir(parents=True, exist_ok=True)
        torch.save(
            {
                "model_state": self.model.state_dict(),
                "center": self.center.cpu() if self.center is not None else None,
                "r_max": self.r_max,
                "train_step": self._train_step,
            },
            str(path),
        )

    @classmethod
    def load(
        cls,
        path: str | Path,
        model: GATAnomalyDetector,
        device: str = "cpu",
    ) -> "SVDDTrainer":
        ckpt = torch.load(str(path), map_location=device, weights_only=False)
        model.load_state_dict(ckpt["model_state"])
        trainer = cls(model, device)
        if ckpt.get("center") is not None:
            trainer.center = ckpt["center"].to(device)
        trainer.r_max = ckpt.get("r_max", 1.0)
        trainer._train_step = ckpt.get("train_step", 0)
        return trainer
