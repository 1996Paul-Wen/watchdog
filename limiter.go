// Package watchdog provides a rate limiter based on "token bucket" algorithm.
// It is a token bucket algorithm implementation based on **real-time calculation**.
// So the generation of tokens **is continuous rather than discrete**.
package watchdog

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Limiter controls the rate of some events
type Limiter struct {
	limit float64 // It represents the maximum permit per second
	burst float64 // bucket size, fullfilled initially. It represents the maximum concurrency at a certain moment

	tokens   float64    // the generation of tokens **is continuous rather than discrete**
	mu       sync.Mutex // protects tokens from concurrency access
	lastTime time.Time  // last time the tokens was modified
}

// NewLimiter generates a Limiter
func NewLimiter(limit float64, burst float64) *Limiter {
	return &Limiter{
		limit: limit,
		burst: burst,

		mu:       sync.Mutex{},
		tokens:   float64(burst),
		lastTime: time.Now(),
	}
}

// SetLimit sets limit
func (l *Limiter) SetLimit(limit float64) {
	l.limit = limit
}

// SetBurst sets burst
func (l *Limiter) SetBurst(burst float64) {
	l.burst = burst
}

// Tokens gets current tokens
func (l *Limiter) Tokens() float64 {
	return l.tokens
}

// Allow is the shorthand of AllowN(1)
func (l *Limiter) Allow() bool {
	return l.AllowN(1)
}

// AllowN judges if n tokens are availiable now
func (l *Limiter) AllowN(n float64) bool {
	if n < 0 || n > l.burst {
		return false
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	total := l.tokens + tokensFromDuration(now.Sub(l.lastTime), l.limit)
	if total > l.burst {
		total = l.burst
	}

	left := total - n
	if left >= 0 {
		l.lastTime = now
		l.tokens = left
		return true
	}

	return false
}

// Wait is shorthand for WaitN(ctx, 1).
func (l *Limiter) Wait(ctx context.Context) error {
	return l.WaitN(ctx, 1)
}

// WaitN waits until n tokens are available and returns nil, or waits until ctx is cancelled and returns err.
func (l *Limiter) WaitN(ctx context.Context, n float64) error {
	// check if ctx is already cancelled
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if n < 0 || n > l.burst {
		return fmt.Errorf("limiter's burst is %f, and %f tokens invalid", l.burst, n)
	}

	l.mu.Lock()
	now := time.Now()
	total := l.tokens + tokensFromDuration(now.Sub(l.lastTime), l.limit)
	if total > l.burst {
		total = l.burst
	}
	left := total - n

	// make limiter's state updated to current timestamp
	l.lastTime = now
	l.tokens = left
	l.mu.Unlock()

	// control logic
	if left < 0 {
		// time to wait
		timeAfterTrigger := time.After(durationFromTokens(-left, l.limit))

		select {
		case <-timeAfterTrigger:
			return nil
		case <-ctx.Done():
			// give n tokens back to limiter
			l.mu.Lock()
			temp := l.tokens + n
			if temp > l.burst {
				temp = l.burst
			}
			l.tokens = temp
			l.mu.Unlock()
			return ctx.Err()
		}
	}

	return nil
}

// tokensFromDuration is a unit conversion function from a time duration to the number of tokens
// which could be accumulated during that duration at a rate of limit tokens per second.
func tokensFromDuration(d time.Duration, limit float64) float64 {
	// Split the integer and fractional parts ourself to minimize rounding errors.
	// See golang.org/issues/34861.
	sec := float64(d/time.Second) * limit
	nsec := float64(d%time.Second) * limit
	return sec + nsec/1e9
}

// durationFromTokens is a unit conversion function from the number of tokens to the duration
// of time it takes to accumulate them at a rate of limit tokens per second.
func durationFromTokens(tokens, limit float64) time.Duration {
	seconds := tokens / limit
	return time.Nanosecond * time.Duration(1e9*seconds)
}
