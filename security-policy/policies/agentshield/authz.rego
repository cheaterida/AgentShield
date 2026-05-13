package agentshield.authz

import rego.v1

# ── 准入控制 ──
# 默认拒绝所有请求
default allow := false

# 允许注册 agent：主体必须属于已知 family_group 且 action 合法
allow if {
	input.subject.family_group_id != ""
	input.action in {"read", "invoke", "register"}
}

# ── 资源访问控制 ──
# 禁止访问敏感路径
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
	"/home/cheater/.ssh",
]

# ── 网络访问控制 ──
# 禁止出站到内网保留地址（需显式允许）
deny_network if {
	input.action == "network_connect"
	input.destination != ""
	net_is_restricted(input.destination)
}

net_is_restricted(dst) if {
	startswith(dst, "10.")
}
net_is_restricted(dst) if {
	startswith(dst, "172.16.")
}
net_is_restricted(dst) if {
	startswith(dst, "192.168.")
}

# ── 速率限制辅助 ──
# 单个 agent 短时间内高频操作触发告警
rate_limit_alert if {
	input.event_count > 100
	input.window_seconds < 60
}

# ── 风险分类辅助 ──
risk_level := "low" if {
	input.risk_score < 0.3
}
risk_level := "medium" if {
	input.risk_score >= 0.3
	input.risk_score < 0.6
}
risk_level := "high" if {
	input.risk_score >= 0.6
	input.risk_score < 0.8
}
risk_level := "critical" if {
	input.risk_score >= 0.8
}
