package risk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"
)

// MLScorer 调用 ml-pipeline 服务进行基于机器学习的事件评分。
type MLScorer interface {
	EmbedEvents(ctx context.Context, events []models.AuditEvent) ([][]float64, error)
	ScoreEvents(ctx context.Context, events []models.AuditEvent) ([]float64, error)
	ScoreCFG(ctx context.Context, agentID string, graphJSON string) (float64, error)
	IsAvailable() bool
	TrainingEventCount() int
}

// HTTPMLScorer 通过 HTTP 调用 ml-pipeline。
type HTTPMLScorer struct {
	baseURL    string
	client     *http.Client
	log        *slog.Logger
	mu         sync.RWMutex
	available  bool
	trainCount int
}

func NewHTTPMLScorer(baseURL string, log *slog.Logger) *HTTPMLScorer {
	s := &HTTPMLScorer{
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		log:       log,
		available: false,
	}
	// Probe on creation
	go s.probe()
	return s
}

func (s *HTTPMLScorer) probe() {
	resp, err := s.client.Get(s.baseURL + "/healthz")
	if err != nil {
		s.log.Warn("ml-pipeline unavailable", "err", err)
		s.mu.Lock()
		s.available = false
		s.mu.Unlock()
		return
	}
	defer resp.Body.Close()

	s.mu.Lock()
	s.available = resp.StatusCode == http.StatusOK

	// Parse training event count from health response
	if s.available {
		var h struct {
			ModelLoaded string `json:"model_loaded"`
		}
		if body, err := io.ReadAll(resp.Body); err == nil {
			json.Unmarshal(body, &h)
			if h.ModelLoaded == "True" {
				s.trainCount = 2000 // conservative default
			}
		}
	}
	s.mu.Unlock()

	if s.available {
		s.log.Info("ml-pipeline available", "url", s.baseURL)
	}
}

func (s *HTTPMLScorer) IsAvailable() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.available
}

func (s *HTTPMLScorer) TrainingEventCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.trainCount
}

// ── event embedding request/response ──

type embedReq struct {
	Events []mlEvent `json:"events"`
}

type embedResp struct {
	Embeddings [][]float64 `json:"embeddings"`
}

type scoreReq struct {
	Events []mlEvent `json:"events"`
}

type scoreResp struct {
	Scores []mlEventScore `json:"scores"`
}

type mlEvent struct {
	EventID    string            `json:"event_id"`
	AgentID    string            `json:"agent_id"`
	Action     string            `json:"action"`
	ResourceRef string           `json:"resource_ref"`
	OccurredAt string            `json:"occurred_at"`
	Attributes map[string]string `json:"attributes"`
}

type mlEventScore struct {
	EventID      string  `json:"event_id"`
	AnomalyScore float64 `json:"anomaly_score"`
}

// ── public methods ──

func (s *HTTPMLScorer) EmbedEvents(ctx context.Context, events []models.AuditEvent) ([][]float64, error) {
	if !s.IsAvailable() {
		return nil, fmt.Errorf("ml-pipeline not available")
	}

	req := embedReq{Events: s.toMLEvents(events)}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/v1/anomaly/embed", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		s.markUnavailable(err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ml-pipeline embed returned %d", resp.StatusCode)
	}

	var er embedResp
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, err
	}
	return er.Embeddings, nil
}

func (s *HTTPMLScorer) ScoreEvents(ctx context.Context, events []models.AuditEvent) ([]float64, error) {
	if !s.IsAvailable() {
		return nil, fmt.Errorf("ml-pipeline not available")
	}

	req := scoreReq{Events: s.toMLEvents(events)}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/v1/anomaly/score", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		s.markUnavailable(err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ml-pipeline score returned %d", resp.StatusCode)
	}

	var sr scoreResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, err
	}

	scores := make([]float64, len(sr.Scores))
	for i, sc := range sr.Scores {
		scores[i] = sc.AnomalyScore
	}
	return scores, nil
}

func (s *HTTPMLScorer) ScoreCFG(ctx context.Context, agentID string, graphJSON string) (float64, error) {
	if !s.IsAvailable() {
		return 0, fmt.Errorf("ml-pipeline not available")
	}

	req := struct {
		GraphJSON string `json:"graph_json"`
		AgentID   string `json:"agent_id"`
	}{GraphJSON: graphJSON, AgentID: agentID}

	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/v1/cfg/analyze", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		AnomalyScore float64 `json:"anomaly_score"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.AnomalyScore, nil
}

// ── helpers ──

func (s *HTTPMLScorer) toMLEvents(events []models.AuditEvent) []mlEvent {
	out := make([]mlEvent, len(events))
	for i, ev := range events {
		out[i] = mlEvent{
			EventID:     ev.EventID,
			AgentID:     ev.AgentID,
			Action:      ev.Action,
			ResourceRef: ev.ResourceRef,
			OccurredAt:  ev.OccurredAt.Format(time.RFC3339),
			Attributes:  ev.Attributes,
		}
	}
	return out
}

func (s *HTTPMLScorer) markUnavailable(err error) {
	s.mu.Lock()
	s.available = false
	s.mu.Unlock()
	s.log.Warn("ml-pipeline marked unavailable", "err", err)
}
