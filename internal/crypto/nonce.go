package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// nonceEntry holds a nonce value and its expiration time.
type nonceEntry struct {
	expiresAt time.Time
}

// NonceStore is a thread-safe, in-memory store for single-use nonces with TTL.
// Expired nonces are periodically cleaned up by a background goroutine.
type NonceStore struct {
	mu       sync.Mutex
	nonces   map[string]nonceEntry
	ttl      time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewNonceStore creates a new NonceStore with the given TTL for nonces.
// A background goroutine cleans up expired nonces every 30 seconds.
// Call Stop() to stop the cleanup goroutine.
func NewNonceStore(ttl time.Duration) *NonceStore {
	return newNonceStoreWithInterval(ttl, 30*time.Second)
}

// newNonceStoreWithInterval creates a NonceStore with a custom cleanup interval.
// This is used in tests to verify cleanup behavior without waiting 30 seconds.
func newNonceStoreWithInterval(ttl, cleanupInterval time.Duration) *NonceStore {
	s := &NonceStore{
		nonces: make(map[string]nonceEntry),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go s.cleanupLoop(cleanupInterval)
	return s
}

// Create generates a new 32-byte random nonce (hex-encoded), stores it with
// an expiry time, and returns it.
func (s *NonceStore) Create() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	nonce := hex.EncodeToString(b)

	s.mu.Lock()
	s.nonces[nonce] = nonceEntry{expiresAt: time.Now().Add(s.ttl)}
	s.mu.Unlock()

	return nonce
}

// Consume checks if the nonce exists and has not expired. If valid, it deletes
// the nonce and returns true (single-use). Otherwise returns false.
func (s *NonceStore) Consume(nonce string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.nonces[nonce]
	if !ok {
		return false
	}
	delete(s.nonces, nonce)

	if time.Now().After(entry.expiresAt) {
		return false
	}
	return true
}

// Stop stops the background cleanup goroutine. Safe to call multiple times.
func (s *NonceStore) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
}

// cleanupLoop periodically removes expired nonces from the store.
func (s *NonceStore) cleanupLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// cleanup removes all expired nonces from the store.
func (s *NonceStore) cleanup() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	for nonce, entry := range s.nonces {
		if now.After(entry.expiresAt) {
			delete(s.nonces, nonce)
		}
	}
}
