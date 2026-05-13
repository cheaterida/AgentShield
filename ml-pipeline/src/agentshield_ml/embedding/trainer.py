"""Training loop for the Contrastive Autoencoder with three objectives:
  1. VAE loss — KL divergence between N(mu,σ) and N(0,I)
  2. Reconstruction loss — cross-entropy over action, path, attribute tokens
  3. Contrastive loss — NT-Xent pulling same-agent events together
"""

import math
import time
from pathlib import Path
from typing import Optional

import torch
import torch.nn.functional as F
from torch.optim import AdamW
from torch.optim.lr_scheduler import CosineAnnealingWarmRestarts
from torch.utils.data import DataLoader, TensorDataset

from .config import EmbeddingConfig, default_config
from .encoder import ContrastiveAutoencoder
from .feature_extractor import FeatureExtractor, AuditEventFeatures


class CAETrainer:
    """Trains the Contrastive Autoencoder on audit event data."""

    def __init__(
        self,
        model: ContrastiveAutoencoder,
        config: EmbeddingConfig | None = None,
        device: str = "cpu",
    ):
        self.model = model
        self.config = config or model.config or default_config
        self.device = device
        self.model.to(device)

        self.optimizer = AdamW(
            model.parameters(),
            lr=self.config.learning_rate,
            weight_decay=1e-5,
        )
        self.scheduler = CosineAnnealingWarmRestarts(
            self.optimizer, T_0=10, T_mult=2
        )

        # anomaly threshold (set after training)
        self.recon_threshold: float = 0.0
        self._train_step = 0

    # ── loss functions ──

    def _vae_loss(self, mu: torch.Tensor, logvar: torch.Tensor) -> torch.Tensor:
        """KL divergence scaled by beta."""
        kl = -0.5 * (1 + logvar - mu.pow(2) - logvar.exp()).sum(dim=-1).mean()
        return self.config.beta * kl

    def _recon_loss(
        self, recon: dict[str, torch.Tensor], batch: dict[str, torch.Tensor]
    ) -> torch.Tensor:
        """Sum of cross-entropy losses over all output heads."""
        loss_action = F.cross_entropy(recon["action_logits"], batch["action"])
        loss_path = F.cross_entropy(
            recon["path_logits"].permute(0, 2, 1), batch["path"], ignore_index=0
        )
        loss_attr_key = F.cross_entropy(
            recon["attr_key_logits"].permute(0, 2, 1), batch["attr_keys"], ignore_index=0
        )
        loss_attr_val = F.cross_entropy(
            recon["attr_val_logits"].permute(0, 2, 1), batch["attr_vals"], ignore_index=0
        )
        return loss_action + loss_path + loss_attr_key + loss_attr_val

    def _contrastive_loss(
        self,
        proj: torch.Tensor,
        agent_ids: list[str],
    ) -> torch.Tensor:
        """NT-Xent loss: events from the same agent are positive pairs."""
        if len(agent_ids) == 0:
            return torch.tensor(0.0, device=self.device)

        # build same-agent mask
        B = len(agent_ids)
        mask = torch.zeros(B, B, device=self.device)
        for i in range(B):
            for j in range(B):
                if i != j and agent_ids[i] == agent_ids[j]:
                    mask[i, j] = 1.0

        if mask.sum() < 1:
            return torch.tensor(0.0, device=self.device)

        # normalize projection vectors
        proj_norm = F.normalize(proj, dim=-1)  # [B, D]

        # cosine similarity matrix
        sim = proj_norm @ proj_norm.T  # [B, B]
        sim = sim / self.config.temperature

        # for each row i, positives are j where mask[i,j] == 1
        # NT-Xent: -log( sum(exp(sim_pos)) / sum(exp(sim_all)) )
        loss = torch.tensor(0.0, device=self.device)
        count = 0
        for i in range(B):
            positives = mask[i]
            n_pos = positives.sum()
            if n_pos == 0:
                continue

            # numerical stability: subtract max
            sim_i = sim[i]
            sim_max = sim_i.max()
            exp_sim = (sim_i - sim_max).exp()

            pos_sum = (exp_sim * positives).sum()
            all_sum = exp_sim.sum() - exp_sim[i].exp()  # exclude self

            if all_sum > 0 and pos_sum > 0:
                loss_i = -torch.log(pos_sum / all_sum)
                loss += loss_i
                count += 1

        if count > 0:
            loss = loss / count
        return loss

    # ── training ──

    def train_step(
        self,
        batch: dict[str, torch.Tensor],
        agent_ids: list[str],
    ) -> dict[str, float]:
        """Single training step. Returns loss values for logging."""
        self.model.train()
        self.optimizer.zero_grad()

        # move batch to device
        device_batch = {k: v.to(self.device) for k, v in batch.items()}
        output = self.model(device_batch, agent_ids)

        loss_vae = self._vae_loss(output["mu"], output["logvar"])
        loss_recon = self._recon_loss(output["recon"], device_batch)
        loss_contrast = self._contrastive_loss(output["proj"], agent_ids)

        total = (
            self.config.recon_weight * loss_recon
            + loss_vae
            + self.config.contrastive_weight * loss_contrast
        )

        total.backward()
        torch.nn.utils.clip_grad_norm_(self.model.parameters(), 1.0)
        self.optimizer.step()
        self.scheduler.step()
        self._train_step += 1

        return {
            "loss_total": total.item(),
            "loss_vae": loss_vae.item(),
            "loss_recon": loss_recon.item(),
            "loss_contrast": loss_contrast.item(),
            "lr": self.scheduler.get_last_lr()[0],
        }

    def fit(
        self,
        extractor: FeatureExtractor,
        events: list[dict],
        epochs: int | None = None,
        batch_size: int | None = None,
        callbacks: Optional[list] = None,
    ) -> list[dict[str, float]]:
        """Full training loop over a corpus of events."""
        epochs = epochs or self.config.epochs
        batch_size = batch_size or self.config.batch_size

        # extract features
        features = extractor.extract_batch(events)
        agent_ids = [ev.get("agent_id", "") for ev in events]

        history: list[dict[str, float]] = []

        for epoch in range(epochs):
            epoch_losses = {"loss_total": 0.0, "loss_vae": 0.0, "loss_recon": 0.0, "loss_contrast": 0.0}
            n_batches = 0

            # shuffle
            indices = torch.randperm(len(features))
            for start in range(0, len(features), batch_size):
                batch_idx = indices[start : start + batch_size]
                batch_features = [features[i] for i in batch_idx]
                batch_agents = [agent_ids[i] for i in batch_idx]

                batch = extractor.collate(batch_features)
                losses = self.train_step(batch, batch_agents)

                for k in epoch_losses:
                    epoch_losses[k] += losses[k]
                n_batches += 1

            # average
            for k in epoch_losses:
                epoch_losses[k] /= max(n_batches, 1)
            epoch_losses["epoch"] = float(epoch)
            history.append(epoch_losses)

            if callbacks:
                for cb in callbacks:
                    cb(epoch, epoch_losses, self.model)

        # compute anomaly threshold from reconstruction errors
        self._compute_threshold(extractor, features)

        return history

    def _compute_threshold(
        self, extractor: FeatureExtractor, features: list[AuditEventFeatures]
    ) -> None:
        """Set reconstruction error threshold at configured percentile."""
        self.model.eval()
        errors = []
        batch_size = self.config.batch_size

        with torch.no_grad():
            for start in range(0, len(features), batch_size):
                batch_features = features[start : start + batch_size]
                batch = extractor.collate(batch_features)
                device_batch = {k: v.to(self.device) for k, v in batch.items()}
                err = self.model.reconstruction_error(device_batch)
                errors.append(err.cpu())

        all_errors = torch.cat(errors)
        self.recon_threshold = float(
            torch.quantile(all_errors, self.config.recon_error_percentile / 100.0).item()
        )

    # ── persistence ──

    def save(self, path: str | Path) -> None:
        path = Path(path)
        path.parent.mkdir(parents=True, exist_ok=True)
        torch.save(
            {
                "model_state": self.model.state_dict(),
                "optimizer_state": self.optimizer.state_dict(),
                "config": self.config,
                "recon_threshold": self.recon_threshold,
                "train_step": self._train_step,
            },
            str(path),
        )

    @classmethod
    def load(
        cls,
        path: str | Path,
        model: ContrastiveAutoencoder,
        device: str = "cpu",
    ) -> "CAETrainer":
        ckpt = torch.load(str(path), map_location=device, weights_only=False)
        model.load_state_dict(ckpt["model_state"])
        trainer = cls(model, ckpt.get("config", default_config), device)
        trainer.optimizer.load_state_dict(ckpt["optimizer_state"])
        trainer.recon_threshold = ckpt.get("recon_threshold", 0.0)
        trainer._train_step = ckpt.get("train_step", 0)
        return trainer

    def export_torchscript(self, path: str | Path) -> None:
        """Export the encoder to TorchScript for distribution."""
        self.model.eval()

        # trace encoder with example input
        batch_size = 1
        S = self.config.max_seq_len
        example = {
            "action": torch.zeros(batch_size, dtype=torch.long),
            "path": torch.zeros(batch_size, S, dtype=torch.long),
            "attr_keys": torch.zeros(batch_size, S, dtype=torch.long),
            "attr_vals": torch.zeros(batch_size, S, dtype=torch.long),
            "temporal": torch.zeros(batch_size, 10),
        }

        class EncodeWrapper(torch.nn.Module):
            def __init__(self, cae: ContrastiveAutoencoder):
                super().__init__()
                self.cae = cae

            def forward(self, action, path, attr_keys, attr_vals, temporal):
                batch = {
                    "action": action,
                    "path": path,
                    "attr_keys": attr_keys,
                    "attr_vals": attr_vals,
                    "temporal": temporal,
                }
                return self.cae.encode(batch)

        wrapper = EncodeWrapper(self.model)
        traced = torch.jit.trace(
            wrapper,
            (
                example["action"],
                example["path"],
                example["attr_keys"],
                example["attr_vals"],
                example["temporal"],
            ),
        )
        torch.jit.save(traced, str(path))
