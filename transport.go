package verexa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Transport returns an http.RoundTripper that checks every turn sent through
// it to an OpenAI-compatible API: POST .../chat/completions and
// POST .../responses. Everything else passes through untouched. A nil base
// uses http.DefaultTransport.
//
// Both halves of a turn share one trace id. A blocked input returns a
// *BlockedError before the provider is called; a redacted input reaches the
// provider with the sensitive span removed.
//
// Streaming is checked after the fact. Chunks reach the caller as they arrive,
// and a blocking output verdict surfaces as a *BlockedError from the body's
// Read once the stream ends: a retraction signal, not a gate.
func (c *Client) Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &guardTransport{client: c, base: base}
}

type guardTransport struct {
	client *Client
	base   http.RoundTripper
	memo   blockMemo
}

type endpoint interface {
	inputText(body jsonObject) (text, systemPrompt string)
	redactInput(body jsonObject, text string) bool
	outputs(body jsonObject) []outputSlot
	streamEvent(data []byte) streamEvent
}

type outputSlot struct {
	text string
	set  func(string)
}

func (t *guardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ep := endpointFor(req)
	if ep == nil {
		return t.base.RoundTrip(req)
	}

	raw, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, err
	}

	body, err := parseObject(raw)
	if err != nil {
		return t.base.RoundTrip(withBody(req, raw))
	}

	key := memoKey(req, raw)
	if isRetry(req) {
		if blocked := t.memo.get(key); blocked != nil {
			return nil, blocked
		}
	}
	res, err := t.roundTrip(req, ep, body, raw)
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		t.memo.put(key, blocked)
	}
	return res, err
}

func (t *guardTransport) roundTrip(req *http.Request, ep endpoint, body jsonObject, raw []byte) (*http.Response, error) {
	ctx := req.Context()
	traceID := NewTraceID()
	inputText, systemPrompt := ep.inputText(body)
	opts := CheckOptions{TraceID: traceID, SystemPrompt: systemPrompt}

	if inputText != "" {
		verdict := t.client.Check(ctx, PhaseInput, inputText, opts)
		if verdict.Action == ActionBlock {
			return nil, &BlockedError{Phase: PhaseInput, Response: verdict}
		}
		if verdict.Action == ActionRedact && verdict.Text != inputText {
			// Structured input cannot be rebuilt from the flattened text the
			// check ran on, so the redaction is surfaced instead of dropped.
			if !ep.redactInput(body, verdict.Text) {
				return nil, &BlockedError{Phase: PhaseInput, Response: verdict}
			}
			var err error
			if raw, err = json.Marshal(body); err != nil {
				return nil, err
			}
		}
	}

	res, err := t.base.RoundTrip(withBody(req, raw))
	if err != nil || res.StatusCode < 200 || res.StatusCode > 299 {
		return res, err
	}

	finish := func(texts []string) *BlockedError {
		verdicts := t.checkAll(ctx, texts, opts)
		for _, v := range verdicts {
			if v != nil && v.Action == ActionBlock {
				return &BlockedError{Phase: PhaseOutput, Response: v}
			}
		}
		return nil
	}

	if isEventStream(res.Header) {
		res.Body = &guardedStream{body: res.Body, event: ep.streamEvent, finish: finish}
		return res, nil
	}

	return t.checkOutput(ctx, res, ep, opts)
}

func (t *guardTransport) checkOutput(ctx context.Context, res *http.Response, ep endpoint, opts CheckOptions) (*http.Response, error) {
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}

	body, err := parseObject(raw)
	if err != nil {
		setResponseBody(res, raw)
		return res, nil
	}

	// Every choice is a candidate reply the caller may show, so every one is
	// checked, not just the first.
	slots := ep.outputs(body)
	texts := make([]string, len(slots))
	for i, s := range slots {
		texts[i] = s.text
	}
	verdicts := t.checkAll(ctx, texts, opts)

	for _, v := range verdicts {
		if v != nil && v.Action == ActionBlock {
			return nil, &BlockedError{Phase: PhaseOutput, Response: v}
		}
	}

	changed := false
	for i, v := range verdicts {
		if v != nil && v.Action == ActionRedact && v.Text != slots[i].text {
			slots[i].set(v.Text)
			changed = true
		}
	}
	if changed {
		if raw, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	setResponseBody(res, raw)
	return res, nil
}

func (t *guardTransport) checkAll(ctx context.Context, texts []string, opts CheckOptions) []*CheckResponse {
	verdicts := make([]*CheckResponse, len(texts))
	var wg sync.WaitGroup
	for i, text := range texts {
		if text == "" {
			continue
		}
		wg.Add(1)
		go func(i int, text string) {
			defer wg.Done()
			verdicts[i] = t.client.Check(ctx, PhaseOutput, text, opts)
		}(i, text)
	}
	wg.Wait()
	return verdicts
}

func endpointFor(req *http.Request) endpoint {
	if req.Method != http.MethodPost || req.Body == nil || req.URL == nil {
		return nil
	}
	path := strings.TrimSuffix(req.URL.Path, "/")
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return chatEndpoint{}
	case strings.HasSuffix(path, "/responses"):
		return responsesEndpoint{}
	}
	return nil
}

func withBody(req *http.Request, raw []byte) *http.Request {
	out := req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(raw))
	out.ContentLength = int64(len(raw))
	out.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	out.Header.Del("Content-Length")
	return out
}

func setResponseBody(res *http.Response, raw []byte) {
	res.Body = io.NopCloser(bytes.NewReader(raw))
	res.ContentLength = int64(len(raw))
	res.Header.Set("Content-Length", strconv.Itoa(len(raw)))
}

func isEventStream(h http.Header) bool {
	mediaType, _, _ := mime.ParseMediaType(h.Get("Content-Type"))
	return mediaType == "text/event-stream"
}
