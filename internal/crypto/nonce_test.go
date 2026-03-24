package crypto

import (
	"testing"
	"time"
)

func TestNonceCreateAndConsume(t *testing.T) {
	store := NewNonceStore(5 * time.Minute)
	defer store.Stop()

	nonce := store.Create()
	if nonce == "" {
		t.Fatal("Create returned empty nonce")
	}
	if !store.Consume(nonce) {
		t.Fatal("Consume returned false for a valid nonce")
	}
}

func TestNonceDoubleConsume(t *testing.T) {
	store := NewNonceStore(5 * time.Minute)
	defer store.Stop()

	nonce := store.Create()
	if !store.Consume(nonce) {
		t.Fatal("first Consume should return true")
	}
	if store.Consume(nonce) {
		t.Fatal("second Consume should return false")
	}
}

func TestNonceExpiry(t *testing.T) {
	store := NewNonceStore(50 * time.Millisecond)
	defer store.Stop()

	nonce := store.Create()
	time.Sleep(100 * time.Millisecond)
	if store.Consume(nonce) {
		t.Fatal("Consume should return false for expired nonce")
	}
}

func TestNonceCleanup(t *testing.T) {
	// Use a very short TTL and a custom cleanup interval to test cleanup.
	store := newNonceStoreWithInterval(50*time.Millisecond, 50*time.Millisecond)
	defer store.Stop()

	_ = store.Create()
	_ = store.Create()
	_ = store.Create()

	// Wait for expiry + cleanup cycle.
	time.Sleep(200 * time.Millisecond)

	store.mu.Lock()
	count := len(store.nonces)
	store.mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 nonces after cleanup, got %d", count)
	}
}
