package agentshield.approval

# ── AgentShield Three-Tier Approval Policy (Track B3) ──
#
# Determines the required approval tier for agent actions based on
# risk score and action type. The Go backend queries this policy to
# decide whether to auto-approve, request department lead approval,
# escalate to the security team, or escalate to the CISO.
#
# Query: POST /v1/data/agentshield/approval

# Auto-approve: low-risk read operations require no human approval.
default require_approval := false

approval_tier := "none" if {
	input.risk_score < 0.3
	input.action in {"read", "list", "query"}
}

# Department-level: moderate risk or standard write/exec operations.
approval_tier := "department" if {
	input.risk_score >= 0.3
	input.risk_score < 0.6
}

approval_tier := "department" if {
	input.action in {"write", "exec", "deploy"}
	not input.risk_score >= 0.6
}

# Security team: high risk or destructive / privileged operations.
approval_tier := "security" if {
	input.risk_score >= 0.6
	input.risk_score < 0.8
}

approval_tier := "security" if {
	input.action in {"delete", "grant", "network_connect"}
	not input.risk_score >= 0.8
}

# CISO: critical risk — immediate CISO escalation required.
approval_tier := "ciso" if {
	input.risk_score >= 0.8
}

# A request requires approval if the tier is not "none".
require_approval if {
	approval_tier != "none"
}
