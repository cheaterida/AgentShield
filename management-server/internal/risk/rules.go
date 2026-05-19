package risk

import (
	"strings"

	"agentshield.dev/agentshield/management-server/internal/models"
)

type Rule interface {
	Evaluate(event models.AuditEvent) (float64, string) // score 0-1, reason
}

// SensitivePathRule 检测对敏感路径的访问。
type SensitivePathRule struct{}

var sensitivePrefixes = []string{
	"/etc/passwd", "/etc/shadow", "/root/", "/proc/", "/sys/",
	"/var/run/docker.sock",
}

func (r *SensitivePathRule) Evaluate(ev models.AuditEvent) (float64, string) {
	for _, prefix := range sensitivePrefixes {
		if strings.HasPrefix(ev.ResourceRef, prefix) {
			return 0.5, "sensitive path: " + ev.ResourceRef
		}
	}
	return 0, ""
}

// WriteActionRule 检测写/删/执行操作。
type WriteActionRule struct{}

var riskyActions = []string{"write", "delete", "exec", "chmod", "chown", "mount"}

func (r *WriteActionRule) Evaluate(ev models.AuditEvent) (float64, string) {
	action := strings.ToLower(ev.Action)
	for _, ra := range riskyActions {
		if strings.Contains(action, ra) {
			return 0.2, "risky action: " + ev.Action
		}
	}
	return 0, ""
}

// NetworkAccessRule 检测异常网络访问属性。
type NetworkAccessRule struct{}

func (r *NetworkAccessRule) Evaluate(ev models.AuditEvent) (float64, string) {
	if ev.Attributes == nil {
		return 0, ""
	}
	if dst, ok := ev.Attributes["network_dst"]; ok {
		if strings.HasPrefix(dst, "0.0.0.0") || strings.Contains(dst, ":") {
			return 0.3, "network access: " + dst
		}
	}
	if _, ok := ev.Attributes["socket_create"]; ok {
		return 0.2, "socket creation"
	}
	return 0, ""
}
