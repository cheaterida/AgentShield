package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OPAMode controls how the OPA backend evaluates policies.
type OPAMode string

const (
	OPAModeRemote  OPAMode = "remote"
	OPAModeBuiltin OPAMode = "builtin"
)

// OPAConfig holds configuration for the OPA client.
type OPAConfig struct {
	BaseURL              string
	Mode                 OPAMode
	RegoContent          string
	AllowBuiltinFallback bool
}

// OPAClient 封装对 OPA 服务器的 HTTP 调用，支持远程和内置回退模式。
type OPAClient struct {
	baseURL              string
	mode                 OPAMode
	regoContent          string
	allowBuiltinFallback bool
	http                 *http.Client
}

// NewOPAClient creates an OPA client with remote mode (backward compatible).
func NewOPAClient(baseURL string) *OPAClient {
	return NewOPAClientWithConfig(OPAConfig{
		BaseURL: baseURL,
		Mode:    OPAModeRemote,
	})
}

// NewOPAClientWithConfig creates an OPA client with full configuration.
func NewOPAClientWithConfig(cfg OPAConfig) *OPAClient {
	if cfg.Mode == "" {
		cfg.Mode = OPAModeRemote
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:8181"
	}
	if err := validateOPAURL(cfg.BaseURL); err != nil {
		log.Printf("[WARN] OPA URL validation failed: %v", err)
	}
	return &OPAClient{
		baseURL:              strings.TrimRight(cfg.BaseURL, "/"),
		mode:                 cfg.Mode,
		regoContent:          cfg.RegoContent,
		allowBuiltinFallback: cfg.AllowBuiltinFallback,
		http:                 &http.Client{Timeout: 10 * time.Second},
	}
}

// Evaluate 查询 OPA 策略评估。
func (c *OPAClient) Evaluate(ctx context.Context, path string, input any) (map[string]any, error) {
	switch c.mode {
	case OPAModeBuiltin:
		return c.evaluateBuiltin(ctx, path, input)
	default:
		result, err := c.evaluateRemote(ctx, path, input)
		if err != nil && c.allowBuiltinFallback && c.regoContent != "" {
			return c.evaluateBuiltin(ctx, path, input)
		}
		return result, err
	}
}

func (c *OPAClient) evaluateRemote(ctx context.Context, path string, input any) (map[string]any, error) {
	body := map[string]any{"input": input}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	url := fmt.Sprintf("%s/v1/data/%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opa request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("opa response: %w", err)
	}
	return result.Result, nil
}

// evaluateBuiltin provides a minimal in-process Rego evaluator for
// air-gapped / fallback scenarios. It handles default declarations,
// single-line and multi-line rule bodies, ==, !=, and not.
func (c *OPAClient) evaluateBuiltin(_ context.Context, path string, input any) (map[string]any, error) {
	if c.regoContent == "" {
		return nil, fmt.Errorf("opa builtin mode requires rego content")
	}

	// Extract the target rule name from the query path, e.g.
	// "agentshield/audit" → target "audit" for rules like "default allow = false".
	parts := strings.Split(path, "/")
	targetRule := parts[len(parts)-1]

	inputMap, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("builtin evaluator requires map[string]any input")
	}

	defaults := make(map[string]bool)
	lines := strings.Split(c.regoContent, "\n")

	// First pass: collect default declarations.
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "default ") {
			p := strings.SplitN(strings.TrimPrefix(stripped, "default "), "=", 2)
			if len(p) == 2 {
				key := strings.TrimSpace(p[0])
				value := strings.EqualFold(strings.TrimSpace(p[1]), "true")
				defaults[key] = value
			}
		}
	}

	result := defaults[targetRule]

	// Second pass: evaluate rule bodies.
	inRule := false
	ruleConds := make([]string, 0)

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(stripped, targetRule+" if {"):
			if strings.HasSuffix(stripped, "}") {
				body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(stripped, targetRule+" if {"), "}"))
				if evalRegoCondition(body, inputMap) {
					result = true
				}
			} else {
				inRule = true
				ruleConds = ruleConds[:0]
			}
		case strings.HasPrefix(stripped, targetRule+" {") && !strings.Contains(stripped, "if"):
			if strings.HasSuffix(stripped, "}") {
				body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(stripped, targetRule+" {"), "}"))
				if evalRegoCondition(body, inputMap) {
					result = true
				}
			} else {
				inRule = true
				ruleConds = ruleConds[:0]
			}
		case inRule && stripped == "}":
			matched := true
			for _, cond := range ruleConds {
				if !evalRegoCondition(cond, inputMap) {
					matched = false
					break
				}
			}
			if matched && len(ruleConds) > 0 {
				result = true
			}
			inRule = false
			ruleConds = ruleConds[:0]
		case inRule && stripped != "" && !strings.HasPrefix(stripped, "#"):
			ruleConds = append(ruleConds, stripped)
		}
	}

	return map[string]any{
		"allow":              result,
		"deny_sensitive_path": defaults["deny_sensitive_path"],
		"deny_network":       defaults["deny_network"],
		"risky_write":        defaults["risky_write"],
		"risk_level":         "",
	}, nil
}

func evalRegoCondition(condition string, context map[string]any) bool {
	condition = strings.TrimSpace(strings.TrimSuffix(condition, ";"))
	if condition == "" {
		return false
	}
	if strings.HasPrefix(condition, "not ") {
		return !evalRegoCondition(strings.TrimSpace(strings.TrimPrefix(condition, "not ")), context)
	}
	if strings.Contains(condition, "==") {
		parts := strings.SplitN(condition, "==", 2)
		left := strings.TrimSpace(parts[0])
		right := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		leftValue := resolveRegoPath(left, context)
		switch right {
		case "true":
			b, ok := leftValue.(bool)
			return ok && b
		case "false":
			b, ok := leftValue.(bool)
			return ok && !b
		default:
			return fmt.Sprint(leftValue) == right
		}
	}
	if strings.Contains(condition, "!=") {
		parts := strings.SplitN(condition, "!=", 2)
		left := strings.TrimSpace(parts[0])
		right := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		leftValue := resolveRegoPath(left, context)
		return fmt.Sprint(leftValue) != right
	}
	return truthy(resolveRegoPath(condition, context))
}

func resolveRegoPath(path string, data map[string]any) any {
	current := any(data)
	for _, part := range strings.Split(path, ".") {
		if part == "input" {
			continue
		}
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = asMap[part]
	}
	return current
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != "" && !strings.EqualFold(v, "false")
	case nil:
		return false
	default:
		return true
	}
}

// validateOPAURL rejects URLs with non-HTTP schemes or known SSRF targets.
func validateOPAURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid OPA URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported OPA URL scheme %q: only http and https are allowed", parsed.Scheme)
	}
	blockedHosts := map[string]bool{
		"169.254.169.254":         true,
		"metadata.google.internal": true,
	}
	host := strings.ToLower(parsed.Hostname())
	if blockedHosts[host] {
		return fmt.Errorf("OPA URL host %q is blocked to prevent SSRF", host)
	}
	return nil
}
