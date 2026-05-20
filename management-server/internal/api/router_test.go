package api

import (
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"agentshield.dev/agentshield/management-server/internal/store"
)

func TestHealthzRouting(t *testing.T) {
	r := NewRouter(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		store.NewMemory(0), nil, nil, nil, nil)

	tests := []struct {
		path string
	}{
		{"/healthz"},
		{"/api/v1/healthz"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Errorf("%s: expected 200, got %d", tc.path, w.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s: invalid json: %v", tc.path, err)
			}
			if body["status"] != "ok" {
				t.Errorf("%s: expected status ok, got %s", tc.path, body["status"])
			}
		})
	}
}
