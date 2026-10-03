package verexa

import (
	"testing"
	"time"
)

func testBreaker() (*CircuitBreaker, *time.Time) {
	now := time.Unix(0, 0)
	b := NewCircuitBreaker(3, time.Second)
	b.now = func() time.Time { return now }
	return b, &now
}

func TestBreakerStaysClosedUnderThreshold(t *testing.T) {
	b, _ := testBreaker()
	b.RecordFailure()
	b.RecordFailure()
	if b.IsOpen() {
		t.Fatal("opened below threshold")
	}
}

func TestBreakerOpensAtThreshold(t *testing.T) {
	b, _ := testBreaker()
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}
	if !b.IsOpen() {
		t.Fatal("expected open")
	}
}

func TestBreakerResetsOnSuccess(t *testing.T) {
	b, _ := testBreaker()
	b.RecordFailure()
	b.RecordFailure()
	b.RecordSuccess()
	b.RecordFailure()
	if b.IsOpen() {
		t.Fatal("success should reset the count")
	}
}

func TestBreakerClosesAfterCoolDown(t *testing.T) {
	b, now := testBreaker()
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}
	*now = now.Add(999 * time.Millisecond)
	if !b.IsOpen() {
		t.Fatal("closed before cool-down ended")
	}
	*now = now.Add(time.Millisecond)
	if b.IsOpen() {
		t.Fatal("still open after cool-down")
	}
}

func TestBreakerGivesFullBudgetAfterCoolDown(t *testing.T) {
	b, now := testBreaker()
	for i := 0; i < 3; i++ {
		b.RecordFailure()
	}
	*now = now.Add(time.Second)
	if b.IsOpen() {
		t.Fatal("expected closed")
	}
	b.RecordFailure()
	if b.IsOpen() {
		t.Fatal("a single failure after cool-down re-opened the breaker")
	}
}
