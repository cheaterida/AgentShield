"""Hyperparameters for the 150D Contrastive Autoencoder embedding model."""

from dataclasses import dataclass, field


@dataclass
class EmbeddingConfig:
    """Configuration for the CAE embedding pipeline.

    The 150D latent space is partitioned for interpretability:
      action_proj  (28D) — what operation was performed
      resource_proj (42D) — what resource was targeted
      attr_proj     (30D) — contextual attributes
      temporal      (10D) — when (hour-of-day sin/cos)
      agent_proj    (20D) — which agent
      residual      (20D) — free capacity for unmodelled factors
    """

    # ── latent space budget (must sum to 150) ──
    latent_dim: int = 150
    action_dim: int = 28
    resource_dim: int = 42
    attr_dim: int = 30
    temporal_dim: int = 10
    agent_dim: int = 20
    residual_dim: int = 20

    # ── vocabularies ──
    max_action_vocab: int = 50
    action_embed_dim: int = 32

    max_path_vocab: int = 128
    path_embed_dim: int = 32

    max_attr_key_vocab: int = 64
    attr_key_embed_dim: int = 32
    attr_val_embed_dim: int = 32

    max_agent_vocab: int = 1024
    agent_embed_dim: int = 32

    # ── transformer ──
    d_model: int = 128
    nhead: int = 4
    num_encoder_layers: int = 2
    num_decoder_layers: int = 2
    dim_feedforward: int = 256
    max_seq_len: int = 32

    # ── training ──
    beta: float = 0.1         # KL divergence weight (β-VAE)
    temperature: float = 0.07  # NT-Xent contrastive temperature
    contrastive_weight: float = 0.3
    recon_weight: float = 1.0
    learning_rate: float = 1e-3
    batch_size: int = 64
    epochs: int = 50
    warmup_steps: int = 100

    # ── data ──
    training_events_min: int = 500       # min events before training starts
    fine_tune_interval: int = 500       # new events before fine-tuning
    synthetic_samples: int = 2000        # cold-start synthetic data points

    # ── anomaly ──
    recon_error_percentile: float = 95.0  # percentile for anomaly threshold

    @property
    def dim_sum(self) -> int:
        return (
            self.action_dim
            + self.resource_dim
            + self.attr_dim
            + self.temporal_dim
            + self.agent_dim
            + self.residual_dim
        )

    def validate(self) -> None:
        if self.dim_sum != self.latent_dim:
            raise ValueError(
                f"Dimension sum {self.dim_sum} != latent_dim {self.latent_dim}"
            )


# singleton default
default_config = EmbeddingConfig()
