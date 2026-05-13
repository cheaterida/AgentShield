"""Convert raw AuditEvent dicts into token sequences for the CAE encoder."""

import math
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Optional

import torch

from .config import EmbeddingConfig, default_config


@dataclass
class AuditEventFeatures:
    """Pre-tokenized features extracted from a single audit event."""

    event_id: str
    agent_id: str
    action_idx: int            # index into action vocabulary
    path_indices: list[int]    # indices for each path component
    attr_key_indices: list[int]
    attr_val_indices: list[int]
    hour_sin: float
    hour_cos: float
    dayofweek_sin: float
    dayofweek_cos: float
    is_weekend: float

    def to_tensor_dict(self, config: EmbeddingConfig) -> dict[str, torch.Tensor]:
        """Convert to padded tensors ready for the encoder."""
        # action
        action = torch.tensor([self.action_idx], dtype=torch.long)

        # path — pad/truncate to max_seq_len
        path = _pad_sequence(self.path_indices, config.max_seq_len, 0)

        # attributes
        attr_keys = _pad_sequence(self.attr_key_indices, config.max_seq_len, 0)
        attr_vals = _pad_sequence(self.attr_val_indices, config.max_seq_len, 0)

        return {
            "action": action,
            "path": path,
            "attr_keys": attr_keys,
            "attr_vals": attr_vals,
            "temporal": torch.tensor(
                [[self.hour_sin, self.hour_cos, self.dayofweek_sin, self.dayofweek_cos,
                  self.is_weekend, 0, 0, 0, 0, 0]], dtype=torch.float32
            ),
        }


# ── path classification helpers (shared with cfg/resource_classifier) ──

SENSITIVE_PREFIXES = [
    "/etc/passwd", "/etc/shadow", "/root/", "/proc/", "/sys/",
    "/home/cheater/.ssh", "/var/run/docker.sock",
]

RESOURCE_CLASS_MAP: dict[tuple[str, ...], str] = {
    ("/etc/",): "CONFIG",
    ("/proc/", "/sys/"): "KERNEL",
    ("/data/", "/home/"): "USER_DATA",
    ("/tmp/", "/dev/shm/"): "TEMP",
    ("/var/run/", "/var/log/"): "SYSTEM",
}


def classify_resource(resource_ref: str) -> str:
    """Bucket a resource path into a high-level class for graph nodes."""
    if not resource_ref:
        return "OTHER"
    for prefixes, cls in RESOURCE_CLASS_MAP.items():
        for p in prefixes:
            if resource_ref.startswith(p):
                return cls
    return "OTHER"


def is_sensitive_path(resource_ref: str) -> bool:
    return any(resource_ref.startswith(p) for p in SENSITIVE_PREFIXES)


# ── temporal encoding ──

def _temporal_features(ts: Optional[str]) -> tuple[float, float, float, float, float]:
    """Encode a RFC3339 timestamp as cyclical features."""
    if ts is None:
        return 0.0, 1.0, 0.0, 1.0, 0.0  # default: midnight Sunday

    try:
        # Handle various ISO formats
        ts_clean = ts.replace("Z", "+00:00")
        dt = datetime.fromisoformat(ts_clean)
    except (ValueError, TypeError):
        return 0.0, 1.0, 0.0, 1.0, 0.0

    hour = dt.hour + dt.minute / 60.0
    dow = dt.weekday()  # 0=Monday .. 6=Sunday

    hour_sin = math.sin(2 * math.pi * hour / 24.0)
    hour_cos = math.cos(2 * math.pi * hour / 24.0)
    dow_sin = math.sin(2 * math.pi * dow / 7.0)
    dow_cos = math.cos(2 * math.pi * dow / 7.0)
    is_weekend = 1.0 if dow >= 5 else 0.0

    return hour_sin, hour_cos, dow_sin, dow_cos, is_weekend


# ── vocabulary helpers ──

def _tokenize_path(path: str, vocab: dict[str, int], max_len: int) -> list[int]:
    """Split a filesystem path into components and look up each."""
    parts = [p for p in path.strip("/").split("/") if p]
    indices = [vocab.get(p, 1) for p in parts]  # 1 = <unk>
    return indices[:max_len]


def _tokenize_attributes(
    attrs: dict[str, str],
    key_vocab: dict[str, int],
    val_vocab: dict[str, int],
    max_len: int,
) -> tuple[list[int], list[int]]:
    """Flatten key-value attribute pairs into parallel index lists."""
    keys: list[int] = []
    vals: list[int] = []
    for k, v in sorted(attrs.items()):
        if len(keys) >= max_len:
            break
        keys.append(key_vocab.get(k, 1))
        vals.append(val_vocab.get(v, 1))
    return keys, vals


def _pad_sequence(seq: list[int], target_len: int, pad_idx: int) -> torch.Tensor:
    t = seq[:target_len]
    t += [pad_idx] * (target_len - len(t))
    return torch.tensor(t, dtype=torch.long)


# ── main extractor ──

class FeatureExtractor:
    """Converts AuditEvent payloads into tokenised feature tensors.

    Maintains dynamic vocabularies that grow as new actions / paths / attributes
    are observed.  Vocab indices 0 and 1 are reserved for <pad> and <unk>.
    """

    def __init__(self, config: EmbeddingConfig | None = None):
        self.config = config or default_config
        self.config.validate()

        # dynamic vocabularies (populated during fit / incremental updates)
        self.action_vocab: dict[str, int] = {"<pad>": 0, "<unk>": 1}
        self.path_vocab: dict[str, int] = {"<pad>": 0, "<unk>": 1}
        self.attr_key_vocab: dict[str, int] = {"<pad>": 0, "<unk>": 1}
        self.attr_val_vocab: dict[str, int] = {"<pad>": 0, "<unk>": 1}

    # ── vocabulary management ──

    def _lookup_or_add(self, vocab: dict[str, int], token: str, max_size: int) -> int:
        idx = vocab.get(token)
        if idx is not None:
            return idx
        if len(vocab) >= max_size:
            return 1  # <unk>
        idx = len(vocab)
        vocab[token] = idx
        return idx

    def fit_on_events(self, events: list[dict]) -> None:
        """Pre-populate vocabularies from a corpus of events."""
        for ev in events:
            action = ev.get("action", "")
            if action:
                self._lookup_or_add(self.action_vocab, action.lower(), self.config.max_action_vocab)

            resource = ev.get("resource_ref", "")
            if resource:
                for part in resource.strip("/").split("/"):
                    if part:
                        self._lookup_or_add(self.path_vocab, part, self.config.max_path_vocab)

            attrs: dict[str, str] = ev.get("attributes", {}) or {}
            for k, v in attrs.items():
                self._lookup_or_add(self.attr_key_vocab, k, self.config.max_attr_key_vocab)
                self._lookup_or_add(self.attr_val_vocab, v, self.config.max_attr_key_vocab)

    # ── single-event extraction ──

    def extract(self, event: dict) -> AuditEventFeatures:
        """Convert a single audit event dict into feature representation."""
        cfg = self.config

        action = event.get("action", "unknown").lower()
        action_idx = self._lookup_or_add(self.action_vocab, action, cfg.max_action_vocab)

        resource = event.get("resource_ref", "")
        path_indices = _tokenize_path(resource, self.path_vocab, cfg.max_seq_len)

        attrs: dict[str, str] = event.get("attributes", {}) or {}
        attr_keys, attr_vals = _tokenize_attributes(
            attrs, self.attr_key_vocab, self.attr_val_vocab, cfg.max_seq_len
        )

        hour_sin, hour_cos, dow_sin, dow_cos, is_weekend = _temporal_features(
            event.get("occurred_at")
        )

        return AuditEventFeatures(
            event_id=event.get("event_id", ""),
            agent_id=event.get("agent_id", ""),
            action_idx=action_idx,
            path_indices=path_indices,
            attr_key_indices=attr_keys,
            attr_val_indices=attr_vals,
            hour_sin=hour_sin,
            hour_cos=hour_cos,
            dayofweek_sin=dow_sin,
            dayofweek_cos=dow_cos,
            is_weekend=is_weekend,
        )

    def extract_batch(self, events: list[dict]) -> list[AuditEventFeatures]:
        return [self.extract(ev) for ev in events]

    # ── tensor batch for model input ──

    def collate(
        self, features: list[AuditEventFeatures]
    ) -> dict[str, torch.Tensor]:
        """Stack a list of extracted features into a batched tensor dict."""
        cfg = self.config

        actions = torch.stack([f.to_tensor_dict(cfg)["action"] for f in features])
        paths = torch.stack([f.to_tensor_dict(cfg)["path"] for f in features])
        attr_keys = torch.stack([f.to_tensor_dict(cfg)["attr_keys"] for f in features])
        attr_vals = torch.stack([f.to_tensor_dict(cfg)["attr_vals"] for f in features])
        temporal = torch.stack([f.to_tensor_dict(cfg)["temporal"] for f in features])

        return {
            "action": actions.squeeze(1),               # [B]
            "path": paths,                               # [B, max_seq_len]
            "attr_keys": attr_keys,                      # [B, max_seq_len]
            "attr_vals": attr_vals,                      # [B, max_seq_len]
            "temporal": temporal.squeeze(1),             # [B, 10]
        }
