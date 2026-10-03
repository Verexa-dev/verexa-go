package verexa

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCheckReturnsParsedVerdict(t *testing.T) {
	s := newVerdictServer(t)
	s.setRespond(func(r CheckRequest) (int, any) { return 200, blockVerdict(r.Text, "output.secret_leak") })
	c := newTestClient(s.URL + "/")

	got := c.CheckOutput(bg, "here is the key AKIA...", CheckOptions{})

	if got.Action != ActionBlock || got.Degraded || got.Detectors[0].DetectorID != "output.secret_leak" {
		t.Fatalf("unexpected verdict %+v", got)
	}
	calls := s.calls()
	if len(calls) != 1 || calls[0].path != "/v1/check" {
		t.Fatalf("expected one POST to /v1/check, got %+v", calls)
	}
	if h := calls[0].header.Get("Authorization"); h != "Bearer k" {
		t.Fatalf("authorization header = %q", h)
	}
}

func TestCheckForwardsRequestFields(t *testing.T) {
	s := newVerdictServer(t)
	c := newTestClient(s.URL)

	c.CheckInput(bg, "hello", CheckOptions{TraceID: "t1", Profile: "deterministic", SystemPrompt: "sys"})

	body := s.calls()[0].body
	if body.Phase != PhaseInput || body.Text != "hello" || body.TraceID != "t1" || body.Profile != "deterministic" || body.SystemPrompt != "sys" {
		t.Fatalf("unexpected request body %+v", body)
	}
}

func TestCheckGeneratesTraceID(t *testing.T) {
	s := newVerdictServer(t)
	c := newTestClient(s.URL)

	c.CheckInput(bg, "a", CheckOptions{})
	c.CheckInput(bg, "b", CheckOptions{})

	calls := s.calls()
	if calls[0].body.TraceID == "" || calls[0].body.TraceID == calls[1].body.TraceID {
		t.Fatalf("expected distinct generated trace ids, got %q and %q", calls[0].body.TraceID, calls[1].body.TraceID)
	}
}

func TestFailsOpenOnNetworkError(t *testing.T) {
	c := newTestClient(deadURL(t))

	got := c.CheckInput(bg, "hello", CheckOptions{})

	if got.Action != ActionAllow || !got.Degraded || got.Text != "hello" || got.PlanHash != "unavailable" {
		t.Fatalf("unexpected fallback %+v", got)
	}
	if got.Detectors == nil || got.Stages == nil {
		t.Fatal("fallback slices must be empty, not nil, so they encode as []")
	}
}

func TestFailsClosedWhenConfigured(t *testing.T) {
	c := newTestClient(deadURL(t), func(cfg *Config) { cfg.FailMode = FailClosed })

	if got := c.CheckInput(bg, "hello", CheckOptions{}); got.Action != ActionBlock || !got.Degraded {
		t.Fatalf("expected degraded block, got %+v", got)
	}
}

func TestServerErrorFallsBack(t *testing.T) {
	s := newVerdictServer(t)
	s.setRespond(func(CheckRequest) (int, any) { return 500, "boom" })
	c := newTestClient(s.URL)

	if got := c.CheckInput(bg, "hello", CheckOptions{}); got.Action != ActionAllow || !got.Degraded {
		t.Fatalf("expected degraded allow, got %+v", got)
	}
}

func TestMalformedBodyFallsBack(t *testing.T) {
	s := newVerdictServer(t)
	s.setRespond(func(CheckRequest) (int, any) { return 200, "{not json" })
	c := newTestClient(s.URL)

	if got := c.CheckInput(bg, "hello", CheckOptions{}); !got.Degraded {
		t.Fatalf("expected degraded fallback, got %+v", got)
	}
	if ev := c.Telemetry().Flush(); len(ev) != 1 || ev[0].Type != EventError {
		t.Fatalf("expected one error event, got %+v", ev)
	}
}

func TestRepeated401DoesNotOpenCircuit(t *testing.T) {
	s := newVerdictServer(t)
	s.setRespond(func(CheckRequest) (int, any) { return 401, "unauthorized" })
	c := newTestClient(s.URL)

	for i := 0; i < 8; i++ {
		if got := c.CheckInput(bg, "hello", CheckOptions{}); got.Action != ActionAllow || !got.Degraded {
			t.Fatalf("call %d: unexpected %+v", i, got)
		}
	}
	if n := len(s.calls()); n != 8 {
		t.Fatalf("expected 8 requests to reach the server, got %d", n)
	}
	for _, e := range c.Telemetry().Flush() {
		if e.Type != EventError || e.Status != 401 {
			t.Fatalf("expected error events with status 401, got %+v", e)
		}
	}
}

func TestRepeated5xxOpensCircuit(t *testing.T) {
	s := newVerdictServer(t)
	s.setRespond(func(CheckRequest) (int, any) { return 503, "down" })
	c := newTestClient(s.URL)

	for i := 0; i < 5; i++ {
		c.CheckInput(bg, "hello", CheckOptions{})
	}
	if n := len(s.calls()); n != 5 {
		t.Fatalf("expected 5 requests, got %d", n)
	}
	c.Telemetry().Flush()

	c.CheckInput(bg, "hello", CheckOptions{})
	if n := len(s.calls()); n != 5 {
		t.Fatalf("open circuit still sent a request: %d", n)
	}
	if ev := c.Telemetry().Flush(); len(ev) != 1 || ev[0].Type != EventCircuitOpen {
		t.Fatalf("expected a circuit_open event, got %+v", ev)
	}
}

func TestNetworkErrorsOpenCircuit(t *testing.T) {
	c := newTestClient(deadURL(t))
	for i := 0; i < 5; i++ {
		c.CheckInput(bg, "hello", CheckOptions{})
	}
	c.Telemetry().Flush()

	got := c.CheckInput(bg, "hello", CheckOptions{})
	if !got.Degraded {
		t.Fatalf("expected degraded, got %+v", got)
	}
	if ev := c.Telemetry().Flush(); len(ev) != 1 || ev[0].Type != EventCircuitOpen {
		t.Fatalf("expected circuit_open, got %+v", ev)
	}
}

func TestTelemetryRecordsSuccessAndFailure(t *testing.T) {
	s := newVerdictServer(t)
	calls := 0
	s.setRespond(func(r CheckRequest) (int, any) {
		calls++
		if calls == 1 {
			return 200, blockVerdict(r.Text, "output.secret_leak")
		}
		return 500, "boom"
	})
	c := newTestClient(s.URL)

	c.CheckOutput(bg, "a", CheckOptions{})
	c.CheckOutput(bg, "b", CheckOptions{})

	ev := c.Telemetry().Flush()
	if len(ev) != 2 || ev[0].Type != EventCheck || ev[1].Type != EventError {
		t.Fatalf("expected [check error], got %+v", ev)
	}
	if ev[0].Action != ActionBlock || ev[0].Phase != PhaseOutput || ev[1].Status != 500 {
		t.Fatalf("unexpected event fields %+v", ev)
	}
}

func TestPerCallTimeoutOverridesDefault(t *testing.T) {
	s := newVerdictServer(t)
	s.delay = 60 * time.Millisecond
	c := newTestClient(s.URL, func(cfg *Config) { cfg.Timeout = 10 * time.Millisecond })

	if got := c.CheckInput(bg, "hi", CheckOptions{}); !got.Degraded {
		t.Fatalf("expected the 10ms client timeout to fall back, got %+v", got)
	}
	if got := c.CheckInput(bg, "hi", CheckOptions{Timeout: 500 * time.Millisecond}); got.Degraded || got.PlanHash != "p_test" {
		t.Fatalf("expected the per-call timeout to get a real verdict, got %+v", got)
	}
}

func TestCancelledContextFallsBack(t *testing.T) {
	s := newVerdictServer(t)
	c := newTestClient(s.URL)
	ctx, cancel := context.WithCancel(bg)
	cancel()

	if got := c.CheckInput(ctx, "hi", CheckOptions{}); !got.Degraded || got.Action != ActionAllow {
		t.Fatalf("expected degraded allow, got %+v", got)
	}
}

func TestAuditWarnsOnce(t *testing.T) {
	s := newVerdictServer(t)
	logger, buf := captureLogger()
	c := newTestClient(s.URL, func(cfg *Config) { cfg.Logger = logger })

	c.CheckInput(bg, "hi", CheckOptions{Profile: "audit"})
	first := strings.Count(buf.String(), "audit profile")
	if first != 1 {
		t.Fatalf("expected one audit warning, got %d: %s", first, buf.String())
	}
	c.CheckInput(bg, "hi", CheckOptions{Profile: "audit"})
	c.CheckInput(bg, "hi", CheckOptions{Profile: "deterministic"})
	if n := strings.Count(buf.String(), "audit profile"); n != 1 {
		t.Fatalf("expected the warning only once, got %d", n)
	}
}

func TestRejectedProfileWarnsOncePerName(t *testing.T) {
	s := newVerdictServer(t)
	s.setRespond(func(CheckRequest) (int, any) { return 400, "unknown profile \"hardend\"" })
	logger, buf := captureLogger()
	c := newTestClient(s.URL, func(cfg *Config) { cfg.Logger = logger })

	got := c.CheckInput(bg, "hi", CheckOptions{Profile: "hardend"})
	c.CheckInput(bg, "hi", CheckOptions{Profile: "hardend"})

	if !got.Degraded {
		t.Fatalf("expected the degraded fallback, got %+v", got)
	}
	if n := strings.Count(buf.String(), "rejected profile"); n != 1 || !strings.Contains(buf.String(), "hardend") {
		t.Fatalf("expected one warning naming the profile, got %d: %s", n, buf.String())
	}
	c.CheckInput(bg, "hi", CheckOptions{Profile: "other"})
	if n := strings.Count(buf.String(), "rejected profile"); n != 2 {
		t.Fatalf("expected a second warning for a second name, got %d", n)
	}
}

func TestAuditDoesNotWarnWhenTimeoutRaised(t *testing.T) {
	s := newVerdictServer(t)
	logger, buf := captureLogger()
	c := newTestClient(s.URL, func(cfg *Config) { cfg.Logger = logger })

	c.CheckInput(bg, "hi", CheckOptions{Profile: "audit", Timeout: 15 * time.Second})
	if buf.Len() != 0 {
		t.Fatalf("expected no warning, got %s", buf.String())
	}
}

func TestEmptyKeyFailsOpenWithoutRequest(t *testing.T) {
	t.Setenv("VEREXA_API_KEY", "")
	t.Setenv("GUARD_API_KEY", "")
	s := newVerdictServer(t)
	c := newTestClient(s.URL, func(cfg *Config) { cfg.APIKey = "" })

	got := c.CheckInput(bg, "hi", CheckOptions{})

	if len(s.calls()) != 0 {
		t.Fatal("expected no request without a key")
	}
	if got.Action != ActionAllow || !got.Degraded {
		t.Fatalf("expected degraded allow, got %+v", got)
	}
	if ev := c.Telemetry().Flush(); len(ev) != 1 || ev[0].Type != EventError {
		t.Fatalf("expected one error event, got %+v", ev)
	}
}

func TestEmptyKeyFailsClosedWhenConfigured(t *testing.T) {
	t.Setenv("VEREXA_API_KEY", "")
	t.Setenv("GUARD_API_KEY", "")
	c := newTestClient("http://unused.invalid", func(cfg *Config) { cfg.APIKey = ""; cfg.FailMode = FailClosed })

	if got := c.CheckInput(bg, "hi", CheckOptions{}); got.Action != ActionBlock {
		t.Fatalf("expected block, got %+v", got)
	}
}

func TestBlockedErrorMessage(t *testing.T) {
	v := blockVerdict("x", "output.secret_leak")
	if msg := (&BlockedError{Phase: PhaseOutput, Response: &v}).Error(); msg != "guard blocked output: output.secret_leak" {
		t.Fatalf("message = %q", msg)
	}
	empty := allowVerdict("x")
	if msg := (&BlockedError{Phase: PhaseInput, Response: &empty}).Error(); msg != "guard blocked input: policy action block" {
		t.Fatalf("message = %q", msg)
	}
}

func TestNewTraceIDIsUUIDv4(t *testing.T) {
	id := NewTraceID()
	if len(id) != 36 || id[14] != '4' || !strings.ContainsRune("89ab", rune(id[19])) {
		t.Fatalf("not a v4 uuid: %q", id)
	}
}
