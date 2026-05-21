package agentshield.common

import rego.v1

# ── Shared constants & helpers ──

# Known LLM API endpoints (public cloud AI services)
known_llm_endpoints := [
	# DeepSeek
	{"ip_prefix": "175.", "port": 443, "label": "DeepSeek API"},
	# OpenAI
	{"ip_prefix": "104.18.", "port": 443, "label": "OpenAI API"},
	{"ip_prefix": "104.19.", "port": 443, "label": "OpenAI API"},
	# Anthropic
	{"ip_prefix": "34.168.", "port": 443, "label": "Anthropic API"},
	# Google / Gemini
	{"ip_prefix": "142.250.", "port": 443, "label": "Google API"},
	{"ip_prefix": "172.217.", "port": 443, "label": "Google API"},
	# Cloudflare (many AI APIs sit behind CF)
	{"ip_prefix": "104.21.", "port": 443, "label": "Cloudflare"},
]

# Well-known safe system tools (commonly used by AI agents)
known_safe_tools := [
	"/usr/bin/uname",
	"/usr/bin/git",
	"/usr/bin/python3",
	"/usr/bin/python",
	"/usr/bin/node",
	"/usr/bin/npm",
	"/usr/bin/pip",
	"/usr/bin/pip3",
	"/usr/bin/curl",
	"/usr/bin/wget",
	"/usr/bin/cat",
	"/usr/bin/ls",
	"/usr/bin/df",
	"/usr/bin/du",
	"/usr/bin/stat",
	"/usr/bin/file",
	"/usr/bin/grep",
	"/usr/bin/find",
	"/usr/bin/which",
	"/usr/sbin/uname",
	"/usr/local/bin/uname",
	"/usr/local/bin/git",
]

# DNS resolver destinations (systemd-resolved, etc.)
dns_endpoints := [
	{"ip_prefix": "127.0.0.", "port": 53},
	{"ip_prefix": "::1", "port": 53},
]

# AgentShield internal endpoints
agentshield_endpoints := [
	{"ip_prefix": "100.123.70.98", "port": 8081, "label": "AgentShield Trace API"},
	{"ip_prefix": "100.123.70.98", "port": 8080, "label": "AgentShield Management"},
]

is_known_llm_endpoint(dst) if {
	some ep in known_llm_endpoints
	startswith(dst, ep.ip_prefix)
	endswith(dst, sprintf(":%d", [ep.port]))
}

is_dns_endpoint(dst) if {
	some ep in dns_endpoints
	startswith(dst, ep.ip_prefix)
	endswith(dst, sprintf(":%d", [ep.port]))
}

is_agentshield_endpoint(dst) if {
	some ep in agentshield_endpoints
	startswith(dst, ep.ip_prefix)
	endswith(dst, sprintf(":%d", [ep.port]))
}

# Any well-known safe destination (LLM API + DNS + AgentShield)
is_safe_destination(dst) if is_known_llm_endpoint(dst)
is_safe_destination(dst) if is_dns_endpoint(dst)
is_safe_destination(dst) if is_agentshield_endpoint(dst)

is_known_safe_tool(path) if {
	some tool in known_safe_tools
	path == tool
}
