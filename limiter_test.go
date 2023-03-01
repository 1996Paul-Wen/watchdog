package watchdog

import (
	"context"
	"fmt"
	"testing"
	"time"
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
