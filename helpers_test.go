package verexa

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func allowVerdict(text string) CheckResponse {
	return CheckResponse{
		Action:    ActionAllow,
		Detectors: []DetectorOutcome{},
		Text:      text,
		LatencyMs: 0.1,
		PlanHash:  "p_test",
		Stages:    []StageOutcome{},
	}
}

func blockVerdict(text, detector string) CheckResponse {
	v := allowVerdict(text)
	v.Action = ActionBlock
	v.Score = 1
	v.Detectors = []DetectorOutcome{{DetectorID: detector, Score: 1, Action: ActionBlock}}
	return v
}

func redactVerdict(redacted string) CheckResponse {
	v := allowVerdict(redacted)
	v.Action = ActionRedact
	v.Score = 1
	v.Detectors = []DetectorOutcome{{DetectorID: "text.pii", Score: 1, Action: ActionRedact}}
	return v
}

type recorded struct {
	body   CheckRequest
	header http.Header
	path   string
}

// verdictServer is a real HTTP server standing in for verdict-api. It records
// every request and answers with whatever respond returns.
type verdictServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recorded
	respond  func(CheckRequest) (int, any)
	delay    time.Duration
}

func newVerdictServer(t *testing.T) *verdictServer {
	t.Helper()
	s := &verdictServer{respond: func(r CheckRequest) (int, any) { return 200, allowVerdict(r.Text) }}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body CheckRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.requests = append(s.requests, recorded{body: body, header: r.Header.Clone(), path: r.URL.Path})
		respond, delay := s.respond, s.delay
		s.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		status, payload := respond(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if raw, ok := payload.(string); ok {
			_, _ = w.Write([]byte(raw))
			return
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *verdictServer) setRespond(fn func(CheckRequest) (int, any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respond = fn
}

func (s *verdictServer) calls() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.requests...)
}

func newTestClient(baseURL string, mutate ...func(*Config)) *Client {
	cfg := Config{APIKey: "k", BaseURL: baseURL, Logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	for _, m := range mutate {
		m(&cfg)
	}
	return New(cfg)
}

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func deadURL(t *testing.T) string {
	t.Helper()
	s := httptest.NewServer(http.NotFoundHandler())
	url := s.URL
	s.Close()
	return url
}

var bg = context.Background()
