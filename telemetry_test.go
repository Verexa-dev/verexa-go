package verexa

import "testing"

func TestTelemetryFlushDrains(t *testing.T) {
	b := NewTelemetryBuffer(10)
	b.Push(TelemetryEvent{Type: EventCheck, TraceID: "a"})
	b.Push(TelemetryEvent{Type: EventError, TraceID: "b"})

	if b.Len() != 2 {
		t.Fatalf("len = %d", b.Len())
	}
	if got := b.Flush(); len(got) != 2 || got[0].TraceID != "a" {
		t.Fatalf("unexpected %+v", got)
	}
	if b.Len() != 0 || len(b.Flush()) != 0 {
		t.Fatal("flush did not drain")
	}
}

func TestTelemetryDropsOldestWhenFull(t *testing.T) {
	b := NewTelemetryBuffer(3)
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		b.Push(TelemetryEvent{TraceID: id})
	}
	got := b.Flush()
	if len(got) != 3 || got[0].TraceID != "3" || got[2].TraceID != "5" {
		t.Fatalf("expected [3 4 5], got %+v", got)
	}
}
