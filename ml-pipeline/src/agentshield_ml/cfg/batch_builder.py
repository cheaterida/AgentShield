"""Batch CFG graph builder — converts per-agent event sequences into DGL graphs.

Orchestrates:  event extraction → CAE embedding → CFG construction → DGL conversion.
"""

from __future__ import annotations

from typing import Optional

import torch

from .builder import CFGBuilder, ResourceClassifier
from ..embedding.config import EmbeddingConfig, default_config
from ..embedding.encoder import ContrastiveAutoencoder
from ..embedding.feature_extractor import FeatureExtractor


def build_graphs_from_events(
    agent_events: dict[str, list[dict]],
    *,
    model: Optional[ContrastiveAutoencoder] = None,
    extractor: Optional[FeatureExtractor] = None,
    builder: Optional[CFGBuilder] = None,
    device: str = "cpu",
    window_size: int = 200,
    batch_size: int = 64,
) -> list[dict]:
    """Build DGL graphs from per-agent event sequences.

    Args:
        agent_events: ``{agent_id: [AuditEvent, ...]}`` as returned by
            ``preprocess.build_cfg_dataset()``.
        model: Pre-loaded ContrastiveAutoencoder for 150D embeddings.
            If None, graphs are built without embeddings.
        extractor: FeatureExtractor. Created on first call if None.
        builder: CFGBuilder instance.
        device: Torch device string.
        window_size: Max events per agent window.
        batch_size: CAE encoding batch size.

    Returns:
        List of dicts: ``{agent_id, graph: CFGGraph, node_feats: Tensor,
        edge_feats: Tensor, label: str}``
    """
    if builder is None:
        builder = CFGBuilder(window_size=window_size)

    # Build CFGGraphs (no embeddings yet)
    cfgs = builder.build_from_agent_groups(agent_events)

    results: list[dict] = []

    # If we have an encoder, compute embeddings and attach them
    if model is not None:
        if extractor is None:
            extractor = FeatureExtractor()

        for agent_id, graph in cfgs.items():
            events = agent_events.get(agent_id, [])
            label = _infer_label(events)

            # Compute embeddings for this agent's events in batches
            all_embeddings: dict[str, list[float]] = {}
            recent_events = events[-window_size:] if len(events) > window_size else events

            for batch_start in range(0, len(recent_events), batch_size):
                batch_events = recent_events[batch_start:batch_start + batch_size]
                feats = extractor.extract_batch(batch_events)
                batch = extractor.collate(feats)
                device_batch = {k: v.to(device) for k, v in batch.items()}

                with torch.no_grad():
                    emb = model.encode(device_batch)  # [B, 150]

                emb_list = emb.cpu().tolist()
                for i, ev in enumerate(batch_events):
                    action = ev.get("action", "unknown").lower()
                    resource_ref = ev.get("resource_ref", "")
                    attrs = ev.get("attributes") or {}
                    rc = builder.classifier.classify(resource_ref, action, attrs)
                    node_label = f"{action}→{rc}"
                    if i < len(emb_list):
                        all_embeddings[node_label] = emb_list[i]

            if all_embeddings:
                graph.set_node_embeddings(all_embeddings)

            dgl_graph, node_feats, edge_feats = graph.to_dgl()
            results.append({
                "agent_id": agent_id,
                "graph": graph,
                "dgl_graph": dgl_graph,
                "node_feats": node_feats,
                "edge_feats": edge_feats,
                "label": label,
            })
    else:
        for agent_id, graph in cfgs.items():
            events = agent_events.get(agent_id, [])
            label = _infer_label(events)
            dgl_graph, node_feats, edge_feats = graph.to_dgl()

            results.append({
                "agent_id": agent_id,
                "graph": graph,
                "dgl_graph": dgl_graph,
                "node_feats": node_feats,
                "edge_feats": edge_feats,
                "label": label,
            })

    return results


def _infer_label(events: list[dict]) -> str:
    """Extract the majority label from event attributes."""
    labels: dict[str, int] = {}
    for ev in events:
        attrs = ev.get("attributes") or {}
        lb = attrs.get("label", "unknown")
        labels[lb] = labels.get(lb, 0) + 1
    if not labels:
        return "unknown"
    return max(labels, key=lambda k: labels[k])
