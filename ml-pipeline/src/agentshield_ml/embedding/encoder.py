"""Contrastive Autoencoder with VAE latent space (150D).

Architecture:
  Encoder: TokenEmbedding → TransformerEncoder → MeanPool → mu/logvar → z (150D)
  Decoder: z → TransformerDecoder → output heads (action, path, attributes)

The 150D latent is partitioned for interpretability per EmbeddingConfig.
"""

import math
from typing import Optional

import torch
import torch.nn as nn
import torch.nn.functional as F

from .config import EmbeddingConfig, default_config


# ── token embedding layer ──

class TokenEmbedding(nn.Module):
    """Embeds categorical tokens (action, path components, attr keys/vals) into
    a unified d_model-dimensional space, then sums with a learned positional
    encoding to form the transformer input sequence."""

    def __init__(self, config: EmbeddingConfig):
        super().__init__()
        self.d_model = config.d_model
        self.max_seq_len = config.max_seq_len

        self.action_embed = nn.Embedding(config.max_action_vocab, config.action_embed_dim)
        self.path_embed = nn.Embedding(config.max_path_vocab, config.path_embed_dim)
        self.attr_key_embed = nn.Embedding(config.max_attr_key_vocab, config.attr_key_embed_dim)
        self.attr_val_embed = nn.Embedding(config.max_attr_key_vocab, config.attr_val_embed_dim)

        # project per-field embeddings to d_model
        self.action_proj = nn.Linear(config.action_embed_dim, config.d_model)
        self.path_proj = nn.Linear(config.path_embed_dim, config.d_model)
        self.attr_proj = nn.Linear(
            config.attr_key_embed_dim + config.attr_val_embed_dim, config.d_model
        )
        self.temporal_proj = nn.Linear(10, config.d_model)  # 10D temporal features

        # positional encoding (learned)
        self.pos_encoding = nn.Parameter(torch.zeros(1, config.max_seq_len, config.d_model))
        nn.init.normal_(self.pos_encoding, std=0.02)

    def forward(
        self,
        action: torch.Tensor,       # [B]
        path: torch.Tensor,         # [B, S]
        attr_keys: torch.Tensor,    # [B, S]
        attr_vals: torch.Tensor,    # [B, S]
        temporal: torch.Tensor,     # [B, 10]
    ) -> tuple[torch.Tensor, torch.Tensor]:
        """Returns (token_seq [B, S, d_model], padding_mask [B, S])."""
        B, S = path.shape

        # action token → broadcast to first position
        a = self.action_proj(self.action_embed(action))  # [B, d_model]
        a = a.unsqueeze(1)  # [B, 1, d_model]

        # path tokens
        p = self.path_proj(self.path_embed(path))  # [B, S, d_model]

        # attribute tokens (concat key+val per position)
        ak = self.attr_key_embed(attr_keys)  # [B, S, ek]
        av = self.attr_val_embed(attr_vals)  # [B, S, ev]
        attr = self.attr_proj(torch.cat([ak, av], dim=-1))  # [B, S, d_model]

        # temporal → broadcast to all positions
        t = self.temporal_proj(temporal).unsqueeze(1).expand(-1, S, -1)  # [B, S, d_model]

        # assemble: action at position 0, then path+attr+temporal for rest
        seq = a + p + attr + t  # [B, S, d_model]

        # add positional encoding
        seq = seq + self.pos_encoding[:, :S, :]

        # padding mask: True where path token is 0 (<pad>)
        pad_mask = (path == 0)  # [B, S]

        return seq, pad_mask


# ── encoder ──

class CAEEncoder(nn.Module):
    """Transformer encoder that maps a token sequence to a 150D latent vector
    via variational reparameterization."""

    def __init__(self, config: EmbeddingConfig):
        super().__init__()
        self.latent_dim = config.latent_dim
        self.d_model = config.d_model

        encoder_layer = nn.TransformerEncoderLayer(
            d_model=config.d_model,
            nhead=config.nhead,
            dim_feedforward=config.dim_feedforward,
            batch_first=True,
            activation="gelu",
        )
        self.transformer = nn.TransformerEncoder(encoder_layer, num_layers=config.num_encoder_layers)

        self.mu_head = nn.Sequential(
            nn.Linear(config.d_model, config.d_model),
            nn.GELU(),
            nn.LayerNorm(config.d_model),
            nn.Linear(config.d_model, config.latent_dim),
        )
        self.logvar_head = nn.Sequential(
            nn.Linear(config.d_model, config.d_model),
            nn.GELU(),
            nn.LayerNorm(config.d_model),
            nn.Linear(config.d_model, config.latent_dim),
        )

    def forward(
        self, token_seq: torch.Tensor, pad_mask: torch.Tensor
    ) -> tuple[torch.Tensor, torch.Tensor, torch.Tensor]:
        """Returns (z, mu, logvar)."""
        # transformer encode
        encoded = self.transformer(token_seq, src_key_padding_mask=pad_mask)  # [B, S, d_model]

        # mean pool over non-padded positions
        if pad_mask is not None:
            # invert mask: True → keep
            keep = (~pad_mask).float().unsqueeze(-1)  # [B, S, 1]
            pooled = (encoded * keep).sum(dim=1) / keep.sum(dim=1).clamp(min=1)
        else:
            pooled = encoded.mean(dim=1)  # [B, d_model]

        mu = self.mu_head(pooled)
        logvar = self.logvar_head(pooled)

        # reparameterization
        std = torch.exp(0.5 * logvar)
        eps = torch.randn_like(std)
        z = mu + eps * std

        return z, mu, logvar


# ── decoder ──

class CAEDecoder(nn.Module):
    """Transformer decoder that reconstructs action, path, and attribute tokens
    from the 150D latent vector."""

    def __init__(self, config: EmbeddingConfig):
        super().__init__()
        self.d_model = config.d_model
        self.max_seq_len = config.max_seq_len

        self.latent_proj = nn.Linear(config.latent_dim, config.d_model)

        decoder_layer = nn.TransformerEncoderLayer(
            d_model=config.d_model,
            nhead=config.nhead,
            dim_feedforward=config.dim_feedforward,
            batch_first=True,
            activation="gelu",
        )
        self.transformer = nn.TransformerEncoder(decoder_layer, num_layers=config.num_decoder_layers)

        # output heads — predict token indices
        self.action_head = nn.Linear(config.d_model, config.max_action_vocab)
        self.path_head = nn.Linear(config.d_model, config.max_path_vocab)
        self.attr_key_head = nn.Linear(config.d_model, config.max_attr_key_vocab)
        self.attr_val_head = nn.Linear(config.d_model, config.max_attr_key_vocab)

        # learned query positions for decoder
        self.query_pos = nn.Parameter(torch.zeros(1, config.max_seq_len, config.d_model))
        nn.init.normal_(self.query_pos, std=0.02)

    def forward(self, z: torch.Tensor) -> dict[str, torch.Tensor]:
        """Returns logits for each output head."""
        B = z.shape[0]
        S = self.max_seq_len

        # project latent and expand to sequence length
        h = self.latent_proj(z).unsqueeze(1).expand(-1, S, -1)  # [B, S, d_model]
        h = h + self.query_pos[:, :S, :]

        decoded = self.transformer(h)  # [B, S, d_model]

        return {
            "action_logits": self.action_head(decoded[:, 0, :]),     # [B, vocab_a]
            "path_logits": self.path_head(decoded),                  # [B, S, vocab_p]
            "attr_key_logits": self.attr_key_head(decoded),          # [B, S, vocab_k]
            "attr_val_logits": self.attr_val_head(decoded),          # [B, S, vocab_v]
        }


# ── full CAE ──

class ContrastiveAutoencoder(nn.Module):
    """Contrastive Autoencoder combining VAE reconstruction with NT-Xent loss.

    Training objectives:
      1. VAE loss: KL(N(mu,σ) || N(0,I)) scaled by beta
      2. Reconstruction: CrossEntropy over action + path + attr tokens
      3. Contrastive: NT-Xent between events from same agent (positive pairs)
    """

    def __init__(self, config: EmbeddingConfig | None = None):
        super().__init__()
        self.config = config or default_config
        self.config.validate()

        self.token_embed = TokenEmbedding(self.config)
        self.encoder = CAEEncoder(self.config)
        self.decoder = CAEDecoder(self.config)

        # projection head for contrastive learning
        self.projection = nn.Sequential(
            nn.Linear(self.config.latent_dim, self.config.latent_dim),
            nn.GELU(),
            nn.Linear(self.config.latent_dim, 64),
        )

    def forward(
        self, batch: dict[str, torch.Tensor], agent_ids: Optional[list[str]] = None
    ) -> dict[str, torch.Tensor]:
        """Full forward pass.

        Returns a dict with:
          z          [B, 150]  — latent vectors
          mu         [B, 150]  — encoder mean
          logvar     [B, 150]  — encoder log-variance
          recon      dict      — decoder output logits
          proj       [B, 64]   — contrastive projection
        """
        seq, pad_mask = self.token_embed(
            batch["action"], batch["path"],
            batch["attr_keys"], batch["attr_vals"],
            batch["temporal"],
        )
        z, mu, logvar = self.encoder(seq, pad_mask)
        recon = self.decoder(z)
        proj = self.projection(z)

        return {
            "z": z,
            "mu": mu,
            "logvar": logvar,
            "recon": recon,
            "proj": proj,
            "pad_mask": pad_mask,
        }

    @torch.no_grad()
    def encode(self, batch: dict[str, torch.Tensor]) -> torch.Tensor:
        """Encode a batch to 150D latent vectors (inference-only, no sampling)."""
        seq, pad_mask = self.token_embed(
            batch["action"], batch["path"],
            batch["attr_keys"], batch["attr_vals"],
            batch["temporal"],
        )
        # use mu directly (no sampling) at inference
        _, mu, _ = self.encoder(seq, pad_mask)
        return mu  # [B, 150]

    @torch.no_grad()
    def reconstruction_error(
        self, batch: dict[str, torch.Tensor]
    ) -> torch.Tensor:
        """Per-sample reconstruction error (MSE over all token predictions).
        Returns [B] tensor — higher values indicate more anomalous events."""
        output = self.forward(batch)
        recon = output["recon"]

        # CrossEntropy per sample
        action_loss = F.cross_entropy(
            recon["action_logits"], batch["action"], reduction="none"
        )
        path_loss = F.cross_entropy(
            recon["path_logits"].permute(0, 2, 1), batch["path"], reduction="none"
        ).mean(dim=1)
        attr_key_loss = F.cross_entropy(
            recon["attr_key_logits"].permute(0, 2, 1), batch["attr_keys"], reduction="none"
        ).mean(dim=1)
        attr_val_loss = F.cross_entropy(
            recon["attr_val_logits"].permute(0, 2, 1), batch["attr_vals"], reduction="none"
        ).mean(dim=1)

        total = action_loss + path_loss + attr_key_loss + attr_val_loss
        return total  # [B]
