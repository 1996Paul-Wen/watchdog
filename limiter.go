// Package watchdog provides a rate limiter based on "token bucket" algorithm.
// It is a token bucket algorithm implementation based on **real-time calculation**.
// So the generation of tokens **is continuous rather than discrete**.
package watchdog

import (
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

// tokensFromDuration is a unit conversion function from a time duration to the number of tokens
// which could be accumulated during that duration at a rate of limit tokens per second.
func tokensFromDuration(d time.Duration, limit float64) float64 {
	// Split the integer and fractional parts ourself to minimize rounding errors.
	// See golang.org/issues/34861.
	sec := float64(d/time.Second) * limit
	nsec := float64(d%time.Second) * limit
	return sec + nsec/1e9
}
