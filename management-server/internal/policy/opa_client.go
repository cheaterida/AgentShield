package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OPAClient 封装对 OPA 服务器的 HTTP 调用。
type OPAClient struct {
	baseURL string
	http    *http.Client
}

func NewOPAClient(baseURL string) *OPAClient {
	return &OPAClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Evaluate 查询 OPA 策略评估。
func (c *OPAClient) Evaluate(ctx context.Context, path string, input any) (map[string]any, error) {
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
