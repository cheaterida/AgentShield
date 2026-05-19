"""Tests for CFG graph building."""

from agentshield_ml.cfg.builder import (
    CFGBuilder,
    CFGGraph,
    CFGNode,
    CFGEdge,
    ResourceClassifier,
)


def test_cfg_node_label():
    node = CFGNode(1, "read", "FILE", "/data/test.csv")
    assert node.label == "read→FILE"


def test_cfg_edge_transition():
    edge = CFGEdge(0, 1)
    edge.add_transition(0.5)
    edge.add_transition(1.5)
    assert edge.weight == 3  # initial 1 + 2 calls
    assert edge.avg_time_delta_sec == 1.0


def test_cfg_graph_add_event():
    graph = CFGGraph("agent-1", window_size=200)
    graph.add_event(
        event_idx=0,
        action="read",
        resource_class="FILE",
        resource_ref="/data/test.csv",
    )
    assert len(graph.nodes) == 1
    node = list(graph.nodes.values())[0]
    assert node.action == "read"
    assert node.resource_class == "FILE"
    assert node.frequency == 1


def test_cfg_graph_edge_creation():
    graph = CFGGraph("agent-1", window_size=200)
    graph.add_event(0, "read", "FILE", "/data/test.csv")
    graph.add_event(
        1, "write", "FILE", "/data/output.json",
        prev_action="read", prev_resource_class="FILE",
    )
    assert len(graph.edges) == 1
    edge = list(graph.edges.values())[0]
    assert edge.src == 0
    assert edge.dst == 1


def test_cfg_graph_node_deduplication():
    graph = CFGGraph("agent-1", window_size=200)
    graph.add_event(0, "read", "FILE", "/data/test.csv")
    graph.add_event(
        1, "read", "FILE", "/data/test2.csv",
        prev_action="read", prev_resource_class="FILE",
    )
    # Same label "read→FILE" should reuse node
    assert len(graph.nodes) == 1
    node = list(graph.nodes.values())[0]
    assert node.frequency == 2  # visited twice


def test_cfg_graph_to_dgl_empty():
    graph = CFGGraph("agent-1", window_size=200)
    dgl_graph, node_feats, edge_feats = graph.to_dgl()
    # Empty graph should return zero-dim tensors
    assert node_feats.numel() == 0
    assert edge_feats.numel() == 0


def test_cfg_builder_builds_graph():
    builder = CFGBuilder(window_size=200)
    events = [
        {"action": "read", "resource_ref": "/data/test.csv", "attributes": {}},
        {"action": "write", "resource_ref": "/data/output.json", "attributes": {}},
    ]
    graph = builder.build("agent-1", events)
    assert graph.agent_id == "agent-1"
    assert len(graph.nodes) >= 1


def test_cfg_builder_window_truncation():
    builder = CFGBuilder(window_size=5)
    events = [
        {"action": f"act{i}", "resource_ref": f"/data/{i}", "attributes": {}}
        for i in range(20)
    ]
    graph = builder.build("agent-1", events)
    # Window size 5 + deduplication of labels
    assert len(graph.nodes) <= 5


def test_resource_classifier():
    classifier = ResourceClassifier()
    assert classifier.classify("/etc/passwd", "read", {}) == "CONFIG"
    assert classifier.classify("/proc/self/status", "read", {}) == "KERNEL"
    assert classifier.classify("/sys/class/net", "read", {}) == "KERNEL"
    assert classifier.classify("/home/user/data.csv", "read", {}) == "USER_DATA"
    assert classifier.classify("/tmp/cache.db", "read", {}) == "TEMP"
