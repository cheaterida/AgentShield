"""Tests for CAE encoder embedding pipeline."""

import torch
import pytest

from agentshield_ml.embedding.config import EmbeddingConfig, default_config
from agentshield_ml.embedding.feature_extractor import FeatureExtractor
from agentshield_ml.embedding.encoder import ContrastiveAutoencoder


def test_embedding_config_dimensions():
    cfg = default_config
    total = (
        cfg.action_dim
        + cfg.resource_dim
        + cfg.attr_dim
        + cfg.temporal_dim
        + cfg.agent_dim
        + cfg.residual_dim
    )
    assert total == 150, f"latent dims sum to {total}, expected 150"


def test_feature_extractor_extract_single():
    extractor = FeatureExtractor()
    event = {
        "event_id": "evt1",
        "agent_id": "agent-1",
        "action": "read",
        "resource_ref": "/data/production/dataset.csv",
        "occurred_at": "2024-06-15T12:00:00.000Z",
        "attributes": {"size": "1048576", "format": "csv"},
    }
    feats = extractor.extract(event)
    assert feats is not None
    assert feats.agent_id == "agent-1"
    assert feats.action_idx >= 0


def test_feature_extractor_extract_batch():
    extractor = FeatureExtractor()
    events = [
        {"event_id": "e1", "agent_id": "a1", "action": "read", "resource_ref": "/data/a.csv",
         "occurred_at": "2024-01-01T00:00:00Z", "attributes": {}},
        {"event_id": "e2", "agent_id": "a1", "action": "write", "resource_ref": "/data/b.json",
         "occurred_at": "2024-01-01T00:00:01Z", "attributes": {}},
    ]
    feats = extractor.extract_batch(events)
    assert len(feats) == 2
    batch = extractor.collate(feats)
    assert "action" in batch
    assert "path" in batch
    assert "temporal" in batch
    assert batch["action"].shape[0] == 2


def test_contrastive_autoencoder_creation():
    model = ContrastiveAutoencoder(default_config)
    assert model is not None


def test_encoder_encode_output_shape():
    model = ContrastiveAutoencoder(default_config)
    model.eval()

    extractor = FeatureExtractor()
    events = [
        {"event_id": f"e{i}", "agent_id": "a1", "action": "read",
         "resource_ref": f"/data/file{i}.csv",
         "occurred_at": "2024-01-01T00:00:00Z", "attributes": {}}
        for i in range(4)
    ]
    feats = extractor.extract_batch(events)
    batch = extractor.collate(feats)

    with torch.no_grad():
        embeddings = model.encode(batch)  # [B, 150]

    assert embeddings.shape == (4, 150)


def test_encoder_reconstruction_error_shape():
    model = ContrastiveAutoencoder(default_config)
    model.eval()

    extractor = FeatureExtractor()
    events = [
        {"event_id": "e1", "agent_id": "a1", "action": "read",
         "resource_ref": "/data/a.csv",
         "occurred_at": "2024-01-01T00:00:00Z", "attributes": {}},
    ]
    feats = extractor.extract_batch(events)
    batch = extractor.collate(feats)

    with torch.no_grad():
        recon_err = model.reconstruction_error(batch)

    assert recon_err.shape == (1,)
    assert recon_err.item() >= 0.0
