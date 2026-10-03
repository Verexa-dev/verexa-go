package verexa

import (
	"sync"
	"time"
)

type TelemetryEventType string

const (
	EventCheck       TelemetryEventType = "check"
	EventError       TelemetryEventType = "error"
	EventCircuitOpen TelemetryEventType = "circuit_open"
)

type TelemetryEvent struct {
	Type      TelemetryEventType
	TraceID   string
	Phase     Phase
	Action    Action
	LatencyMs float64
	Degraded  bool
	// Status is the HTTP status when the failure was a response rather than a
	// transport error.
	Status    int
	Timestamp time.Time
}

type TelemetryBuffer struct {
	mu      sync.Mutex
	events  []TelemetryEvent
	maxSize int
}

func NewTelemetryBuffer(maxSize int) *TelemetryBuffer {
	return &TelemetryBuffer{maxSize: maxSize}
}

// Push drops the oldest event rather than the newest when full, so the
// errors worth seeing are never the ones discarded.
func (t *TelemetryBuffer) Push(event TelemetryEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) >= t.maxSize {
		t.events = append(t.events[:0:0], t.events[len(t.events)-t.maxSize+1:]...)
	}
	t.events = append(t.events, event)
}

func (t *TelemetryBuffer) Flush() []TelemetryEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	drained := t.events
	t.events = nil
	return drained
}

func (t *TelemetryBuffer) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.events)
}
