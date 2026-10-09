package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestLimiter_BurstDenyRefill(t *testing.T) {
	currTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return currTime }

	// Rate: 2 req/s, Burst: 4
	l := NewWithClock(2.0, 4, clock)

	// First 4 requests should succeed (burst capacity)
	for i := 1; i <= 4; i++ {
		if !l.Allow("user-1") {
			t.Fatalf("request %d should have been allowed within burst", i)
		}
	}

	// 5th request exceeds burst, should be denied
	if l.Allow("user-1") {
		t.Fatal("request 5 should be denied (burst exhausted)")
	}

	// Advance time by 500ms -> refills 1 token (2 tokens/sec * 0.5s = 1 token)
	currTime = currTime.Add(500 * time.Millisecond)
	if !l.Allow("user-1") {
		t.Fatal("request after 500ms should be allowed (1 token refilled)")
	}
	if l.Allow("user-1") {
		t.Fatal("subsequent request should be denied immediately")
	}

	// Advance time by 1 second -> refills 2 tokens
	currTime = currTime.Add(1 * time.Second)
	if !l.Allow("user-1") {
		t.Fatal("1st token from 1s refill should be allowed")
	}
	if !l.Allow("user-1") {
		t.Fatal("2nd token from 1s refill should be allowed")
	}
	if l.Allow("user-1") {
		t.Fatal("3rd request should be denied")
	}

	// Advance time by 100 seconds -> should cap at burst (4)
	currTime = currTime.Add(100 * time.Second)
	for i := 1; i <= 4; i++ {
		if !l.Allow("user-1") {
			t.Fatalf("request %d should have been allowed after cap at burst", i)
		}
	}
	if l.Allow("user-1") {
		t.Fatal("request beyond burst cap should be denied")
	}
}

func TestLimiter_MultipleKeysIndependent(t *testing.T) {
	currTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return currTime }

	l := NewWithClock(1.0, 2, clock)

	// Exhaust user-1
	if !l.Allow("user-1") || !l.Allow("user-1") {
		t.Fatal("user-1 burst failed")
	}
	if l.Allow("user-1") {
		t.Fatal("user-1 should be denied")
	}

	// user-2 should still have full burst
	if !l.Allow("user-2") || !l.Allow("user-2") {
		t.Fatal("user-2 burst failed")
	}
	if l.Allow("user-2") {
		t.Fatal("user-2 should be denied")
	}
}

func TestLimiter_IdleEviction(t *testing.T) {
	currTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return currTime }

	l := NewWithClock(1.0, 2, clock)

	// Consume tokens
	l.Allow("idle-user")
	l.Allow("idle-user")
	if l.Allow("idle-user") {
		t.Fatal("should be denied")
	}

	// Advance beyond 10 minutes idle
	currTime = currTime.Add(11 * time.Minute)

	// A new allow should see fresh state after eviction
	if !l.Allow("idle-user") {
		t.Fatal("idle-user should be allowed with fresh burst after 10 min idle eviction")
	}
}

func TestLimiter_ConcurrentAccess(t *testing.T) {
	l := New(100.0, 100)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = l.Allow("shared-key")
			}
		}(i)
	}
	wg.Wait()
}
