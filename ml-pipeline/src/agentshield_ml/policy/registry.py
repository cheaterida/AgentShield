"""Policy Registry — manages versioned ML policy lifecycle.

Integrates with the management-server's PolicyBundle store for persistence.
When running standalone (no management-server), uses local file storage.
"""

import json
import logging
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)


class PolicyRecord:
    """Metadata record for a stored policy."""

    def __init__(
        self,
        family_group_id: str,
        version: str,
        policy_type: str = "gnn_policy",
        active: bool = False,
    ):
        self.family_group_id = family_group_id
        self.version = version
        self.policy_type = policy_type
        self.active = active
        self.training_events_count: int = 0
        self.r_max: float = 1.0
        self.file_path: str = ""
        self.created_at: str = datetime.now(timezone.utc).isoformat()
        self.activated_at: Optional[str] = None

    def to_dict(self) -> dict:
        return {
            "family_group_id": self.family_group_id,
            "version": self.version,
            "type": self.policy_type,
            "active": self.active,
            "training_events_count": self.training_events_count,
            "r_max": self.r_max,
            "file_path": self.file_path,
            "created_at": self.created_at,
            "activated_at": self.activated_at,
        }


class PolicyRegistry:
    """Local file-based registry for ML policies.

    In production, this is backed by the management-server's SQLite store.
    The registry provides a local cache and API client for the server.
    """

    def __init__(
        self,
        storage_dir: str | Path = "./policies",
        management_server_url: str = "http://localhost:8080",
    ):
        self.storage_dir = Path(storage_dir)
        self.storage_dir.mkdir(parents=True, exist_ok=True)
        self.mgmt_url = management_server_url.rstrip("/")
        self._index: dict[str, dict[str, PolicyRecord]] = {}  # family_group_id → version → record
        self._load_index()

    def _index_path(self) -> Path:
        return self.storage_dir / "registry_index.json"

    def _load_index(self) -> None:
        idx_path = self._index_path()
        if idx_path.exists():
            try:
                data = json.loads(idx_path.read_text())
                for fg_id, versions in data.items():
                    self._index[fg_id] = {}
                    for ver, rec_data in versions.items():
                        rec = PolicyRecord(rec_data["family_group_id"], rec_data["version"])
                        rec.policy_type = rec_data.get("type", "gnn_policy")
                        rec.active = rec_data.get("active", False)
                        rec.training_events_count = rec_data.get("training_events_count", 0)
                        rec.r_max = rec_data.get("r_max", 1.0)
                        rec.file_path = rec_data.get("file_path", "")
                        rec.created_at = rec_data.get("created_at", "")
                        rec.activated_at = rec_data.get("activated_at")
                        self._index[fg_id][ver] = rec
            except (json.JSONDecodeError, KeyError) as e:
                logger.warning("Failed to load registry index: %s", e)
                self._index = {}

    def _save_index(self) -> None:
        data = {}
        for fg_id, versions in self._index.items():
            data[fg_id] = {ver: rec.to_dict() for ver, rec in versions.items()}
        self._index_path().write_text(json.dumps(data, indent=2))

    def register(
        self,
        family_group_id: str,
        version: str,
        bundle_path: str | Path,
        training_events_count: int = 0,
        r_max: float = 1.0,
    ) -> PolicyRecord:
        """Register a new policy version."""
        rec = PolicyRecord(family_group_id, version)
        rec.file_path = str(bundle_path)
        rec.training_events_count = training_events_count
        rec.r_max = r_max

        if family_group_id not in self._index:
            self._index[family_group_id] = {}
        self._index[family_group_id][version] = rec
        self._save_index()

        logger.info("Registered policy %s v%s", family_group_id, version)
        return rec

    def activate(self, family_group_id: str, version: str) -> Optional[PolicyRecord]:
        """Activate a policy version, deactivating all others for this family group."""
        if family_group_id not in self._index:
            logger.warning("No policies for family group %s", family_group_id)
            return None

        if version not in self._index[family_group_id]:
            logger.warning("Policy %s v%s not found", family_group_id, version)
            return None

        # deactivate all
        for rec in self._index[family_group_id].values():
            rec.active = False

        # activate target
        rec = self._index[family_group_id][version]
        rec.active = True
        rec.activated_at = datetime.now(timezone.utc).isoformat()
        self._save_index()

        logger.info("Activated policy %s v%s", family_group_id, version)
        return rec

    def get_active(self, family_group_id: str) -> Optional[PolicyRecord]:
        """Get the currently active policy for a family group."""
        if family_group_id not in self._index:
            return None
        for rec in self._index[family_group_id].values():
            if rec.active:
                return rec
        return None

    def list_versions(self, family_group_id: str) -> list[PolicyRecord]:
        """List all policy versions for a family group, newest first."""
        if family_group_id not in self._index:
            return []
        return sorted(
            self._index[family_group_id].values(),
            key=lambda r: r.created_at,
            reverse=True,
        )

    def list_all(self) -> dict[str, list[PolicyRecord]]:
        """List policies for all family groups."""
        result = {}
        for fg_id in self._index:
            result[fg_id] = self.list_versions(fg_id)
        return result

    def rollback(self, family_group_id: str) -> Optional[PolicyRecord]:
        """Rollback to the previous active version."""
        versions = self.list_versions(family_group_id)
        if len(versions) < 2:
            logger.warning("Not enough versions to rollback for %s", family_group_id)
            return None

        # find currently active
        current_idx = None
        for i, rec in enumerate(versions):
            if rec.active:
                current_idx = i
                break

        if current_idx is None:
            # activate the latest
            return self.activate(family_group_id, versions[0].version)

        # activate the next oldest
        prev_idx = current_idx + 1
        if prev_idx >= len(versions):
            prev_idx = 0  # wrap to newest if no older

        return self.activate(family_group_id, versions[prev_idx].version)
