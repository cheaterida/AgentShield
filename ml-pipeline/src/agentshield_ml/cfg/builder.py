"""Control Flow Graph builder — converts agent audit event sequences into DGL graphs.

Each graph represents an agent's recent behaviour:
  Nodes: unique (action, resource_class) pairs with 150D embedding features
  Edges: directed transitions between consecutive events, weighted by frequency
"""

import json
from collections import defaultdict
from typing import Optional

import torch

from .resource_classifier import ResourceClassifier
from ..embedding.config import EmbeddingConfig, default_config


# ── graph data structures ──

class CFGNode:
    """A node in the control flow graph."""
    def __init__(self, node_id: int, action: str, resource_class: str, resource_ref: str = ""):
        self.node_id = node_id
        self.action = action
        self.resource_class = resource_class
        self.resource_ref = resource_ref
        self.frequency: int = 1
        self.first_seen_idx: int = 0
        self.last_seen_idx: int = 0
        self.embedding: Optional[list[float]] = None  # 150D

    @property
    def label(self) -> str:
        return f"{self.action}→{self.resource_class}"

    def to_dict(self) -> dict:
        return {
            "node_id": self.node_id,
            "action": self.action,
            "resource_class": self.resource_class,
            "resource_ref": self.resource_ref,
            "frequency": self.frequency,
            "label": self.label,
        }


class CFGEdge:
    """A directed edge between two CFG nodes."""
    def __init__(self, src: int, dst: int):
        self.src = src
        self.dst = dst
        self.weight: int = 1
        self.avg_time_delta_sec: float = 0.0
        self.time_deltas: list[float] = []

    def add_transition(self, time_delta_sec: float = 0.0):
        self.weight += 1
        self.time_deltas.append(time_delta_sec)
        self.avg_time_delta_sec = sum(self.time_deltas) / len(self.time_deltas)

    def to_dict(self) -> dict:
        return {
            "src": self.src,
            "dst": self.dst,
            "weight": self.weight,
            "avg_time_delta_sec": round(self.avg_time_delta_sec, 3),
        }


class CFGGraph:
    """Complete control flow graph for a single agent."""
    def __init__(self, agent_id: str, window_size: int = 200):
        self.agent_id = agent_id
        self.window_size = window_size
        self.nodes: dict[int, CFGNode] = {}
        self.edges: dict[tuple[int, int], CFGEdge] = {}
        self._node_idx: dict[str, int] = {}  # label → node_id
        self.total_events: int = 0

    def add_event(
        self,
        event_idx: int,
        action: str,
        resource_class: str,
        resource_ref: str = "",
        prev_action: Optional[str] = None,
        prev_resource_class: Optional[str] = None,
        time_delta_sec: float = 0.0,
    ):
        """Add one event to the graph, creating nodes and edges as needed."""
        label = f"{action}→{resource_class}"

        # get or create node
        node_id = self._node_idx.get(label)
        if node_id is None:
            node_id = len(self.nodes)
            self._node_idx[label] = node_id
            self.nodes[node_id] = CFGNode(node_id, action, resource_class, resource_ref)
            self.nodes[node_id].first_seen_idx = event_idx

        node = self.nodes[node_id]
        node.frequency += 1
        node.last_seen_idx = event_idx
        self.total_events += 1

        # create edge from previous event
        if prev_action is not None and prev_resource_class is not None:
            prev_label = f"{prev_action}→{prev_resource_class}"
            prev_id = self._node_idx.get(prev_label)
            if prev_id is not None:
                edge_key = (prev_id, node_id)
                if edge_key not in self.edges:
                    self.edges[edge_key] = CFGEdge(prev_id, node_id)
                self.edges[edge_key].add_transition(time_delta_sec)

    def set_node_embeddings(self, embeddings: dict[str, list[float]]):
        """Assign 150D embeddings to nodes by label."""
        for label, emb in embeddings.items():
            node_id = self._node_idx.get(label)
            if node_id is not None:
                self.nodes[node_id].embedding = emb

    def to_dgl(self, default_embedding_dim: int = 150) -> tuple:
        """Convert to DGL graph tensors. Returns (graph, node_features, edge_features)."""
        import dgl

        if len(self.nodes) == 0:
            g = dgl.graph(([], []))
            g.ndata["feat"] = torch.zeros(0, default_embedding_dim)
            g.edata["feat"] = torch.zeros(0, 4)
            return g, torch.zeros(0, default_embedding_dim), torch.zeros(0, 4)

        n = len(self.nodes)
        src_nodes = []
        dst_nodes = []

        for (s, d), edge in self.edges.items():
            src_nodes.append(s)
            dst_nodes.append(d)

        g = dgl.graph((src_nodes, dst_nodes), num_nodes=n)

        # build node features [n, 150+4] → projected to 150
        node_feats = []
        for i in range(n):
            node = self.nodes[i]
            if node.embedding is not None:
                emb = torch.tensor(node.embedding, dtype=torch.float32)
            else:
                emb = torch.zeros(default_embedding_dim)

            stats = torch.tensor(
                [
                    node.frequency,
                    node.first_seen_idx,
                    node.last_seen_idx,
                    node.last_seen_idx - node.first_seen_idx + 1,
                ],
                dtype=torch.float32,
            )
            node_feats.append(torch.cat([emb, stats]))

        node_feats = torch.stack(node_feats)  # [n, 154]

        # build edge features [e, 4]
        edge_feats = []
        for (s, d), edge in self.edges.items():
            edge_feats.append(
                [
                    float(edge.weight),
                    edge.avg_time_delta_sec,
                    max(edge.time_deltas) if edge.time_deltas else 0.0,
                    1.0 if s == d else 0.0,  # self-loop flag
                ]
            )
        edge_feats = torch.tensor(edge_feats, dtype=torch.float32)

        g.ndata["feat"] = node_feats
        g.edata["feat"] = edge_feats

        return g, node_feats, edge_feats

    def to_dict(self) -> dict:
        return {
            "agent_id": self.agent_id,
            "window_size": self.window_size,
            "total_events": self.total_events,
            "nodes": [node.to_dict() for node in self.nodes.values()],
            "edges": [edge.to_dict() for edge in self.edges.values()],
        }

    def to_json(self) -> str:
        return json.dumps(self.to_dict(), ensure_ascii=False)


# ── builder ──

class CFGBuilder:
    """Builds Control Flow Graphs from agent event sequences."""

    def __init__(
        self,
        classifier: ResourceClassifier = None,
        window_size: int = 200,
        embedding_config: EmbeddingConfig = None,
    ):
        self.classifier = classifier or ResourceClassifier()
        self.window_size = window_size
        self.embedding_config = embedding_config or default_config

    def build(
        self,
        agent_id: str,
        events: list[dict],
        embeddings: Optional[dict[str, list[float]]] = None,
        include_dgl: bool = False,
    ) -> CFGGraph:
        """Build a CFG from an agent's event sequence (most recent N events)."""
        graph = CFGGraph(agent_id, self.window_size)

        # take most recent window_size events
        recent = events[-self.window_size :] if len(events) > self.window_size else events

        prev_action: Optional[str] = None
        prev_resource_class: Optional[str] = None
        prev_time: Optional[float] = None

        for i, ev in enumerate(recent):
            action = ev.get("action", "unknown").lower()
            resource_ref = ev.get("resource_ref", "")
            attributes = ev.get("attributes", {}) or {}
            resource_class = self.classifier.classify(resource_ref, action, attributes)

            # compute time delta from previous event
            time_delta = 0.0
            occurred_at = ev.get("occurred_at", "")
            if occurred_at and prev_time is not None:
                try:
                    from datetime import datetime
                    ts = occurred_at.replace("Z", "+00:00")
                    cur_time = datetime.fromisoformat(ts).timestamp()
                    time_delta = cur_time - prev_time
                except (ValueError, TypeError):
                    pass

            graph.add_event(
                event_idx=i,
                action=action,
                resource_class=resource_class,
                resource_ref=resource_ref,
                prev_action=prev_action,
                prev_resource_class=prev_resource_class,
                time_delta_sec=max(time_delta, 0.0),
            )

            prev_action = action
            prev_resource_class = resource_class
            if occurred_at:
                try:
                    from datetime import datetime
                    ts = occurred_at.replace("Z", "+00:00")
                    prev_time = datetime.fromisoformat(ts).timestamp()
                except (ValueError, TypeError):
                    pass

        # assign embeddings if provided
        if embeddings:
            graph.set_node_embeddings(embeddings)

        return graph

    def build_from_agent_groups(
        self,
        agent_events: dict[str, list[dict]],
        embeddings_by_agent: Optional[dict[str, dict[str, list[float]]]] = None,
    ) -> dict[str, CFGGraph]:
        """Build CFGs for multiple agents at once."""
        graphs: dict[str, CFGGraph] = {}
        for agent_id, events in agent_events.items():
            emb = embeddings_by_agent.get(agent_id) if embeddings_by_agent else None
            graphs[agent_id] = self.build(agent_id, events, emb)
        return graphs

    @staticmethod
    def from_json(graph_json: str) -> CFGGraph:
        """Reconstruct a CFGGraph from JSON serialization."""
        data = json.loads(graph_json)
        g = CFGGraph(data["agent_id"], data.get("window_size", 200))
        g.total_events = data.get("total_events", 0)

        for nd in data.get("nodes", []):
            node = CFGNode(
                nd["node_id"],
                nd["action"],
                nd["resource_class"],
                nd.get("resource_ref", ""),
            )
            node.frequency = nd.get("frequency", 1)
            node.first_seen_idx = nd.get("first_seen_idx", 0)
            node.last_seen_idx = nd.get("last_seen_idx", 0)
            if "embedding" in nd:
                node.embedding = nd["embedding"]
            g.nodes[node.node_id] = node
            g._node_idx[node.label] = node.node_id

        for ed in data.get("edges", []):
            edge = CFGEdge(ed["src"], ed["dst"])
            edge.weight = ed.get("weight", 1)
            edge.avg_time_delta_sec = ed.get("avg_time_delta_sec", 0.0)
            g.edges[(ed["src"], ed["dst"])] = edge

        return g
