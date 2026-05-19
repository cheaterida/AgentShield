"""Graph Attention Network (GAT) for CFG anomaly detection.

Architecture:
  3×GATConv with residual connections + LayerNorm
  → GlobalAttentionPooling → 150D graph embedding
  → Graph head: anomaly_score [0,1]
  → Node head: per-node anomaly_score
"""

import dgl
import torch
import torch.nn as nn
import torch.nn.functional as F


class GATAnomalyDetector(nn.Module):
    """GAT-based anomaly detector for agent control flow graphs.

    The model learns a hypersphere center in the 150D graph embedding space.
    Anomaly score = distance from center, normalized by training-set radius.
    """

    def __init__(
        self,
        in_dim: int = 154,          # 150D embedding + 4D stats
        hidden_dim: int = 128,
        out_dim: int = 150,
        num_heads: int = 4,
        num_layers: int = 3,
        edge_dim: int = 4,          # weight, avg_time_delta, max_time_delta, is_self_loop
        dropout: float = 0.1,
    ):
        super().__init__()
        self.in_dim = in_dim
        self.hidden_dim = hidden_dim
        self.out_dim = out_dim

        # input projection
        self.input_proj = nn.Linear(in_dim, hidden_dim)

        # GAT layers with residual
        self.convs = nn.ModuleList()
        self.norms = nn.ModuleList()

        for i in range(num_layers):
            in_ch = hidden_dim * num_heads if i > 0 else hidden_dim
            out_ch = hidden_dim
            heads = num_heads if i < num_layers - 1 else 2
            concat = (i < num_layers - 1)

            conv = GATConvWithEdge(
                in_dim=in_ch,
                out_dim=out_ch,
                num_heads=heads,
                edge_dim=edge_dim,
                dropout=dropout,
                concat=concat,
            )
            self.convs.append(conv)
            # When concat=True, output dim is out_ch*heads; when False, out_ch (averaged)
            norm_dim = out_ch * heads if concat else out_ch
            self.norms.append(nn.LayerNorm(norm_dim))

        # project final conv output to target embedding dimension
        final_conv_dim = hidden_dim * 2 if num_layers == 1 else hidden_dim
        # Last layer: if num_layers==1, heads=2 with concat → hidden*2; else no concat → hidden
        self.out_proj = nn.Linear(hidden_dim, out_dim)

        # global pooling via attention (operates on out_dim)
        self.pool_gate = nn.Sequential(
            nn.Linear(out_dim, out_dim),
            nn.Tanh(),
            nn.Linear(out_dim, 1),
        )

        # graph-level head
        self.graph_head = nn.Sequential(
            nn.Linear(out_dim, 64),
            nn.ReLU(),
            nn.Dropout(dropout),
            nn.Linear(64, 1),
            nn.Sigmoid(),
        )

        # node-level head
        self.node_head = nn.Sequential(
            nn.Linear(out_dim, 64),
            nn.ReLU(),
            nn.Linear(64, 1),
            nn.Sigmoid(),
        )

    def forward(self, g, node_feats, edge_feats=None):
        """Forward pass returning (graph_embedding, anomaly_score, node_scores).

        Args:
            g: DGL graph (batched or single)
            node_feats: [N, in_dim]
            edge_feats: [E, edge_dim] or None

        Returns:
            graph_emb: [B, out_dim] or [1, out_dim] for single graph
            graph_score: [B, 1]
            node_scores: [N, 1]
        """
        h = self.input_proj(node_feats)  # [N, hidden]

        for i, (conv, norm) in enumerate(zip(self.convs, self.norms)):
            residual = h
            h = conv(g, h, edge_feats)
            # residual connection if shapes match
            if h.shape[-1] == residual.shape[-1]:
                h = h + residual
            h = norm(h)
            if i < len(self.convs) - 1:
                h = F.elu(h)

        # project to target embedding dimension
        h = self.out_proj(h)  # [N, out_dim]

        # node-level anomaly scores
        node_scores = self.node_head(h)

        # global mean pooling (simple, reliable)
        g.ndata["h"] = h
        graph_emb = dgl.readout_nodes(g, "h", op="mean")  # [B, out_dim]

        graph_score = self.graph_head(graph_emb)  # [B, 1]

        return graph_emb, graph_score, node_scores


class GATConvWithEdge(nn.Module):
    """GAT convolution with edge feature support."""

    def __init__(
        self,
        in_dim: int,
        out_dim: int,
        num_heads: int,
        edge_dim: int = 4,
        dropout: float = 0.1,
        concat: bool = True,
    ):
        super().__init__()
        self.num_heads = num_heads
        self.out_dim = out_dim
        self.concat = concat

        self.fc_src = nn.Linear(in_dim, out_dim * num_heads, bias=False)
        self.fc_dst = nn.Linear(in_dim, out_dim * num_heads, bias=False)
        self.fc_edge = nn.Linear(edge_dim, out_dim * num_heads, bias=False)

        self.attn_l = nn.Parameter(torch.zeros(1, num_heads, out_dim))
        self.attn_r = nn.Parameter(torch.zeros(1, num_heads, out_dim))
        self.attn_e = nn.Parameter(torch.zeros(1, num_heads, out_dim))

        self.leaky_relu = nn.LeakyReLU(0.2)
        self.dropout = nn.Dropout(dropout)

        nn.init.xavier_uniform_(self.fc_src.weight)
        nn.init.xavier_uniform_(self.fc_dst.weight)
        nn.init.xavier_uniform_(self.fc_edge.weight)
        nn.init.xavier_uniform_(self.attn_l)
        nn.init.xavier_uniform_(self.attn_r)
        nn.init.xavier_uniform_(self.attn_e)

    def forward(self, g, node_feats, edge_feats=None):
        import dgl.function as fn

        h_src = self.fc_src(node_feats).view(-1, self.num_heads, self.out_dim)
        h_dst = self.fc_dst(node_feats).view(-1, self.num_heads, self.out_dim)

        # compute per-node attention scores
        el = (h_src * self.attn_l).sum(dim=-1, keepdim=True)  # [N, H, 1]
        er = (h_dst * self.attn_r).sum(dim=-1, keepdim=True)  # [N, H, 1]

        # store per-node scores and let DGL broadcast to edges via apply_edges
        g.srcdata["el"] = el
        g.srcdata["h_src"] = h_src
        g.dstdata["er"] = er
        g.dstdata["h_dst"] = h_dst

        has_edge_feats = edge_feats is not None and edge_feats.shape[0] > 0
        if has_edge_feats:
            e = self.fc_edge(edge_feats).view(-1, self.num_heads, self.out_dim)
            ee = (e * self.attn_e).sum(dim=-1, keepdim=True)  # [E, H, 1]
            g.edata["ee"] = ee

            def _edge_attn(edges):
                return {"a": self.leaky_relu(
                    edges.src["el"] + edges.dst["er"] + edges.data["ee"]
                )}
        else:
            def _edge_attn(edges):
                return {"a": self.leaky_relu(
                    edges.src["el"] + edges.dst["er"]
                )}

        g.apply_edges(_edge_attn)

        # softmax per dst node using apply_edges for correct batching
        g.edata["a_exp"] = torch.exp(g.edata["a"])
        g.update_all(fn.copy_e("a_exp", "m"), fn.sum("m", "a_sum"))

        def _norm(edges):
            return {"a_norm": edges.data["a_exp"] / edges.dst["a_sum"]}
        g.apply_edges(_norm)

        # aggregate weighted messages via apply_edges
        def _weighted(edges):
            return {"h_weighted": edges.data["a_norm"] * edges.src["h_src"]}
        g.apply_edges(_weighted)

        g.update_all(fn.copy_e("h_weighted", "m"), fn.sum("m", "h_out"))

        h_out = g.dstdata["h_out"]  # [N, H, D]

        if self.concat:
            h_out = h_out.view(-1, self.num_heads * self.out_dim)
        else:
            h_out = h_out.mean(dim=1)

        return self.dropout(h_out)
