package verexa

import (
	"sync"
	"time"
)

type CircuitBreaker struct {
	mu                  sync.Mutex
	failureThreshold    int
	coolDown            time.Duration
	consecutiveFailures int
	openedAt            time.Time
	open                bool
	now                 func() time.Time
}

func NewCircuitBreaker(failureThreshold int, coolDown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{failureThreshold: failureThreshold, coolDown: coolDown, now: time.Now}
}

// IsOpen also clears the failure count when the cool-down expires. Without
// that the breaker stays one failure away from tripping forever: the next
// error after the cool-down re-opens it immediately and it never gets a real
// trial request.
func (b *CircuitBreaker) IsOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return false
	}
	if b.now().Sub(b.openedAt) >= b.coolDown {
		b.open = false
		b.consecutiveFailures = 0
		return false
	}
	return true
}

func (b *CircuitBreaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutiveFailures = 0
	b.open = false
}

func (b *CircuitBreaker) RecordFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.consecutiveFailures++
	if b.consecutiveFailures >= b.failureThreshold {
		b.open = true
		b.openedAt = b.now()
	}
}
