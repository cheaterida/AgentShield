"""PyTorch DataLoader for CFG DGL graphs — batched GNN training."""

from __future__ import annotations

from typing import Optional

import dgl
import torch
from torch.utils.data import Dataset


class CFGGraphDataset(Dataset):
    """Holds a list of CFG-to-DGL conversion results for GNN training.

    Each item: ``(node_feats: Tensor[N, 154], edge_feats: Tensor[E, 4],
    graph: DGLGraph, label: str)``
    """

    def __init__(
        self,
        graphs: list[dict],
        *,
        label_to_idx: Optional[dict[str, int]] = None,
    ):
        self.items: list[dict] = []
        self.label_to_idx: dict[str, int] = {"normal": 0}

        if label_to_idx is not None:
            self.label_to_idx.update(label_to_idx)

        for g in graphs:
            node_feats = g.get("node_feats")
            edge_feats = g.get("edge_feats")
            dgl_graph = g.get("dgl_graph")
            label = g.get("label", "normal")

            if node_feats is None or dgl_graph is None:
                continue
            if node_feats.numel() == 0:
                continue

            if label not in self.label_to_idx:
                self.label_to_idx[label] = len(self.label_to_idx)

            self.items.append({
                "node_feats": node_feats,
                "edge_feats": edge_feats,
                "dgl_graph": dgl_graph,
                "label_idx": self.label_to_idx[label],
                "agent_id": g.get("agent_id", ""),
            })

    def __len__(self) -> int:
        return len(self.items)

    def __getitem__(self, idx: int):
        return self.items[idx]

    @property
    def num_classes(self) -> int:
        return len(self.label_to_idx)


def collate_graph_batch(
    batch: list[dict],
) -> dict:
    """Collate a list of single-graph items into a batched DGL graph.

    Returns a dict with keys: ``graph, node_feats, edge_feats, labels``.
    """
    graphs: list[dgl.DGLGraph] = []
    node_feats_list: list[torch.Tensor] = []
    edge_feats_list: list[torch.Tensor] = []
    labels: list[int] = []
    agent_ids: list[str] = []

    for item in batch:
        g = item["dgl_graph"]
        nf = item["node_feats"]
        ef = item.get("edge_feats")

        graphs.append(g)
        node_feats_list.append(nf)
        if ef is not None:
            edge_feats_list.append(ef if ef.numel() > 0 else torch.zeros(g.num_edges(), 4))
        labels.append(item["label_idx"])
        agent_ids.append(item.get("agent_id", ""))

    try:
        batched_graph = dgl.batch(graphs)
    except Exception:
        # Fallback: return first graph unbatched
        batched_graph = graphs[0]
        node_feats_list = node_feats_list[:1]
        labels = labels[:1]

    batched_nf = torch.cat(node_feats_list, dim=0) if node_feats_list else torch.zeros(0, 154)
    batched_ef = torch.cat(edge_feats_list, dim=0) if edge_feats_list else torch.zeros(0, 4)

    return {
        "graph": batched_graph,
        "node_feats": batched_nf,
        "edge_feats": batched_ef,
        "labels": torch.tensor(labels, dtype=torch.long),
        "agent_ids": agent_ids,
    }
