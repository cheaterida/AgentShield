"""Resource path classifier — maps filesystem paths to high-level resource classes
for CFG node construction.  Also used by the embedding feature extractor."""

# Re-export from embedding.feature_extractor to avoid duplication
from ..embedding.feature_extractor import (
    classify_resource,
    is_sensitive_path,
    SENSITIVE_PREFIXES,
    RESOURCE_CLASS_MAP,
)


class ResourceClassifier:
    """Stateless classifier for resource_ref → resource_class."""

    # Standard classification rules
    RULES: list[tuple[tuple[str, ...], str]] = [
        (("/etc/",), "CONFIG"),
        (("/proc/", "/sys/"), "KERNEL"),
        (("/data/", "/home/"), "USER_DATA"),
        (("/tmp/", "/dev/shm/"), "TEMP"),
        (("/var/run/", "/var/log/"), "SYSTEM"),
    ]

    # Extra rules for network-like attributes
    NETWORK_ACTION_PREFIXES = ("network_", "socket_", "connect", "bind", "listen")

    @classmethod
    def classify(cls, resource_ref: str, action: str = "", attributes: dict[str, str] | None = None) -> str:
        """Classify a resource reference into a high-level category.

        Priority:
          1. Explicit path matches (CONFIG, KERNEL, USER_DATA, TEMP, SYSTEM)
          2. Network-related actions/attributes → NETWORK
          3. Everything else → OTHER
        """
        # Check network signals first
        if cls._is_network(resource_ref, action, attributes or {}):
            return "NETWORK"

        return classify_resource(resource_ref)

    @classmethod
    def _is_network(cls, resource_ref: str, action: str, attributes: dict[str, str]) -> bool:
        if resource_ref.startswith(("tcp://", "udp://", "http://", "https://", "ws://")):
            return True
        if any(action.lower().startswith(p) for p in cls.NETWORK_ACTION_PREFIXES):
            return True
        if any(k.startswith("network_") or k.startswith("socket_") for k in attributes):
            return True
        if "destination" in attributes or "dst_addr" in attributes or "dst_port" in attributes:
            return True
        return False

    @classmethod
    def all_classes(cls) -> list[str]:
        return ["CONFIG", "KERNEL", "USER_DATA", "TEMP", "SYSTEM", "NETWORK", "OTHER"]
