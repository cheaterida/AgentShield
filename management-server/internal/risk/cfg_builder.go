package risk

import (
	"encoding/json"
	"sort"
)

// cfgNode is a lightweight node for the CFG graph JSON sent to the ML pipeline.
type cfgNode struct {
	NodeID        int    `json:"node_id"`
	Action        string `json:"action"`
	ResourceClass string `json:"resource_class"`
	ResourceRef   string `json:"resource_ref"`
	Frequency     int    `json:"frequency"`
	Label         string `json:"label"`
}

// cfgEdge is a lightweight edge for the CFG graph JSON.
type cfgEdge struct {
	Src              int     `json:"src"`
	Dst              int     `json:"dst"`
	Weight           int     `json:"weight"`
	AvgTimeDeltaSec  float64 `json:"avg_time_delta_sec"`
}

type cfgGraph struct {
	AgentID     string    `json:"agent_id"`
	TotalEvents int       `json:"total_events"`
	Nodes       []cfgNode `json:"nodes"`
	Edges       []cfgEdge `json:"edges"`
}

// resourceClass classifies a resource path into a high-level category.
func resourceClass(resourceRef string) string {
	if resourceRef == "" {
		return "UNKNOWN"
	}
	// Check prefix matches
	prefixes := []struct {
		prefix string
		class  string
	}{
		{"/etc/", "CONFIG"},
		{"/proc/", "KERNEL"},
		{"/sys/", "KERNEL"},
		{"/dev/", "DEVICE"},
		{"/tmp/", "TEMP"},
		{"/var/tmp/", "TEMP"},
		{"/home/", "USER_DATA"},
		{"/root/", "USER_DATA"},
		{"/data/", "USER_DATA"},
		{"/mnt/", "MOUNT"},
		{"/lib/", "LIBRARY"},
		{"/usr/lib/", "LIBRARY"},
		{"/usr/bin/", "EXECUTABLE"},
		{"/bin/", "EXECUTABLE"},
		{"/sbin/", "EXECUTABLE"},
		{"/opt/", "APPLICATION"},
	}
	for _, p := range prefixes {
		if len(resourceRef) >= len(p.prefix) && resourceRef[:len(p.prefix)] == p.prefix {
			return p.class
		}
	}
	// Check for IP:port patterns
	if len(resourceRef) > 0 && (resourceRef[0] >= '0' && resourceRef[0] <= '9') {
		return "NETWORK"
	}
	return "OTHER"
}

// buildCFGJSON builds a lightweight CFG graph JSON string from a sequence of events.
func buildCFGJSON(agentID string, actions, resourceRefs []string) string {
	if len(actions) == 0 {
		return "{}"
	}

	// Deduplicate (action, resourceClass) pairs as nodes
	type nodeKey struct {
		action string
		rc     string
	}
	nodeIdx := make(map[nodeKey]int)
	var nodes []cfgNode

	// First pass: collect unique nodes
	for i, action := range actions {
		ref := ""
		if i < len(resourceRefs) {
			ref = resourceRefs[i]
		}
		rc := resourceClass(ref)
		nk := nodeKey{action: action, rc: rc}
		if _, ok := nodeIdx[nk]; !ok {
			nid := len(nodes)
			nodeIdx[nk] = nid
			nodes = append(nodes, cfgNode{
				NodeID:        nid,
				Action:        action,
				ResourceClass: rc,
				ResourceRef:   ref,
				Frequency:     1,
				Label:         action + "→" + rc,
			})
		} else {
			nodes[nodeIdx[nk]].Frequency++
		}
	}

	// Second pass: create edges for consecutive events
	type edgeKey struct {
		src, dst int
	}
	edgeWeights := make(map[edgeKey]int)
	var edgeOrder []edgeKey

	prevID := -1
	for i, action := range actions {
		ref := ""
		if i < len(resourceRefs) {
			ref = resourceRefs[i]
		}
		rc := resourceClass(ref)
		nk := nodeKey{action: action, rc: rc}
		curID := nodeIdx[nk]

		if prevID >= 0 {
			ek := edgeKey{src: prevID, dst: curID}
			if _, ok := edgeWeights[ek]; !ok {
				edgeOrder = append(edgeOrder, ek)
			}
			edgeWeights[ek]++
		}
		prevID = curID
	}

	var edges []cfgEdge
	for _, ek := range edgeOrder {
		edges = append(edges, cfgEdge{
			Src:             ek.src,
			Dst:             ek.dst,
			Weight:          edgeWeights[ek],
			AvgTimeDeltaSec: 0.0,
		})
	}

	// Sort nodes by frequency descending for consistent output
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Frequency > nodes[j].Frequency
	})

	g := cfgGraph{
		AgentID:     agentID,
		TotalEvents: len(actions),
		Nodes:       nodes,
		Edges:       edges,
	}

	b, _ := json.Marshal(g)
	return string(b)
}
