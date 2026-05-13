package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type AlertInfo struct {
	AlertID       string `json:"alert_id"`
	AgentID       string `json:"agent_id"`
	FamilyGroupID string `json:"family_group_id"`
	Severity      string `json:"severity"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Status        string `json:"status"`
}

// Subscriber 从 management-server 拉取开态告警。
type Subscriber struct {
	mgmtURL string
	http    *http.Client
	log     *slog.Logger
}

func NewSubscriber(mgmtURL string, log *slog.Logger) *Subscriber {
	return &Subscriber{
		mgmtURL: mgmtURL,
		http:    &http.Client{Timeout: 10 * time.Second},
		log:     log,
	}
}

// PollOpenAlerts 拉取所有 open 状态告警。
func (s *Subscriber) PollOpenAlerts(ctx context.Context) ([]AlertInfo, error) {
	url := fmt.Sprintf("%s/api/v1/alerts?status=open&limit=50", s.mgmtURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll alerts: %w", err)
	}
	defer resp.Body.Close()

	var body struct {
		Alerts []AlertInfo `json:"alerts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode alerts: %w", err)
	}
	return body.Alerts, nil
}
