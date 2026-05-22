package agentshield.audit

import rego.v1
import data.agentshield.common

# ── AgentShield Audit Policy ──
# Incorporates enterprise-grade rules from Microsoft Agent Governance Toolkit:
#   - Credential & API key protection
#   - PII / financial data / PHI detection
#   - MCP tool poisoning detection (invisible unicode, hidden instructions, exfiltration)
#   - Prompt injection detection (OWASP LLM01: direct override, delimiter, role play, context manipulation)
#   - SQL safety (destructive operations, mass operations, file I/O)
#   - Sandbox escape & dangerous shell detection
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
# 10. MCP SECURITY
#    (from AGT mcp-security.yaml)
# ================================================================

default deny_invisible_unicode := false

deny_invisible_unicode if {
	regex.match(`[\x{200b}\x{200c}\x{200d}\x{feff}]`, input.resource_ref)
}

deny_invisible_unicode if {
	regex.match(`[\x{202a}-\x{202e}]`, input.resource_ref)
}

deny_invisible_unicode if {
	regex.match(`[\x{2060}\x{180e}]`, input.resource_ref)
}

default deny_hidden_instructions := false

deny_hidden_instructions if {
	regex.match(`(?i)ignore\s+(all\s+)?previous`, input.resource_ref)
}

deny_hidden_instructions if {
	regex.match(`(?i)override\s+(the\s+)?(previous|above|original)`, input.resource_ref)
}

deny_hidden_instructions if {
	regex.match(`(?i)disregard\s+(all\s+)?(above|prior|previous)`, input.resource_ref)
}

default deny_privilege_escalation := false

deny_privilege_escalation if {
	regex.match(`(?i)\bsudo\b`, input.resource_ref)
}

deny_privilege_escalation if {
	regex.match(`(?i)\badmin\s+access\b`, input.resource_ref)
}

deny_privilege_escalation if {
	regex.match(`(?i)\broot\s+access\b`, input.resource_ref)
}

deny_privilege_escalation if {
	regex.match(`(?i)\belevate\s+privile`, input.resource_ref)
}

default deny_exfiltration := false

deny_exfiltration if {
	regex.match(`\bcurl\b`, input.resource_ref)
}

deny_exfiltration if {
	regex.match(`\bwget\b`, input.resource_ref)
}

deny_exfiltration if {
	regex.match(`\bfetch\s*\(`, input.resource_ref)
}

deny_exfiltration if {
	regex.match(`(?i)include\s+the\s+contents?\s+of`, input.resource_ref)
}

default deny_role_override := false

deny_role_override if {
	regex.match(`(?i)you\s+are\b`, input.resource_ref)
}

deny_role_override if {
	regex.match(`(?i)your\s+task\s+is\b`, input.resource_ref)
}

deny_role_override if {
	regex.match(`(?i)respond\s+with\b`, input.resource_ref)
}

deny_role_override if {
	regex.match(`(?i)you\s+must\b`, input.resource_ref)
}

# ================================================================
# 11. PROMPT INJECTION DETECTION (OWASP LLM01)
#    (from AGT prompt-injection-safety.yaml)
# ================================================================

default deny_prompt_injection := false

# Direct override
deny_prompt_injection if {
	regex.match(`(?i)ignore\s+(all\s+)?previous\s+instructions`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`(?i)you\s+are\s+now\b`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`(?i)forget\s+(everything|all|your)\b`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`(?i)do\s+not\s+follow\s+(your|the)\s+(previous\s+)?instructions`, input.resource_ref)
}

# Delimiter injection
deny_prompt_injection if {
	regex.match(`(?i)END\s+SYSTEM`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`<\|im_start\|>`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`\[INST\]`, input.resource_ref)
}

# Role play
deny_prompt_injection if {
	regex.match(`(?i)pretend\s+you\s+are`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`(?i)\bjailbreak\b`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`(?i)\bDAN\s+mode\b`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`(?i)bypass\s+(all\s+)?(safety|content)\s+(filters?|restrictions?)`, input.resource_ref)
}

# Context manipulation
deny_prompt_injection if {
	regex.match(`(?i)the\s+above\s+instructions\s+are\s+wrong`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`(?i)your\s+true\s+purpose\s+is`, input.resource_ref)
}

deny_prompt_injection if {
	regex.match(`(?i)the\s+real\s+system\s+prompt\s+is`, input.resource_ref)
}

# ================================================================
# 12. SQL SAFETY
#    (from AGT sql-safety.yaml)
# ================================================================

default deny_destructive_sql := false

deny_destructive_sql if {
	regex.match(`(?i)\bDROP\s+(TABLE|DATABASE|INDEX|VIEW|SCHEMA)`, input.resource_ref)
}

deny_destructive_sql if {
	regex.match(`(?i)\bTRUNCATE\b`, input.resource_ref)
}

deny_destructive_sql if {
	regex.match(`(?i)\bALTER\s+(TABLE|USER|ROLE)`, input.resource_ref)
}

deny_destructive_sql if {
	regex.match(`(?i)\bGRANT\b`, input.resource_ref)
}

deny_destructive_sql if {
	regex.match(`(?i)\bREVOKE\b`, input.resource_ref)
}

deny_destructive_sql if {
	regex.match(`(?i)\bEXEC(UTE)?\s+XP_CMDSHELL`, input.resource_ref)
}

default deny_mass_operation := false

deny_mass_operation if {
	regex.match(`(?i)\bDELETE\s+FROM`, input.resource_ref)
	not regex.match(`(?i)\bWHERE\b`, input.resource_ref)
}

deny_mass_operation if {
	regex.match(`(?i)\bUPDATE\s+\w+\s+SET`, input.resource_ref)
	not regex.match(`(?i)\bWHERE\b`, input.resource_ref)
}

default deny_file_sql := false

deny_file_sql if {
	regex.match(`(?i)\bLOAD_FILE\s*\(`, input.resource_ref)
}

deny_file_sql if {
	regex.match(`(?i)\bINTO\s+(OUT|DUMP)FILE`, input.resource_ref)
}

# ================================================================
# 13. SANDBOX + CLI SAFETY
#    (from AGT sandbox-safety.yaml + cli-security-rules.yaml)
# ================================================================

default deny_sandbox_escape := false

deny_sandbox_escape if {
	regex.match(`(?i)\bsubprocess\b`, input.resource_ref)
}

deny_sandbox_escape if {
	regex.match(`(?i)\bos\.system\b`, input.resource_ref)
}

deny_sandbox_escape if {
	regex.match(`(?i)\bctypes\b`, input.resource_ref)
}

deny_sandbox_escape if {
	regex.match(`__import__\s*\(\s*["\x27]os["\x27]`, input.resource_ref)
}

deny_sandbox_escape if {
	regex.match(`(?i)\beval\s*\(`, input.resource_ref)
}

default deny_dangerous_shell := false

deny_dangerous_shell if {
	regex.match(`\bnc\s+`, input.resource_ref)
}

deny_dangerous_shell if {
	regex.match(`(?i)\bnetcat\b`, input.resource_ref)
}

deny_dangerous_shell if {
	regex.match(`>\s*/dev/null`, input.resource_ref)
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

# CRITICAL — immediate block
risk_level := "critical" if {
	deny_credential_leak
} else := "critical" if {
	deny_pii
} else := "critical" if {
	deny_financial_data
} else := "critical" if {
	deny_sensitive_path
	input.risk_score >= 0.5
} else := "critical" if {
	deny_unsafe_coding
} else := "critical" if {
	deny_invisible_unicode
} else := "critical" if {
	deny_hidden_instructions
} else := "critical" if {
	deny_prompt_injection
} else := "critical" if {
	deny_destructive_sql
} else := "critical" if {
	deny_sandbox_escape
# HIGH — block and alert
} else := "high" if {
	deny_sensitive_path
	input.risk_score < 0.5
} else := "high" if {
	deny_network
} else := "high" if {
	risky_write
} else := "high" if {
	not allow
} else := "high" if {
	deny_privilege_escalation
} else := "high" if {
	deny_exfiltration
} else := "high" if {
	deny_role_override
} else := "high" if {
	deny_mass_operation
} else := "high" if {
	deny_file_sql
} else := "high" if {
	deny_dangerous_shell
# MEDIUM — log and monitor
} else := "medium" if {
	input.action == "exec"
	allow
} else := "medium" if {
	input.action == "network_connect"
	not common.is_safe_destination(input.resource_ref)
	not common.is_known_llm_endpoint(input.resource_ref)
# LOW — normal operation
} else := "low" if {
	allow
} else := ""
