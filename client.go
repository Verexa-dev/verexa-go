package verexa

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// defaultTimeout has to stay above the largest budget the data plane spends
// on a check, or the client aborts first, fails open on every request, trips
// the breaker, and the guard silently stops being consulted.
const defaultTimeout = 2 * time.Second

// auditBudget is the audit profile's server-side budget (verdict-api plan.go).
const auditBudget = 8 * time.Second

const maxResponseBytes = 8 << 20

var userAgent = "verexa-go/" + Version + " (go/" + goVersion() + ")"

func goVersion() string {
	fields := strings.Fields(strings.TrimPrefix(runtime.Version(), "devel "))
	if len(fields) == 0 {
		return "unknown"
	}
	return strings.TrimPrefix(fields[0], "go")
}

type CheckOptions struct {
	TraceID      string
	SystemPrompt string
	Profile      string
	// Timeout overrides the client default for this call. A profile whose
	// budget is larger than the client timeout aborts before the service can
	// answer, which fails open and looks exactly like a clean allow.
	Timeout time.Duration
}

type Client struct {
	apiKey           string
	baseURL          string
	timeout          time.Duration
	failMode         FailMode
	httpClient       *http.Client
	logger           *slog.Logger
	breaker          *CircuitBreaker
	telemetry        *TelemetryBuffer
	auditWarned      atomic.Bool
	rejectedProfiles sync.Map
}

// New resolves cfg against the environment and returns a client. Called with
// a zero Config it reads VEREXA_API_KEY and VEREXA_BASE_URL.
func New(cfg Config) *Client {
	cfg = ResolveConfig(cfg)
	c := &Client{
		apiKey:     cfg.APIKey,
		baseURL:    strings.TrimSuffix(cfg.BaseURL, "/"),
		timeout:    cfg.Timeout,
		failMode:   cfg.FailMode,
		httpClient: cfg.HTTPClient,
		logger:     cfg.Logger,
		breaker:    NewCircuitBreaker(5, 30*time.Second),
		telemetry:  NewTelemetryBuffer(500),
	}
	if c.timeout <= 0 {
		c.timeout = defaultTimeout
	}
	if c.failMode == "" {
		c.failMode = FailOpen
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{}
	}
	return c
}

func (c *Client) Telemetry() *TelemetryBuffer { return c.telemetry }

func (c *Client) CheckInput(ctx context.Context, text string, opts CheckOptions) *CheckResponse {
	return c.Check(ctx, PhaseInput, text, opts)
}

func (c *Client) CheckOutput(ctx context.Context, text string, opts CheckOptions) *CheckResponse {
	return c.Check(ctx, PhaseOutput, text, opts)
}

// Check never returns an error. Every failure (no key, open breaker, timeout,
// transport error, bad status, bad body) yields the fallback verdict: allow
// with Degraded set, or block under FailClosed.
func (c *Client) Check(ctx context.Context, phase Phase, text string, opts CheckOptions) *CheckResponse {
	traceID := opts.TraceID
	if traceID == "" {
		traceID = NewTraceID()
	}

	// A missing key is a configuration problem, not a backend health problem,
	// so it skips the network and the breaker entirely.
	if c.apiKey == "" {
		c.telemetry.Push(TelemetryEvent{Type: EventError, TraceID: traceID, Phase: phase, Timestamp: time.Now()})
		return c.fallback(text)
	}

	if c.breaker.IsOpen() {
		c.telemetry.Push(TelemetryEvent{Type: EventCircuitOpen, TraceID: traceID, Phase: phase, Timestamp: time.Now()})
		return c.fallback(text)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = c.timeout
	}
	if opts.Profile == "audit" && timeout < auditBudget+time.Second && c.auditWarned.CompareAndSwap(false, true) {
		c.logger.Warn(fmt.Sprintf("Verexa: the audit profile runs the tier-3 judge synchronously with an "+
			"%dms server-side budget, but this check will abort after %dms. "+
			"The client gives up first, the verdict arrives to no one, and the check fails open. "+
			"Set CheckOptions.Timeout to 15s on audit checks (or Config.Timeout client-wide).",
			auditBudget.Milliseconds(), timeout.Milliseconds()))
	}

	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	parsed, status, err := c.post(ctx, CheckRequest{
		TraceID:      traceID,
		Phase:        phase,
		Text:         text,
		SystemPrompt: opts.SystemPrompt,
		Profile:      opts.Profile,
	})
	if err != nil {
		// A 4xx means the service answered and the credential or request is
		// wrong. Tripping the breaker would turn a bad key into the same silent
		// fail-open as an outage, so only 5xx and transport errors count.
		if status == 0 || status >= 500 {
			c.breaker.RecordFailure()
		}
		if status == http.StatusBadRequest && opts.Profile != "" {
			if _, seen := c.rejectedProfiles.LoadOrStore(opts.Profile, true); !seen {
				c.logger.Warn(fmt.Sprintf("Verexa: the service rejected profile %q (%v). "+
					"Every check that names it returns the degraded fallback until the profile exists. "+
					"Fix the name or create the profile in the dashboard.", opts.Profile, err))
			}
		}
		c.telemetry.Push(TelemetryEvent{Type: EventError, TraceID: traceID, Phase: phase, Status: status, Timestamp: time.Now()})
		return c.fallback(text)
	}

	c.breaker.RecordSuccess()
	c.telemetry.Push(TelemetryEvent{
		Type:      EventCheck,
		TraceID:   traceID,
		Phase:     phase,
		Action:    parsed.Action,
		LatencyMs: parsed.LatencyMs,
		Degraded:  parsed.Degraded,
		Timestamp: time.Now(),
	})
	return parsed
}

func (c *Client) post(ctx context.Context, body CheckRequest) (*CheckResponse, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/check", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", userAgent)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		if message := strings.TrimSpace(string(detail)); message != "" {
			return nil, res.StatusCode, fmt.Errorf("verdict-api returned %d: %s", res.StatusCode, message)
		}
		return nil, res.StatusCode, fmt.Errorf("verdict-api returned %d", res.StatusCode)
	}

	var parsed CheckResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseBytes)).Decode(&parsed); err != nil {
		return nil, 0, err
	}
	parsed.normalizeActions()
	return &parsed, res.StatusCode, nil
}

func (c *Client) fallback(text string) *CheckResponse {
	action := ActionAllow
	if c.failMode == FailClosed {
		action = ActionBlock
	}
	return &CheckResponse{
		Action:    action,
		Detectors: []DetectorOutcome{},
		Text:      text,
		Degraded:  true,
		PlanHash:  "unavailable",
		Stages:    []StageOutcome{},
	}
}

// NewTraceID returns a random UUIDv4.
func NewTraceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t_%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
