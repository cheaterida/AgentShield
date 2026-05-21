package agentshield.audit

import rego.v1
import data.agentshield.common

# ── AgentShield Audit Policy ──
# Incorporates enterprise-grade rules from Microsoft Agent Governance Toolkit:
#   - Credential & API key protection
#   - PII / financial data / PHI detection
#   - Safe logging & encryption requirements
#   - Bulk data export controls
#   - Network boundary enforcement (RFC 1918)
#   - Sensitive path access control
#   - Tiered risk classification
#
# Query: POST /v1/data/agentshield/audit
# Go reads: allow, deny_sensitive_path, deny_network, risky_write,
#           risk_level, matched_path, matched_destination

# ================================================================
# 1. ALLOW / DENY
# ================================================================

default allow := false

# ── Network ──

allow if {
	input.action == "network_connect"
	common.is_known_llm_endpoint(input.resource_ref)
}

allow if {
	input.action == "network_connect"
	common.is_dns_endpoint(input.resource_ref)
}

allow if {
	input.action == "network_connect"
	common.is_agentshield_endpoint(input.resource_ref)
}

# ── Execution ──

allow if {
	input.action == "exec"
	common.is_known_safe_tool(input.resource_ref)
}

# ── File I/O ──

allow if {
	input.action == "read"
	not deny_sensitive_path
	not deny_credential_leak
}

allow if {
	input.action in {"write", "socket_create"}
	not deny_sensitive_path
}

# ================================================================
# 2. CREDENTIAL & SECRET DETECTION
#    (from AGT enterprise.yaml credential_protection)
# ================================================================

default deny_credential_leak := false

# OpenAI API keys: sk-...
deny_credential_leak if {
	regex.match(`sk-[A-Za-z0-9_-]{32,}`, input.resource_ref)
}

# GitHub personal access tokens: ghp_...
deny_credential_leak if {
	regex.match(`ghp_[A-Za-z0-9]{36}`, input.resource_ref)
}

# AWS Access Keys: AKIA...
deny_credential_leak if {
	regex.match(`AKIA[A-Z0-9]{16}`, input.resource_ref)
}

# Generic credential patterns
deny_credential_leak if {
	regex.match(`(?i)(password|api[_-]?key|secret|token)\s*[:=]\s*["\x27][^"\x27]+["\x27]`, input.resource_ref)
}

# JWT tokens
deny_credential_leak if {
	regex.match(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9._-]{10,}`, input.resource_ref)
}

# ================================================================
# 3. SENSITIVE PATH DETECTION
#    (from AGT data-protection.yaml + enterprise.yaml)
# ================================================================

default deny_sensitive_path := false

sensitive_paths := [
	# System authentication
	"/etc/passwd",
	"/etc/shadow",
	"/etc/sudoers",
	"/etc/sudoers.d/",
	"/etc/ssl/private/",
	"/etc/ssh/",
	# Root / privileged access
	"/root/",
	"/root/.ssh/",
	# Kernel & system
	"/proc/sys/kernel/",
	"/proc/sys/net/",
	"/sys/kernel/security/",
	"/boot/",
	"/lib/modules/",
	"/usr/lib/systemd/",
	# Container escape
	"/var/run/docker.sock",
	"/var/run/docker/",
	"/run/containerd/",
	# Audit & security logs
	"/var/log/audit/",
	"/var/log/secure",
	"/var/log/auth.log",
	# Credential stores
	"/.env",
	".bash_history",
	".ssh/id_",
	".gnupg/",
	"credentials",
	# Database files
	".db",
	".sqlite",
	".sqlite3",
]

deny_sensitive_path if {
	input.resource_ref != ""
	some path in sensitive_paths
	contains(input.resource_ref, path)
}

default matched_path := ""
matched_path := path if {
	some path in sensitive_paths
	contains(input.resource_ref, path)
	not longer_match_exists(path)
}

longer_match_exists(current) if {
	some other in sensitive_paths
	contains(input.resource_ref, other)
	count(other) > count(current)
}

# ================================================================
# 4. NETWORK ACCESS CONTROL
#    (from AGT enterprise.yaml network_restrictions)
# ================================================================

default deny_network := false

# Deny connections to private/internal IP ranges unless explicitly allowed
deny_network if {
	input.action == "network_connect"
	not common.is_safe_destination(input.resource_ref)
	ip_is_private(input.resource_ref)
}

# Deny connections to unapproved external endpoints (not known LLM APIs)
# In enterprise mode, external traffic must go through known endpoints
deny_network if {
	input.action == "network_connect"
	not common.is_safe_destination(input.resource_ref)
	not common.is_known_llm_endpoint(input.resource_ref)
	not ip_is_private(input.resource_ref)
	not ip_is_loopback(input.resource_ref)
}

# Private / loopback / CGNAT / link-local detection
ip_is_private(dst) if { startswith(dst, "10.") }
ip_is_private(dst) if { startswith(dst, "192.168.") }
ip_is_private(dst) if {
	startswith(dst, "172.")
	o := split(dst, ".")
	count(o) >= 2
	n := to_number(o[1])
	n >= 16; n <= 31
}
ip_is_private(dst) if { startswith(dst, "100.64.") }
ip_is_private(dst) if { startswith(dst, "169.254.") }

ip_is_loopback(dst) if { startswith(dst, "127.") }
ip_is_loopback(dst) if { startswith(dst, "::1") }

default matched_destination := ""
matched_destination := input.resource_ref if { deny_network }

# ================================================================
# 5. PII / SENSITIVE DATA DETECTION
#    (from AGT data-protection.yaml)
# ================================================================

default deny_pii := false

# SSN (with dashes: 123-45-6789)
deny_pii if {
	regex.match(`\b\d{3}-\d{2}-\d{4}\b`, input.resource_ref)
}

# SSN (without dashes: 123456789)
deny_pii if {
	regex.match(`\b\d{9}\b`, input.resource_ref)
}

# Credit card: Visa, MasterCard, Amex, Discover
deny_pii if {
	regex.match(`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12})\b`, input.resource_ref)
}

# Email addresses
deny_pii if {
	regex.match(`(?i)(email|e-mail)\s*[:=]\s*["\x27]?[\w.+-]+@[\w-]+\.[\w.-]+["\x27]?`, input.resource_ref)
}

# Phone numbers
deny_pii if {
	regex.match(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`, input.resource_ref)
}

# ================================================================
# 6. FINANCIAL DATA DETECTION
#    (from AGT data-protection.yaml)
# ================================================================

default deny_financial_data := false

# Bank account numbers (8-17 digits)
deny_financial_data if {
	regex.match(`(?i)(account|acct)\s*(?:number|#|no)?\s*[:=]?\s*\d{8,17}`, input.resource_ref)
}

# Routing numbers (9 digits)
deny_financial_data if {
	regex.match(`(?i)routing\s*(?:number|#)?\s*[:=]?\s*\d{9}`, input.resource_ref)
}

# CVV/CVC codes
deny_financial_data if {
	regex.match(`(?i)(cvv|cvc|cv2)\s*[:=]?\s*\d{3,4}`, input.resource_ref)
}

# ================================================================
# 7. UNSAFE CODING PRACTICES
#    (from AGT data-protection.yaml + enterprise.yaml)
# ================================================================

default deny_unsafe_coding := false

# Plaintext credentials in code
deny_unsafe_coding if {
	regex.match(`(?i)(password|secret|key)\s*=\s*["\x27][^"\x27]+["\x27]`, input.resource_ref)
}

# Weak hashing (MD5/SHA1)
deny_unsafe_coding if {
	regex.match(`(?i)(md5|sha1)\s*\(`, input.resource_ref)
}

# Bulk data export
deny_unsafe_coding if {
	regex.match(`(?i)(pg_dump|mysqldump|mongodump)`, input.resource_ref)
}

deny_unsafe_coding if {
	regex.match(`(?i)SELECT\s+\*\s+FROM\s+\w+\s*;`, input.resource_ref)
}

# Dangerous shell commands
deny_unsafe_coding if {
	regex.match(`(?i)(rm\s+-rf|del\s+/[sS]|format\s+[a-zA-Z]:)`, input.resource_ref)
}

deny_unsafe_coding if {
	regex.match(`(?i)(curl|wget)\s+.*\|\s*(bash|sh|python)`, input.resource_ref)
}

deny_unsafe_coding if {
	regex.match(`(?i)chmod\s+777`, input.resource_ref)
}

# ================================================================
# 8. WRITE / EXEC RISK
# ================================================================

default risky_write := false

risky_write if {
	input.action == "exec"
	not common.is_known_safe_tool(input.resource_ref)
}

risky_write if {
	input.action == "write"
	not startswith(input.resource_ref, "/home/")
	not startswith(input.resource_ref, "/tmp/")
	not startswith(input.resource_ref, "/var/tmp/")
	not startswith(input.resource_ref, "/workspace/")
	not startswith(input.resource_ref, "/data/")
}

# ================================================================
# 9. RISK LEVEL (mutually exclusive, ordered by severity)
# ================================================================

# CRITICAL — immediate termination

risk_level := "critical" if {
	deny_credential_leak
}

risk_level := "critical" if {
	deny_pii
}

risk_level := "critical" if {
	deny_financial_data
}

risk_level := "critical" if {
	deny_sensitive_path
	input.risk_score >= 0.5
}

risk_level := "critical" if {
	deny_unsafe_coding
}

# HIGH — block and alert

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
		not deny_unsafe_coding
	not deny_sensitive_path
	not deny_network
}

risk_level := "high" if {
	not allow
	not deny_sensitive_path
	not deny_network
	not risky_write
	not deny_credential_leak
	not deny_pii
	not deny_financial_data
}

# MEDIUM — log and monitor

risk_level := "medium" if {
	input.action == "exec"
	not deny_sensitive_path
	not deny_network
	not risky_write
	allow
}

risk_level := "medium" if {
	input.action == "network_connect"
	not common.is_safe_destination(input.resource_ref)
	not common.is_known_llm_endpoint(input.resource_ref)
	not deny_network
}

# LOW — normal operation (non-exec only; exec is at least medium)

risk_level := "low" if {
	input.action != "exec"
	not deny_sensitive_path
	not deny_network
	not risky_write
	not deny_credential_leak
	not deny_pii
	not deny_financial_data
	not deny_unsafe_coding
	allow
}
