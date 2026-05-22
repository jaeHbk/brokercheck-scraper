package main

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter is a process-wide token-bucket limiter with adaptive
// circuit-breaker behavior: rate halves on error responses and
// recovers gradually after sustained success.
type Limiter struct {
	mu         sync.Mutex
	rl         *rate.Limiter
	ceiling    float64
	floor      float64
	current    float64
	cleanSince time.Time // start of the current clean streak (or last adjustment)
}

func newLimiter(ratePerSec, floor float64) *Limiter {
	return &Limiter{
		rl:         rate.NewLimiter(rate.Limit(ratePerSec), 1),
		ceiling:    ratePerSec,
		floor:      floor,
		current:    ratePerSec,
		cleanSince: time.Now(),
	}
}

// Wait blocks until a token is available or ctx is canceled.
func (l *Limiter) Wait(ctx context.Context) error {
	return l.rl.Wait(ctx)
}

// onError halves the current rate (floored at l.floor) and resets
// the clean-streak timer. Returns the recommended pause duration.
func (l *Limiter) onError() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	old := l.current
	l.current = math.Max(l.floor, l.current/2)
	l.rl.SetLimit(rate.Limit(l.current))
	l.cleanSince = time.Now()
	if old != l.current {
		log.Printf("limiter: error response; rate %.2f -> %.2f req/s, pausing 60s", old, l.current)
	}
	return 60 * time.Second
}

// onSuccess increases the current rate toward the ceiling if the
// most recent adjustment was at least 5 minutes ago.
func (l *Limiter) onSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.current >= l.ceiling {
		return
	}
	if time.Since(l.cleanSince) >= 5*time.Minute {
		old := l.current
		l.current = math.Min(l.ceiling, l.current*1.25)
		l.rl.SetLimit(rate.Limit(l.current))
		l.cleanSince = time.Now()
		log.Printf("limiter: clean streak; rate %.2f -> %.2f req/s", old, l.current)
	}
}

// currentRate is exposed for testing.
func (l *Limiter) currentRate() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current
}
