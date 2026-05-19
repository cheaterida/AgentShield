#!/usr/bin/env python3
"""Train GNN-based anomaly detector on open-source intrusion-detection datasets.

Usage:
  python scripts/train_gnn.py \\
      --dataset adfa-ld \\
      --data-dir ./data \\
      --epochs 50 \\
      --output ./models/gnn_svdd.pt

Pipeline:
  1. Download / load ADFA-LD traces
  2. Convert system-call traces → AuditEvent dicts
  3. Build CFG graphs (with optional CAE embeddings)
  4. Train Deep SVDD on normal-operation graphs
  5. Export GNN weights + SVDD center + encoder to PolicyBundle
"""

import argparse
import sys
from datetime import datetime, timezone
from pathlib import Path

import torch

_project_root = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_project_root / "src"))


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Train AgentShield GNN anomaly detector"
    )
    parser.add_argument(
        "--dataset", default="adfa-ld",
        help="Dataset name (default: adfa-ld)",
    )
    parser.add_argument(
        "--data-dir", default=str(_project_root / "data"),
        help="Directory for downloaded datasets",
    )
    parser.add_argument(
        "--epochs", type=int, default=50,
        help="SVDD training epochs (default: 50)",
    )
    parser.add_argument(
        "--lr", type=float, default=1e-3,
        help="Learning rate (default: 1e-3)",
    )
    parser.add_argument(
        "--output", default=str(_project_root / "models" / "gnn_svdd.pt"),
        help="Output checkpoint path",
    )
    parser.add_argument(
        "--device", default="cpu",
        help="Torch device (cpu or cuda)",
    )
    parser.add_argument(
        "--max-traces", type=int, default=200,
        help="Max normal traces to load (default: 200)",
    )
    parser.add_argument(
        "--use-embeddings", action="store_true",
        help="Use CAE 150D embeddings as node features",
    )
    parser.add_argument(
        "--window-size", type=int, default=200,
        help="CFG window size (default: 200)",
    )
    parser.add_argument(
        "--batch-size", type=int, default=32,
        help="Training batch size (default: 32)",
    )
    parser.add_argument(
        "--export-policy", action="store_true",
        help="Export trained model as PolicyBundle",
    )
    parser.add_argument(
        "--family-group-id", default="default",
        help="Family group ID for policy export",
    )
    args = parser.parse_args()

    device = torch.device(args.device if torch.cuda.is_available() else "cpu")
    print(f"Using device: {device}")

    # ── 1. Load dataset ──
    print(f"\n=== Loading {args.dataset} dataset ===")
    from agentshield_ml.data.datasets import load_adfa_ld, list_adfa_traces

    traces = load_adfa_ld(args.data_dir, max_traces=args.max_traces)
    print(f"Loaded: {list_adfa_traces(args.data_dir)}")

    # ── 2. Convert to audit events ──
    print("\n=== Converting traces to audit events ===")
    from agentshield_ml.data.preprocess import build_cfg_dataset

    agent_events = build_cfg_dataset(
        traces,
        max_traces_per_category=args.max_traces,
    )
    normal_count = sum(1 for aid in agent_events if "normal" in aid)
    print(f"Agents with events: {len(agent_events)} (normal: {normal_count})")

    # ── 3. Build CFG graphs ──
    print("\n=== Building CFG graphs ===")

    model = None
    extractor = None
    if args.use_embeddings:
        from agentshield_ml.embedding.encoder import ContrastiveAutoencoder
        from agentshield_ml.embedding.feature_extractor import FeatureExtractor
        from agentshield_ml.embedding.config import default_config

        model = ContrastiveAutoencoder(default_config)
        extractor = FeatureExtractor()

        # Try loading pre-trained CAE checkpoint
        cae_path = _project_root / "models" / "cae_checkpoint.pt"
        if cae_path.exists():
            print(f"Loading pre-trained CAE from {cae_path}")
            checkpoint = torch.load(cae_path, map_location=device, weights_only=False)
            model.load_state_dict(checkpoint.get("model_state", checkpoint))
        model.to(device)
        model.eval()

    from agentshield_ml.cfg.batch_builder import build_graphs_from_events

    graphs = build_graphs_from_events(
        agent_events,
        model=model,
        extractor=extractor,
        device=str(device),
        window_size=args.window_size,
        batch_size=args.batch_size,
    )
    print(f"Built {len(graphs)} CFG graphs")

    # Filter: keep only graphs that have at least 1 edge for GNN training
    trainable_graphs = [
        g for g in graphs
        if g.get("edge_feats") is not None and g["edge_feats"].numel() > 0
    ]
    skipped = len(graphs) - len(trainable_graphs)
    if skipped:
        print(f"  Skipped {skipped} graphs with no edges (single-node)")

    normal_graphs = [g for g in trainable_graphs if g.get("label") == "normal"]
    print(f"  Normal: {len(normal_graphs)}, Anomalous: {len(trainable_graphs) - len(normal_graphs)}")

    if len(normal_graphs) < 5:
        print("ERROR: need at least 5 normal graphs for training")
        sys.exit(1)

    # ── 4. Train SVDD on normal graphs ──
    print(f"\n=== Training Deep SVDD ({args.epochs} epochs) ===")
    from agentshield_ml.gnn.dataloader import CFGGraphDataset, collate_graph_batch
    from agentshield_ml.gnn.model import GATAnomalyDetector
    from agentshield_ml.gnn.svdd_trainer import SVDDTrainer

    dataset = CFGGraphDataset(normal_graphs)
    print(f"Training dataset: {len(dataset)} graphs, {dataset.num_classes} label classes")

    from torch.utils.data import DataLoader
    dataloader = DataLoader(
        dataset,
        batch_size=args.batch_size,
        shuffle=True,
        collate_fn=collate_graph_batch,
    )

    gnn_model = GATAnomalyDetector(
        in_dim=154,
        hidden_dim=128,
        out_dim=150,
        num_heads=4,
        num_layers=3,
        edge_dim=4,
        dropout=0.1,
    ).to(device)

    trainer = SVDDTrainer(gnn_model, device, nu=0.1, lr=args.lr)

    # Initialize SVDD center from first batch
    try:
        first_batch = next(iter(dataloader))
        trainer.initialize_center([first_batch])
    except StopIteration:
        print("ERROR: dataloader is empty")
        sys.exit(1)
    except Exception as e:
        print(f"Center init with batch failed ({e}), using model forward pass")
        trainer.initialize_center(dataloader)

    trainer.fit(dataloader, epochs=args.epochs)

    # ── 5. Save checkpoint ──
    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    trainer.save(str(output_path))
    print(f"\n=== Checkpoint saved to {output_path} ===")
    print(f"  SVDD center norm: {trainer.center.norm().item():.4f}")
    print(f"  r_max (95th percentile): {trainer.r_max:.4f}")

    # ── 6. Quick evaluation on a few anomalous graphs ──
    print("\n=== Quick evaluation ===")
    from agentshield_ml.gnn.inference import GNNInference

    inference = GNNInference(model=gnn_model, trainer=trainer, device=device)
    anomaly_graphs = [g for g in trainable_graphs if g.get("label") != "normal"][:10]
    scores: list[dict] = []
    for g in anomaly_graphs:
        result = inference.score_graph(g["graph"], g["agent_id"])
        scores.append(result)
        print(
            f"  {g['agent_id']:40s} label={g['label']:20s} "
            f"score={result['score']:.4f} anomaly={result['is_anomaly']}"
        )

    detected = sum(1 for s in scores if s["is_anomaly"])
    print(f"\nAnomaly detection: {detected}/{len(scores)} flagged as anomalous")

    # ── 7. Export policy bundle (optional) ──
    if args.export_policy and args.use_embeddings and model is not None:
        print("\n=== Exporting PolicyBundle ===")
        from agentshield_ml.policy.exporter import PolicyExporter, PolicyBundle

        exporter = PolicyExporter(output_dir=str(output_path.parent))
        bundle = PolicyBundle(
            family_group_id=args.family_group_id,
            version=datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S"),
            policy_type="gnn_policy",
        )
        bundle.training_events_count = args.epochs * args.max_traces
        bundle.embedding_config = {
            "latent_dim": model.config.latent_dim,
            "d_model": model.config.d_model,
            "max_seq_len": model.config.max_seq_len,
            "action_dim": model.config.action_dim,
            "resource_dim": model.config.resource_dim,
        }
        if trainer.center is not None:
            bundle.svdd_center = trainer.center.cpu().tolist()
        bundle.r_max = trainer.r_max

        # Save TorchScript encoder
        encoder_path = output_path.with_suffix(".encoder.pt")
        try:
            scripted = torch.jit.script(model.encoder)
            torch.jit.save(scripted, str(encoder_path))
            with open(encoder_path, "rb") as f:
                bundle.encoder_bytes = f.read()
            print(f"  TorchScript encoder saved to {encoder_path}")
        except Exception as e:
            print(f"  TorchScript export failed ({e}), saving state_dict instead")
            torch.save(model.state_dict(), str(encoder_path))

        # Save GNN checkpoint (reuse existing)
        with open(output_path, "rb") as f:
            bundle.gnn_state_dict = f.read()

        meta_path = output_path.with_suffix(".bundle.json")
        meta_path.write_text(bundle.to_json())
        print(f"  PolicyBundle metadata saved to {meta_path}")


if __name__ == "__main__":
    main()
