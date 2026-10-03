package verexa

import (
	"runtime"
	"strings"
	"testing"
)

// Same payload as the JS and Python SDK tolerance tests (versioning policy 2.2).
const futureResponse = `{
  "action": "quarantine",
  "score": 0.9,
  "detectors": [{"detectorId": "future.detector_v9", "score": 0.9, "action": "quarantine", "explanation": "new field"}],
  "text": "hello",
  "latencyMs": 1.2,
  "degraded": false,
  "planHash": "ph1_abc",
  "cached": false,
  "stages": [{"tier": 4, "id": "tier4", "label": "Future", "status": "ran", "score": 0.9, "latencyMs": 1, "action": "quarantine", "extra": true}],
  "judge": {"action": "quarantine", "mode": "sync", "status": "completed", "rationale": {"nested": true}},
  "deprecatedIds": ["prompt.old_alias"],
  "futureTopLevel": {"nested": [1, 2, 3]}
}`

func TestCheckToleratesUnknownDataAndBlocksUnknownAction(t *testing.T) {
	s := newVerdictServer(t)
	s.setRespond(func(CheckRequest) (int, any) { return 200, futureResponse })
	c := newTestClient(s.URL)

	got := c.CheckInput(bg, "hello", CheckOptions{})

	if got.Action != ActionBlock || got.Degraded {
		t.Fatalf("want a non-degraded block, got action=%q degraded=%v", got.Action, got.Degraded)
	}
	if d := got.Detectors[0]; d.DetectorID != "future.detector_v9" || d.Action != ActionBlock {
		t.Fatalf("unexpected detector %+v", d)
	}
	if got.Stages[0].Action != ActionBlock || got.Judge == nil || got.Judge.Action != ActionBlock {
		t.Fatalf("unexpected stage or judge action: %+v %+v", got.Stages[0], got.Judge)
	}
	events := c.Telemetry().Flush()
	if len(events) != 1 || events[0].Type != EventCheck || events[0].Action != ActionBlock {
		t.Fatalf("unexpected telemetry %+v", events)
	}
}

func TestCheckLeavesKnownAndAbsentActionsUntouched(t *testing.T) {
	s := newVerdictServer(t)
	s.setRespond(func(r CheckRequest) (int, any) {
		v := redactVerdict(r.Text)
		v.Stages = []StageOutcome{{Tier: 1, ID: "tier1", Status: StageRan}}
		v.Judge = &JudgeOutcome{Mode: JudgeAsync, Status: "pending"}
		return 200, v
	})
	c := newTestClient(s.URL)

	got := c.CheckInput(bg, "hello", CheckOptions{})

	if got.Action != ActionRedact || got.Stages[0].Action != "" || got.Judge.Action != "" {
		t.Fatalf("known or absent actions changed: %+v %+v %+v", got.Action, got.Stages[0], got.Judge)
	}
}

func TestCheckSendsUserAgent(t *testing.T) {
	s := newVerdictServer(t)
	c := newTestClient(s.URL)

	c.CheckInput(bg, "hello", CheckOptions{})

	want := "verexa-go/" + Version + " (go/" + strings.TrimPrefix(runtime.Version(), "go") + ")"
	if got := s.calls()[0].header.Get("User-Agent"); got != want {
		t.Fatalf("User-Agent = %q, want %q", got, want)
	}
}
