package ratelimit

import (
	"sync"
	"time"
)

const (
	defaultIdleTimeout = 10 * time.Minute
	cleanupInterval    = 1 * time.Minute
)

type bucket struct {
	tokens     float64
	lastUpdate time.Time
	lastAccess time.Time
}

// Limiter is a thread-safe, per-key token bucket rate limiter.
type Limiter struct {
	mu          sync.Mutex
	rate        float64
	burst       int
	clock       func() time.Time
	idleTimeout time.Duration
	lastCleanup time.Time
	buckets     map[string]*bucket
}

// New creates a new Limiter with the given rate (tokens/second) and burst capacity.
func New(ratePerSec float64, burst int) *Limiter {
	return NewWithClock(ratePerSec, burst, time.Now)
}

// NewWithClock creates a Limiter with a custom clock function for deterministic testing.
func NewWithClock(ratePerSec float64, burst int, clock func() time.Time) *Limiter {
	now := clock()
	return &Limiter{
		rate:        ratePerSec,
		burst:       burst,
		clock:       clock,
		idleTimeout: defaultIdleTimeout,
		lastCleanup: now,
		buckets:     make(map[string]*bucket),
	}
}

// Allow reports whether an event for key is permitted under the rate limit.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock()

	// Periodic eviction of idle keys
	if now.Sub(l.lastCleanup) >= cleanupInterval {
		for k, b := range l.buckets {
			if now.Sub(b.lastAccess) >= l.idleTimeout {
				delete(l.buckets, k)
			}
		}
		l.lastCleanup = now
	}

	b, exists := l.buckets[key]
	if !exists {
		b = &bucket{
			tokens:     float64(l.burst),
			lastUpdate: now,
			lastAccess: now,
		}
		l.buckets[key] = b
	} else if now.Sub(b.lastAccess) >= l.idleTimeout {
		// Key was idle beyond timeout; reset bucket to fresh burst
		b.tokens = float64(l.burst)
		b.lastUpdate = now
		b.lastAccess = now
	} else {
		// Refill tokens based on elapsed time
		elapsed := now.Sub(b.lastUpdate).Seconds()
		if elapsed > 0 {
			b.tokens += elapsed * l.rate
			if b.tokens > float64(l.burst) {
				b.tokens = float64(l.burst)
			}
			b.lastUpdate = now
		}
		b.lastAccess = now
	}

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}

	return false
}
