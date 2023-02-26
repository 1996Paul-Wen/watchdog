package watchdog

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	// 10/s, and only 1 will be permitted at one certain moment
	// in other words, one single event is permitted every 100ms
	l := NewLimiter(10, 1)

	var headOff int64 = 0
	tick := time.NewTicker(50 * time.Millisecond)
	for i := 0; i < 20; i++ {
		if !l.Allow() {
			headOff++
		}
		<-tick.C
	}
	tick.Stop()
	fmt.Println(headOff)
}
