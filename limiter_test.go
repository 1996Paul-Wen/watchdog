package watchdog

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestWait(t *testing.T) {
	l := NewLimiter(100, 1)
	begin := time.Now()
	for i := 0; i < 200; i++ {
		if err := l.WaitN(context.Background(), 1); err != nil {
			fmt.Println(i, err)
		}
	}
	fmt.Println(time.Since(begin).Milliseconds())
}

func TestTokens(t *testing.T) {
	t0 := time.Now()
	t1 := time.Now().Add(100 * time.Millisecond) // + 1
	t2 := time.Now().Add(200 * time.Millisecond) // + 2
	t3 := time.Now().Add(300 * time.Millisecond) // + 3

	l := NewLimiter(10, 20)
	fmt.Printf("begining tokens: %+v\n", l.TokensAt(time.Now()))

	l.OccupyTokens(t0, InfDuration, 15)
	fmt.Printf("t0 tokens: %+v\n", l.TokensAt(time.Now())) // bucket left 5 token

	o, err := l.OccupyTokens(t1, InfDuration, 10)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("t1 tokens: %+v\n", l.TokensAt(t1)) // bucket left -4 token

	l.OccupyTokens(t2, InfDuration, 2)
	fmt.Printf("t2 tokens: %+v\n", l.TokensAt(t2)) // bucket left -5 token

	o.CancelAt(t3)
	fmt.Printf("t3 tokens: %+v\n", l.TokensAt(t3)) // bucket left 6 token
}

func pressureTestWatchdog(limit int) {
	l := NewLimiter(float64(limit), 1)
	wg := sync.WaitGroup{}
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := l.WaitN(context.Background(), 1); err != nil {
				fmt.Println(i, err)
			}
		}(i)
	}
	wg.Wait()
}

// go test -benchmem -benchtime 30s -run=^$ -bench ^BenchmarkWatchdogMillionConcurrency$ github.com/1996Paul-Wen/watchdog
func BenchmarkWatchdogMillionConcurrency(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pressureTestWatchdog(5000000)
	}
}

func pressureTestRatelimiter(limit int) {
	l := rate.NewLimiter(rate.Limit(limit), 1)
	wg := sync.WaitGroup{}
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := l.WaitN(context.Background(), 1); err != nil {
				fmt.Println(i, err)
			}
		}(i)
	}
	wg.Wait()
}

// go test -benchmem -benchtime 30s -run=^$ -bench ^BenchmarkRatelimiterMillionConcurrency$ github.com/1996Paul-Wen/watchdog
func BenchmarkRatelimiterMillionConcurrency(b *testing.B) {
	for i := 0; i < b.N; i++ {
		pressureTestRatelimiter(5000000)
	}
}
