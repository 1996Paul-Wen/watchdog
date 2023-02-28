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

// InfDuration is infinite time duration.
const InfDuration = time.Duration(1<<63 - 1)

// Limiter controls the rate of events.
type Limiter struct {
	limit float64 // it represents the maximum permit per second
	burst float64 // bucket size, fullfilled initially. It represents the maximum concurrency at a certain moment

	tokens float64    // the generation of tokens **is continuous rather than discrete**
	mu     sync.Mutex // protects tokens from concurrency access

	// lastTime is the tokens-related timestamp
	lastTime time.Time

	// occupied time spans are continuous. that is, there is no gaps between time spans on the timeline.
	// endOfLastOccupiedTimeSpan marks the time point corresponding to the end of the last occupied time span.
	endOfLastOccupiedTimeSpan time.Time
}

// NewLimiter generates a Limiter
func NewLimiter(limit float64, burst float64) *Limiter {
	now := time.Now()
	durationToFullfillBucket := durationFromTokens(burst, limit)
	// set state to the lastest ZERO POINT
	return &Limiter{
		limit: limit,
		burst: burst,

		mu:     sync.Mutex{},
		tokens: 0,

		lastTime:                  now.Add(-durationToFullfillBucket),
		endOfLastOccupiedTimeSpan: now.Add(-durationToFullfillBucket),
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

	// : compare deadline and token ready time.
	deadline := startTime.Add(waitTime)
	// if deprecatedTimeSpan exists, postpone l.endOfLastOccupiedTimeSpan(aka, ZERO POINT).
	durationToFullfillBucket := durationFromTokens(l.burst, l.limit)
	deprecatedTimeSpan := startTime.Sub(l.endOfLastOccupiedTimeSpan) - durationToFullfillBucket
	if deprecatedTimeSpan > 0 {
		l.endOfLastOccupiedTimeSpan = l.endOfLastOccupiedTimeSpan.Add(deprecatedTimeSpan)
	}

	durationForNewTokens := durationFromTokens(tokens, l.limit)
	// the tokens-related time span occupied by this request can start at the time point `limiter.endOfLastOccupiedTimeSpan` at the earliest.
	// and at the `l.endOfLastOccupiedTimeSpan` time point, the tokens of the limiter are always ZERO.
	tokensReadyTime := l.endOfLastOccupiedTimeSpan.Add(durationForNewTokens)

	if deadline.Before(tokensReadyTime) {
		return nil, fmt.Errorf("tokens are not ready for startTime: %s, waitTime: %d ms",
			startTime.String(), waitTime.Milliseconds())
	}

	// : divide branches according to the sequence relationship between startTime and limiter.endOfLastOccupiedTimeSpan

	//    startTime  limiter.endOfLastOccupiedTimeSpan  tokensReadyTime   deadline
	// ---------|-----------|---------------------------------|----------------|->  timeline
	//                      |<         to  occupy            >|
	if startTime.Before(l.endOfLastOccupiedTimeSpan) {
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
			// limiter.endOfLastOccupiedTimeSpan tokensReadyTime startTime    deadline
			// ------------|-------------------------|-------------|--------------|--->  timeline
			//             |<      to occupy        >|
			o.timeToAct = startTime
		} else {
			// limiter.endOfLastOccupiedTimeSpan  startTime  tokensReadyTime  deadline
			// ------------|--------------------------|-----------|--------------|-->  timeline
			//             |<      to occupy                     >|
			o.timeToAct = tokensReadyTime
		}
	}

	// update limiter's tokens to time point `tokensReadyTime` as endOfLastOccupiedTimeSpan
	l.tokens = 0
	l.lastTime = tokensReadyTime
	l.endOfLastOccupiedTimeSpan = tokensReadyTime

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

	// give tokens back to o.limiter by simplely backtracking endOfLastOccupiedTimeSpan
	l := o.limiter
	l.mu.Lock()
	defer l.mu.Unlock()

	reversedDuration := durationFromTokens(o.tokens, l.limit)
	// endOfLastOccupiedTimeSpan回退相应的时间跨度 来 归还tokens
	l.endOfLastOccupiedTimeSpan = l.endOfLastOccupiedTimeSpan.Add(-reversedDuration)
	l.lastTime = l.endOfLastOccupiedTimeSpan

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
