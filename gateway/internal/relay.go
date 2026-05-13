package internal

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Relay 将 Webhook 请求中继到 management-server。
type Relay struct {
	mgmtURL string
	http    *http.Client
	log     *slog.Logger
}

func NewRelay(mgmtURL string, log *slog.Logger) *Relay {
	return &Relay{
		mgmtURL: mgmtURL,
		http:    &http.Client{Timeout: 15 * time.Second},
		log:     log,
	}
}

// Forward 将 body 转发到 management-server 审计事件端点。
func (r *Relay) Forward(ctx context.Context, body io.Reader) error {
	payload, err := io.ReadAll(body)
	if err != nil {
		return err
	}

	url := r.mgmtURL + "/api/v1/audit/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.http.Do(req)
	if err != nil {
		r.log.Error("relay upstream", "err", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		r.log.Error("relay upstream error", "status", resp.StatusCode)
	}
	return nil
}
