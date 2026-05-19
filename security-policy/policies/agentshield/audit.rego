package agentshield.audit

import rego.v1

# ── 审计事件策略评估 ──
# 对每个审计事件进行 OPA 判决，返回结构化结果

default allow := false

# 允许正常操作：已知 family_group + 合法 action
allow if {
	input.subject.family_group_id != ""
	input.action in {"read", "invoke", "register", "chat", "completion", "heartbeat"}
}

# ── 敏感路径检测 ──
default deny_sensitive_path := false

deny_sensitive_path if {
	input.resource_ref != ""
	some path in sensitive_paths
	startswith(input.resource_ref, path)
}

sensitive_paths := [
	"/etc/passwd",
	"/etc/shadow",
	"/root/",
	"/proc/",
	"/sys/kernel/",
	"/var/run/docker.sock",

]

default matched_sensitive_path := ""

# Longest-matching sensitive path (deterministic single value).
# When a resource matches multiple prefixes (e.g. /proc/sys/kernel
# matches both /proc/ and /sys/kernel/), pick the longest.
matched_sensitive_path := path if {
	some path in sensitive_paths
	startswith(input.resource_ref, path)
	not longer_match_exists(path)
}

longer_match_exists(current) if {
	some other in sensitive_paths
	startswith(input.resource_ref, other)
	count(other) > count(current)
}

# ── 网络访问控制 ──
default deny_network := false

deny_network if {
	input.action == "network_connect"
	input.destination != ""
	net_is_restricted(input.destination)
}

net_is_restricted(dst) if startswith(dst, "10.")
net_is_restricted(dst) if startswith(dst, "192.168.")

# RFC 1918 /12: 172.16.0.0 – 172.31.255.255
net_is_restricted(dst) if {
	startswith(dst, "172.")
	octets := split(dst, ".")
	count(octets) >= 2
	second := to_number(octets[1])
	second >= 16
	second <= 31
}

# ── 写操作风险 ──
default risky_write := false

risky_write if {
	input.action in {"write", "delete", "exec", "chmod", "chown", "mount"}
}

# ── 综合风险等级 ──
# 规则按优先级排列，条件互斥避免冲突

risk_level := "critical" if {
	deny_sensitive_path
	input.risk_score >= 0.5
}

risk_level := "critical" if {
	not deny_sensitive_path
	input.risk_score >= 0.8
}

risk_level := "high" if {
	deny_sensitive_path
	input.risk_score < 0.5
}

risk_level := "high" if {
	deny_network
	not deny_sensitive_path
}

risk_level := "high" if {
	risky_write
	not deny_sensitive_path
	not deny_network
}

risk_level := "high" if {
	not deny_sensitive_path
	not deny_network
	not risky_write
	input.risk_score >= 0.6
	input.risk_score < 0.8
}

risk_level := "medium" if {
	not deny_sensitive_path
	not deny_network
	not risky_write
	input.risk_score >= 0.3
	input.risk_score < 0.6
}

risk_level := "low" if {
	not deny_sensitive_path
	not deny_network
	not risky_write
	input.risk_score < 0.3
}

risk_level := "medium" if {
	risky_write
	not deny_sensitive_path
	not deny_network
	input.risk_score < 0.3
}

# ── 汇总判决 ──
# 查询 /v1/data/agentshield/audit 可获取以上全部规则结果
