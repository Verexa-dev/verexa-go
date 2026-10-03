package verexa

import (
	"crypto/sha256"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	memoTTL  = 5 * time.Minute
	memoSize = 1024
)

// blockMemo remembers blocked turns so a client that retries a failed round
// trip gets the same verdict back instead of re-running the turn. openai-go
// retries every transport error, which would otherwise call the model again
// after an output block and re-check a blocked input.
type blockMemo struct {
	mu      sync.Mutex
	entries map[[32]byte]memoEntry
}

type memoEntry struct {
	err     *BlockedError
	expires time.Time
}

func memoKey(req *http.Request, body []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte(req.Method + " " + req.URL.String() + "\n"))
	h.Write(body)
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}

// isRetry reports whether req is a retry from a Stainless-generated client
// (openai-go and friends). Only retries consult the memo, so an identical
// fresh request is always checked again.
func isRetry(req *http.Request) bool {
	n, err := strconv.Atoi(req.Header.Get("X-Stainless-Retry-Count"))
	return err == nil && n > 0
}

func (m *blockMemo) get(key [32]byte) *BlockedError {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[key]
	if !ok || time.Now().After(e.expires) {
		return nil
	}
	return e.err
}

func (m *blockMemo) put(key [32]byte, err *BlockedError) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if m.entries == nil {
		m.entries = map[[32]byte]memoEntry{}
	}
	if len(m.entries) >= memoSize {
		for k, e := range m.entries {
			if now.After(e.expires) {
				delete(m.entries, k)
			}
		}
	}
	for k := range m.entries {
		if len(m.entries) < memoSize {
			break
		}
		delete(m.entries, k)
	}
	m.entries[key] = memoEntry{err: err, expires: now.Add(memoTTL)}
}
