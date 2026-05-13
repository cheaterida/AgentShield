"""AgentShield ML pipeline — 150D embedding, anomaly scoring, CFG analysis."""

import os
import json
from pathlib import Path
from typing import Optional

import torch
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

from ..embedding.config import EmbeddingConfig, default_config
from ..embedding.feature_extractor import FeatureExtractor
from ..embedding.encoder import ContrastiveAutoencoder
from ..embedding.trainer import CAETrainer

# ── FastAPI app ──

app = FastAPI(title="AgentShield ML", version="0.2.0")

# ── Pydantic models ──

class AuditEventScoreReq(BaseModel):
    event_id: str
    agent_id: str
    action: str
    resource_ref: str = ""
    occurred_at: str = ""
    attributes: dict[str, str] = {}


class EventScoreResp(BaseModel):
    event_id: str
    anomaly_score: float
    embedding: Optional[list[float]] = None


class BatchScoreReq(BaseModel):
    events: list[AuditEventScoreReq]


class BatchScoreResp(BaseModel):
    scores: list[EventScoreResp]


class EmbedRequest(BaseModel):
    events: list[AuditEventScoreReq]


class EmbedResponse(BaseModel):
    embeddings: list[list[float]]  # [N, 150]


class TrainRequest(BaseModel):
    events: list[AuditEventScoreReq]
    epochs: int = 30


class TrainResponse(BaseModel):
    status: str
    final_loss: float
    recon_threshold: float
    num_events: int


class CFGAnalyzeReq(BaseModel):
    graph_json: str
    agent_id: str


class CFGAnalyzeResp(BaseModel):
    anomaly_detected: bool
    anomaly_score: float
    details: str = ""


# ── global model state (lazy init) ──

_model: Optional[ContrastiveAutoencoder] = None
_trainer: Optional[CAETrainer] = None
_extractor: Optional[FeatureExtractor] = None
_device: str = "cuda" if torch.cuda.is_available() else "cpu"
_model_path: Path = Path(os.environ.get("AGENTSHIELD_ML_MODEL_PATH", "./models/cae_checkpoint.pt"))


def _get_extractor() -> FeatureExtractor:
    global _extractor
    if _extractor is None:
        _extractor = FeatureExtractor(default_config)
    return _extractor


def _get_model() -> ContrastiveAutoencoder:
    global _model, _trainer

    if _model is not None:
        return _model

    _model = ContrastiveAutoencoder(default_config)
    _model.to(_device)
    _model.eval()

    # try loading pre-trained weights
    if _model_path.exists():
        try:
            _trainer = CAETrainer.load(str(_model_path), _model, _device)
            app.state.model_loaded = True
            app.state.training_events = _trainer._train_step  # approximate count
        except Exception:
            app.state.model_loaded = False
            app.state.training_events = 0
    else:
        app.state.model_loaded = False
        app.state.training_events = 0

    return _model


def _event_to_dict(ev: AuditEventScoreReq) -> dict:
    return {
        "event_id": ev.event_id,
        "agent_id": ev.agent_id,
        "action": ev.action,
        "resource_ref": ev.resource_ref,
        "occurred_at": ev.occurred_at,
        "attributes": ev.attributes,
    }


# ── endpoints ──

@app.get("/healthz")
def healthz() -> dict[str, str]:
    return {
        "status": "ok",
        "version": "0.2.0",
        "device": _device,
        "model_loaded": str(getattr(app.state, "model_loaded", False)),
    }


@app.post("/api/v1/anomaly/embed", response_model=EmbedResponse)
def embed_events(req: EmbedRequest) -> EmbedResponse:
    """Convert audit events to 150D behavioural embeddings."""
    model = _get_model()
    extractor = _get_extractor()

    if not req.events:
        return EmbedResponse(embeddings=[])

    events = [_event_to_dict(ev) for ev in req.events]
    features = extractor.extract_batch(events)
    batch = extractor.collate(features)
    device_batch = {k: v.to(_device) for k, v in batch.items()}

    with torch.no_grad():
        embeddings = model.encode(device_batch)  # [B, 150]

    emb_list = embeddings.cpu().tolist()
    # round for cleaner output
    emb_list = [[round(x, 6) for x in vec] for vec in emb_list]

    return EmbedResponse(embeddings=emb_list)


@app.post("/api/v1/anomaly/score", response_model=BatchScoreResp)
def anomaly_score(req: BatchScoreReq) -> BatchScoreResp:
    """Score events using CAE reconstruction error (ML) or heuristic fallback."""
    model = _get_model()
    extractor = _get_extractor()
    model_loaded = getattr(app.state, "model_loaded", False)

    if not req.events:
        return BatchScoreResp(scores=[])

    scores: list[EventScoreResp] = []

    if model_loaded and _trainer is not None:
        # ML-based scoring via reconstruction error
        events = [_event_to_dict(ev) for ev in req.events]
        features = extractor.extract_batch(events)
        batch = extractor.collate(features)
        device_batch = {k: v.to(_device) for k, v in batch.items()}

        with torch.no_grad():
            recon_errors = model.reconstruction_error(device_batch)

        # Normalize: recon_error / threshold → capped at 1.0
        threshold = max(_trainer.recon_threshold, 0.01)
        for i, ev in enumerate(req.events):
            raw = recon_errors[i].item() / threshold
            score_norm = min(raw, 1.0)
            scores.append(EventScoreResp(event_id=ev.event_id, anomaly_score=round(score_norm, 4)))
    else:
        # Heuristic fallback
        for ev in req.events:
            s = _heuristic_score(ev)
            scores.append(EventScoreResp(event_id=ev.event_id, anomaly_score=round(s, 4)))

    return BatchScoreResp(scores=scores)


@app.post("/api/v1/anomaly/train", response_model=TrainResponse)
def train_embedding(req: TrainRequest) -> TrainResponse:
    """Train (or fine-tune) the CAE embedding model on provided events."""
    global _trainer

    if not req.events:
        raise HTTPException(status_code=400, detail="No events provided")

    model = _get_model()
    extractor = _get_extractor()

    # fit vocabularies
    events = [_event_to_dict(ev) for ev in req.events]
    extractor.fit_on_events(events)

    # init or reuse trainer
    if _trainer is None:
        _trainer = CAETrainer(model, default_config, _device)
    else:
        # re-initialize optimizer for fine-tuning
        from torch.optim import AdamW
        _trainer.optimizer = AdamW(model.parameters(), lr=1e-4, weight_decay=1e-5)

    history = _trainer.fit(extractor, events, epochs=req.epochs)

    final_loss = history[-1]["loss_total"] if history else 0.0
    app.state.model_loaded = True
    app.state.training_events = len(events)

    # persist
    _model_path.parent.mkdir(parents=True, exist_ok=True)
    _trainer.save(str(_model_path))

    return TrainResponse(
        status="trained",
        final_loss=round(final_loss, 4),
        recon_threshold=round(_trainer.recon_threshold, 4),
        num_events=len(events),
    )


@app.post("/api/v1/cfg/analyze", response_model=CFGAnalyzeResp)
def cfg_analyze(req: CFGAnalyzeReq) -> CFGAnalyzeResp:
    """Analyze CFG control flow graph for structural anomalies.

    Uses GNN model when available; heuristic fallback otherwise.
    Phase 2 replaces the stub with real GAT inference.
    """
    # Try GNN inference if available
    gnn_available = getattr(app.state, "gnn_available", False)
    if gnn_available and hasattr(app.state, "gnn_inference"):
        try:
            result = app.state.gnn_inference.score_graph(req.graph_json, req.agent_id)
            return CFGAnalyzeResp(
                anomaly_detected=result["score"] >= 0.5,
                anomaly_score=round(result["score"], 4),
                details=result.get("details", ""),
            )
        except Exception:
            pass  # fall through to heuristic

    # Heuristic: parse graph_json for basic stats
    anomaly = False
    details = "CFG analysis: heuristic mode (GNN not loaded)"
    try:
        graph = json.loads(req.graph_json) if isinstance(req.graph_json, str) else json.loads(str(req.graph_json))
        nodes = graph.get("nodes", [])
        edges = graph.get("edges", [])

        # Simple heuristics
        if len(nodes) > 200:
            anomaly = True
            details = f"Large CFG: {len(nodes)} nodes, {len(edges)} edges (unusual for single agent)"
        elif len(nodes) == 0:
            anomaly = True
            details = "Empty CFG"
        elif len(edges) == 0 and len(nodes) > 1:
            anomaly = True
            details = "Disconnected CFG: multiple nodes with no transitions"
        else:
            details = f"CFG: {len(nodes)} nodes, {len(edges)} edges — normal structure"
    except (json.JSONDecodeError, TypeError):
        if len(req.graph_json) > 1_000_000:
            anomaly = True
            details = "Graph JSON exceeds 1MB threshold"
        else:
            details = "Unable to parse CFG JSON"

    score = 0.7 if anomaly else 0.1
    return CFGAnalyzeResp(
        anomaly_detected=anomaly,
        anomaly_score=score,
        details=details,
    )


# ── heuristic fallback ──

def _heuristic_score(event: AuditEventScoreReq) -> float:
    """Rule-based event scoring (used when ML model not loaded)."""
    score = 0.0

    sensitive = ["/etc/passwd", "/etc/shadow", "/root/", "/proc/", "/sys/"]
    for path in sensitive:
        if event.resource_ref.startswith(path):
            score += 0.5
            break

    risky_actions = ["write", "delete", "exec", "chmod", "chown", "mount"]
    for act in risky_actions:
        if act in event.action.lower():
            score += 0.2
            break

    if "network_dst" in event.attributes or "socket_create" in event.attributes:
        score += 0.3

    return min(score, 1.0)
