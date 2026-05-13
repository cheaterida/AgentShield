"""GNN inference — anomaly scoring for individual CFG graphs."""

import json
import logging
from pathlib import Path
from typing import Optional

import torch

from .model import GATAnomalyDetector
from .svdd_trainer import SVDDTrainer
from ..cfg.builder import CFGBuilder, CFGGraph

logger = logging.getLogger(__name__)


class GNNInference:
    """Loads a trained GNN model and scores CFG graphs for anomalies."""

    def __init__(
        self,
        model: Optional[GATAnomalyDetector] = None,
        trainer: Optional[SVDDTrainer] = None,
        device: str = "cpu",
    ):
        self.model = model or GATAnomalyDetector()
        self.trainer = trainer
        self.device = device
        self.model.to(device)
        self.model.eval()
        self._is_loaded = trainer is not None and trainer.center is not None

    @property
    def is_loaded(self) -> bool:
        return self._is_loaded

    @classmethod
    def from_checkpoint(
        cls,
        checkpoint_path: str | Path,
        device: str = "cpu",
    ) -> "GNNInference":
        """Load from a saved SVDDTrainer checkpoint."""
        model = GATAnomalyDetector()
        trainer = SVDDTrainer.load(checkpoint_path, model, device)
        trainer.model.eval()
        return cls(model, trainer, device)

    @torch.no_grad()
    def score_graph(
        self,
        graph_or_json: str | CFGGraph,
        agent_id: str = "",
    ) -> dict:
        """Score a single CFG graph.

        Returns:
            {"score": float 0-1, "distance": float, "r_max": float,
             "is_anomaly": bool, "details": str}
        """
        if not self._is_loaded or self.trainer is None or self.trainer.center is None:
            return self._heuristic_score(graph_or_json, agent_id)

        # parse graph if needed
        if isinstance(graph_or_json, str):
            graph = CFGBuilder.from_json(graph_or_json)
        else:
            graph = graph_or_json

        if len(graph.nodes) == 0:
            return {
                "score": 0.0,
                "distance": 0.0,
                "r_max": self.trainer.r_max,
                "is_anomaly": False,
                "details": "Empty CFG (no nodes)",
            }

        try:
            g, node_feats, edge_feats = graph.to_dgl()
            g = g.to(self.device)
            node_feats = node_feats.to(self.device)
            if edge_feats.numel() > 0:
                edge_feats = edge_feats.to(self.device)
            else:
                edge_feats = None

            emb, graph_score, node_scores = self.model(g, node_feats, edge_feats)

            # distance from SVDD center
            center = self.trainer.center.unsqueeze(0).to(self.device)
            distance = torch.norm(emb - center, dim=1).item()
            r_max = self.trainer.r_max
            score = min(distance / r_max, 1.0) if r_max > 0 else float(graph_score.item())

            # identify anomalous nodes
            anomalous_nodes = []
            if node_scores.numel() > 0:
                node_scores_cpu = node_scores.squeeze(-1).cpu()
                high_score_mask = node_scores_cpu > 0.5
                for idx in high_score_mask.nonzero(as_tuple=True)[0]:
                    idx_i = idx.item()
                    if idx_i in graph.nodes:
                        anomalous_nodes.append(graph.nodes[idx_i].label)

            details = f"GNN score: distance={distance:.4f}, r_max={r_max:.4f}"
            if anomalous_nodes:
                details += f"; anomalous nodes: {anomalous_nodes[:3]}"

            return {
                "score": round(score, 4),
                "distance": round(distance, 4),
                "r_max": round(r_max, 4),
                "is_anomaly": score >= 0.5,
                "details": details,
            }

        except Exception as e:
            logger.warning("GNN inference failed, falling back to heuristic: %s", e)
            return self._heuristic_score(graph_or_json, agent_id)

    @torch.no_grad()
    def score_graphs(
        self,
        graphs: dict[str, CFGGraph],
    ) -> dict[str, dict]:
        """Batch score multiple CFG graphs."""
        return {agent_id: self.score_graph(graph, agent_id) for agent_id, graph in graphs.items()}

    def _heuristic_score(
        self, graph_or_json: str | CFGGraph, agent_id: str = ""
    ) -> dict:
        """Fallback heuristic when GNN model is not loaded."""
        if isinstance(graph_or_json, str):
            try:
                data = json.loads(graph_or_json)
            except (json.JSONDecodeError, TypeError):
                return {
                    "score": 0.1, "distance": 0.0, "r_max": 1.0,
                    "is_anomaly": False, "details": "Unable to parse CFG JSON",
                }
        elif isinstance(graph_or_json, CFGGraph):
            data = graph_or_json.to_dict()
        else:
            data = {}

        nodes = data.get("nodes", [])
        edges = data.get("edges", [])

        if len(nodes) == 0:
            return {
                "score": 0.0, "distance": 0.0, "r_max": 1.0,
                "is_anomaly": False, "details": "Empty CFG",
            }

        # Heuristic checks
        reasons = []
        score = 0.0

        if len(nodes) > 200:
            score += 0.4
            reasons.append(f"Large node count ({len(nodes)})")

        if len(edges) == 0 and len(nodes) > 2:
            score += 0.3
            reasons.append("Disconnected graph")

        # Check for sensitive path nodes
        sensitive_resources = {"CONFIG", "KERNEL"}
        for node in nodes:
            if node.get("resource_class") in sensitive_resources:
                freq = node.get("frequency", 0)
                if freq > 10:
                    score += 0.2
                    reasons.append(f"High frequency ({freq}) on sensitive resource {node.get('label')}")
                    break

        return {
            "score": round(min(score, 1.0), 4),
            "distance": 0.0,
            "r_max": 1.0,
            "is_anomaly": score >= 0.5,
            "details": "; ".join(reasons) if reasons else "Heuristic: normal CFG structure",
        }
