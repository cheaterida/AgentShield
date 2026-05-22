package policy

import (
	"testing"
)

func TestValidateOPAURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid http", "http://localhost:8181", false},
		{"valid https", "https://opa.example.com", false},
		{"blocked aws metadata", "http://169.254.169.254", true},
		{"blocked gcp metadata", "http://metadata.google.internal", true},
		{"invalid scheme file", "file:///etc/passwd", true},
		{"valid custom port", "http://opa.internal:9191", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOPAURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOPAURL(%q) error = %v, wantErr = %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestOPAClientBuiltin(t *testing.T) {
	regoContent := `
package test

default allow := false

allow if {
	input.action == "read"
}

allow if {
	input.action == "exec"
	input.agent.role == "admin"
}
`
	client := NewOPAClientWithConfig(OPAConfig{
		BaseURL:     "http://localhost:8181",
		Mode:        OPAModeBuiltin,
		RegoContent: regoContent,
	})

	tests := []struct {
		name    string
		input   map[string]any
		wantAllow bool
	}{
		{
			name: "read allowed",
			input: map[string]any{
				"action": "read",
			},
			wantAllow: true,
		},
		{
			name: "admin exec allowed",
			input: map[string]any{
				"action": "exec",
				"agent":  map[string]any{"role": "admin"},
			},
			wantAllow: true,
		},
		{
			name: "non-admin exec denied",
			input: map[string]any{
				"action": "exec",
				"agent":  map[string]any{"role": "analyst"},
			},
			wantAllow: false,
		},
		{
			name: "unknown action denied",
			input: map[string]any{
				"action": "delete",
			},
			wantAllow: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.Evaluate(t.Context(), "test/allow", tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			allowed, _ := result["allow"].(bool)
			if allowed != tt.wantAllow {
				t.Errorf("allow = %v, want %v", allowed, tt.wantAllow)
			}
		})
	}
}

func TestOPAClientBuiltinRequiresContent(t *testing.T) {
	client := NewOPAClientWithConfig(OPAConfig{
		BaseURL: "http://localhost:8181",
		Mode:    OPAModeBuiltin,
	})

	_, err := client.Evaluate(t.Context(), "test/allow", map[string]any{"action": "read"})
	if err == nil {
		t.Error("expected error when rego content is empty")
	}
}

func TestOPAClientFailClosed(t *testing.T) {
	// Remote mode with unreachable OPA should return error, not panic.
	client := NewOPAClientWithConfig(OPAConfig{
		BaseURL: "http://127.0.0.1:19999",
		Mode:    OPAModeRemote,
	})

	_, err := client.Evaluate(t.Context(), "test/allow", map[string]any{})
	if err == nil {
		t.Error("expected error from unreachable OPA")
	}
}

func TestNewOPAClientBackwardCompatible(t *testing.T) {
	client := NewOPAClient("http://localhost:8181")
	if client.mode != OPAModeRemote {
		t.Errorf("default mode = %v, want remote", client.mode)
	}
}

func TestTruthy(t *testing.T) {
	tests := []struct {
		value any
		want  bool
	}{
		{true, true},
		{false, false},
		{"hello", true},
		{"", false},
		{"false", false},
		{nil, false},
		{42, true},
	}

	for _, tt := range tests {
		got := truthy(tt.value)
		if got != tt.want {
			t.Errorf("truthy(%v) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

func TestResolveRegoPath(t *testing.T) {
	data := map[string]any{
		"action": "read",
		"agent": map[string]any{
			"role": "admin",
		},
	}

	if got := resolveRegoPath("input.action", data); got != "read" {
		t.Errorf("input.action = %v, want read", got)
	}
	if got := resolveRegoPath("input.agent.role", data); got != "admin" {
		t.Errorf("input.agent.role = %v, want admin", got)
	}
	if got := resolveRegoPath("input.nonexistent", data); got != nil {
		t.Errorf("input.nonexistent = %v, want nil", got)
	}
}
