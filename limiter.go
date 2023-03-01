// Package watchdog provides a rate limiter based on "token bucket" algorithm.
// It is a token bucket algorithm implementation based on **real-time calculation and ZERO POINT movement**.
package watchdog

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// InfDuration is infinite time duration.
const InfDuration = time.Duration(1<<63 - 1)

// Limiter controls the rate of events.
type Limiter struct {
	limit float64 // it represents the maximum permit per second
	burst float64 // bucket size, fullfilled initially. It represents the maximum concurrency at a certain moment

	zeroPoint time.Time  // zeroPoint marks the start of a new useful time span
	mu        sync.Mutex // protects zeroPoint from concurrency access
}

// NewLimiter generates a Limiter
func NewLimiter(limit float64, burst float64) *Limiter {
	durationToFullfillBucket := durationFromTokens(burst, limit)
	// set state to the lastest ZERO POINT
	return &Limiter{
		limit: limit,
		burst: burst,

		zeroPoint: time.Now().Add(-durationToFullfillBucket),
		mu:        sync.Mutex{},
	}
}

// SetLimit sets limit
func (l *Limiter) SetLimit(limit float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limit = limit
}

// SetBurst sets burst
func (l *Limiter) SetBurst(burst float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.burst = burst
}

// TokensAt returns tokens at time point t
func (l *Limiter) TokensAt(t time.Time) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	tokens := tokensFromDuration(t.Sub(l.zeroPoint), l.limit)
	if tokens > l.burst {
		return l.burst
	}
	return tokens
}

// Allow is the shorthand of AllowN(1)
func (l *Limiter) Allow() bool {
	return l.AllowN(1)
}

// AllowN judges if n tokens are availiable now
func (l *Limiter) AllowN(n float64) bool {
	_, err := l.OccupyTokens(time.Now(), 0, n)
	return err == nil
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

	o, err := l.OccupyTokens(time.Now(), InfDuration, n)
	if err != nil {
		return err
	}

	// Wait if necessary
	delay := o.DelayFrom(time.Now())
	if delay == 0 {
		return nil
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-t.C:
		// We can proceed.
		return nil
	case <-ctx.Done():
		// Context was canceled before we could proceed.  Cancel the
		// occupancy, which may permit other events to proceed sooner.
		o.Cancel()
		return ctx.Err()
	}
}

// Occupancy represents the occupancy of tokens for an event.
type Occupancy struct {
	limiter *Limiter

	tokens    float64   // occupied tokens.
	timeToAct time.Time // it means that the event will happen right at this moment.

	cancelled bool
}

// OccupyTokens tries to occupy tokens at time point `startTime`. If the occupancy cannot be completed immediately, it waits waitTime at most.
// If tokens cannot be occupied after deadline, a nil Occupancy and an error will be return.
func (l *Limiter) OccupyTokens(startTime time.Time, waitTime time.Duration, tokens float64) (o *Occupancy, e error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// : check if input tokens valid.
	if tokens < 0 || tokens > l.burst {
		return nil, fmt.Errorf("limiter's burst is %f, and %f tokens invalid", l.burst, tokens)
	}

	// : move ZERO POINT forward if DEPRECATED TIME SPAN exists
	durationToFullfillBucket := durationFromTokens(l.burst, l.limit)
	deprecatedTimeSpan := startTime.Sub(l.zeroPoint) - durationToFullfillBucket
	if deprecatedTimeSpan > 0 {
		// postpone ZERO POINT by the same length of DEPRECATED TIME SPAN.
		l.zeroPoint = l.zeroPoint.Add(deprecatedTimeSpan)
	}

	// : compare deadline and token ready time.
	deadline := startTime.Add(waitTime)
	durationForNewTokens := durationFromTokens(tokens, l.limit)
	tokensReadyTime := l.zeroPoint.Add(durationForNewTokens)
	if deadline.Before(tokensReadyTime) {
		return nil, fmt.Errorf("tokens are not ready for startTime: %s, waitTime: %d ms",
			startTime.String(), waitTime.Milliseconds())
	}

	// : divide branches according to the sequence relationship between startTime and limiter.zeroPoint

	//    startTime  limiter.zeroPoint                   tokensReadyTime   deadline
	// ---------|-----------|---------------------------------|----------------|->  timeline
	//                      |<         to  occupy            >|
	if startTime.Before(l.zeroPoint) {
		// generate Occupancy
		o = &Occupancy{
			limiter:   l,
			tokens:    tokens,
			timeToAct: tokensReadyTime,
		}
	} else {
		// generate Occupancy
		o = &Occupancy{
			limiter: l,
			tokens:  tokens,
		}
		// choose the later time between startTime and tokensReadyTime as timeToAct
		if tokensReadyTime.Before(startTime) {
			// limiter.zeroPoint               tokensReadyTime startTime    deadline
			// ------------|-------------------------|-------------|--------------|--->  timeline
			//             |<      to occupy        >|
			o.timeToAct = startTime
		} else {
			// limiter.zeroPoint                startTime  tokensReadyTime  deadline
			// ------------|--------------------------|-----------|--------------|-->  timeline
			//             |<      to occupy                     >|
			o.timeToAct = tokensReadyTime
		}
	}

	// move ZERO POINT forward for occupying tokens
	l.zeroPoint = tokensReadyTime

	return
}

// IsCancelled infers the Occupancy is cancelled or not.
func (o *Occupancy) IsCancelled() bool {
	return o.cancelled
}

// Cancel is the shorthand for CancelAt(time.Now()).
func (o *Occupancy) Cancel() {
	o.CancelAt(time.Now())
}

// CancelAt cancels the Occupancy and gives tokens back to the limiter.
func (o *Occupancy) CancelAt(t time.Time) {
	// already cancelled
	if o.IsCancelled() {
		return
	}

	// t is after o.timeToAct
	if t.After(o.timeToAct) {
		o.cancelled = true
		return
	}

	// give tokens back to o.limiter
	l := o.limiter
	l.mu.Lock()
	defer l.mu.Unlock()

	// move ZERO POINT backward
	reversedDuration := durationFromTokens(o.tokens, l.limit)
	l.zeroPoint = l.zeroPoint.Add(-reversedDuration)

	o.cancelled = true
}

// DelayFrom returns the duration for which the reservation holder must wait
// before taking the reserved action.  Zero duration means act immediately.
func (o *Occupancy) DelayFrom(t time.Time) time.Duration {
	delay := o.timeToAct.Sub(t)
	if delay < 0 {
		return 0
	}
	return delay
}

// durationFromTokens is a unit conversion function from the number of tokens to the duration
// of time it takes to accumulate them at a rate of limit tokens per second.
func durationFromTokens(tokens, limit float64) time.Duration {
	seconds := tokens / limit
	return time.Nanosecond * time.Duration(1e9*seconds)
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
