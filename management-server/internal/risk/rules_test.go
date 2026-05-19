package risk

import (
	"testing"

	"agentshield.dev/agentshield/management-server/internal/models"
)

func TestSensitivePathRule_Hit(t *testing.T) {
	rule := &SensitivePathRule{}
	tests := []struct {
		resource string
		wantHit  bool
	}{
		{"/etc/passwd", true},
		{"/etc/passwd.bak", true},
		{"/etc/shadow", true},
		{"/root/.ssh/id_rsa", true},
		{"/proc/self/mem", true},
		{"/sys/kernel/debug", true},
		{"/var/run/docker.sock", true},
		{"/var/run/docker.sock/", true},
	}

	for _, tc := range tests {
		ev := models.AuditEvent{ResourceRef: tc.resource}
		score, reason := rule.Evaluate(ev)
		if tc.wantHit {
			if score != 0.5 {
				t.Errorf("%s: expected score 0.5, got %f", tc.resource, score)
			}
			if reason == "" {
				t.Errorf("%s: expected non-empty reason", tc.resource)
			}
		} else {
			if score != 0 {
				t.Errorf("%s: expected score 0, got %f", tc.resource, score)
			}
		}
	}
}

func TestSensitivePathRule_Miss(t *testing.T) {
	rule := &SensitivePathRule{}
	tests := []string{
		"/home/user/data.txt",
		"/tmp/cache.db",
		"/data/production/dataset.csv",
		"",
	}

	for _, resource := range tests {
		ev := models.AuditEvent{ResourceRef: resource}
		score, reason := rule.Evaluate(ev)
		if score != 0 {
			t.Errorf("%s: expected 0, got %f", resource, score)
		}
		if reason != "" {
			t.Errorf("%s: expected empty reason, got %s", resource, reason)
		}
	}
}

func TestWriteActionRule_Hit(t *testing.T) {
	rule := &WriteActionRule{}
	tests := []string{"write", "delete", "exec", "chmod", "chown", "mount", "execve", "WriteFile"}

	for _, action := range tests {
		ev := models.AuditEvent{Action: action}
		score, reason := rule.Evaluate(ev)
		if score != 0.2 {
			t.Errorf("%s: expected 0.2, got %f", action, score)
		}
		if reason == "" {
			t.Errorf("%s: expected non-empty reason", action)
		}
	}
}

func TestWriteActionRule_Miss(t *testing.T) {
	rule := &WriteActionRule{}
	tests := []string{"read", "open", "stat", "close", "", "list", "get"}

	for _, action := range tests {
		ev := models.AuditEvent{Action: action}
		score, _ := rule.Evaluate(ev)
		if score != 0 {
			t.Errorf("%s: expected 0, got %f", action, score)
		}
	}
}

func TestNetworkAccessRule_NetworkDst(t *testing.T) {
	rule := &NetworkAccessRule{}

	// 0.0.0.0 prefix → score 0.3
	ev := models.AuditEvent{
		Attributes: map[string]string{"network_dst": "0.0.0.0:8080"},
	}
	score, _ := rule.Evaluate(ev)
	if score != 0.3 {
		t.Errorf("expected 0.3 for 0.0.0.0 prefix, got %f", score)
	}

	// contains : → score 0.3
	ev2 := models.AuditEvent{
		Attributes: map[string]string{"network_dst": "10.0.0.1:443"},
	}
	score2, _ := rule.Evaluate(ev2)
	if score2 != 0.3 {
		t.Errorf("expected 0.3 for IP:port, got %f", score2)
	}
}

func TestNetworkAccessRule_SocketCreate(t *testing.T) {
	rule := &NetworkAccessRule{}

	ev := models.AuditEvent{
		Attributes: map[string]string{"socket_create": "AF_UNIX"},
	}
	score, _ := rule.Evaluate(ev)
	if score != 0.2 {
		t.Errorf("expected 0.2 for socket_create, got %f", score)
	}
}

func TestNetworkAccessRule_NilAttributes(t *testing.T) {
	rule := &NetworkAccessRule{}
	ev := models.AuditEvent{}
	score, _ := rule.Evaluate(ev)
	if score != 0 {
		t.Errorf("expected 0 for nil attributes, got %f", score)
	}
}

func TestNetworkAccessRule_EmptyAttributes(t *testing.T) {
	rule := &NetworkAccessRule{}
	ev := models.AuditEvent{Attributes: map[string]string{}}
	score, _ := rule.Evaluate(ev)
	if score != 0 {
		t.Errorf("expected 0 for empty attributes, got %f", score)
	}
}
