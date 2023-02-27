# Introduction
Package watchdog provides a rate limiter based on "token bucket" algorithm. It is a token bucket algorithm implementation based on **real-time calculation**. So the generation of tokens **is continuous rather than discrete**.

# Origin of development
- `golang.org/x/time/rate` is the official implementation of "token bucket" algorithm, but it's a little bit complicated. I want to make it more simple and easier to understand, not like `golang.org/x/time/rate`.
- What's more, I think the method of calculating the token in `golang.org/x/time/rate` is wrong.
For an example from https://github.com/golang/go/issues/56924:
```
func Test_tr(t *testing.T) {
	tr()
}

func tr() {
	t0 := time.Now()
	t1 := time.Now().Add(100 * time.Millisecond) // + 1
	t2 := time.Now().Add(200 * time.Millisecond) // + 2
	t3 := time.Now().Add(300 * time.Millisecond) // + 3

	l := rate.NewLimiter(10, 20)
	l.ReserveN(t0, 15)
	fmt.Printf("t0: %+v\n", l) // bucket left 5 token, and l.lastEvent is t0

	r := l.ReserveN(t1, 10)
	fmt.Printf("t1: %+v\n", l) // bucket left -4 token (5+1-10)
	// r.timeToAct is t1.Add(400 * time.Millisecond), the same as l.lastEvent (that is t5)

	l.ReserveN(t2, 2)
	fmt.Printf("t2: %+v\n", l) // bucket left -5 token (-4+1-2)
	// l.lastEvent is t2.Add(500 * time.Millisecond) (that is t7)

	r.CancelAt(t3) // when we see source code in CancelAt, now logic is: -5 + 1 + (10 - 2) = 4
	// but we expected logic is: -5 + 1 + 10 = 6, this is the right answer. 
    // and (20 + 3) - (15 + 2) is actually 6
	fmt.Printf("t3: %+v\n", l) // bucket left 4 token, which should be wrong.
}
```
I have added some comments to make this example more clear to understand. After r cancelled and tokens returned back at t3 moment, the bucket of the limiter should keep 6 tokens rather than 4. 
On the one hand, r occupied 10 tokens before cancellation, so it's clear that r should return 10 tokens after cancellation.
On the other hand, 23 tokens in total were generated from t0 to t3, and 17 tokens in total have been consumed during the same period, so 6 left exactly.

`watchdog` fixes this. **How many tokens are taken and how many will be returned if cancellation happens, as if nothing happened**.

# Deep understand of "token bucket" algorithm
**We say that an event requires several tokens, but what is actually required is the time span corresponding to these tokens on the timeline**. 
From this perspective, each event is distributed on the timeline in the form of a span of time, and the spans do not overlap with each other.
我们说一个事件需求若干token，实际需求的是时间线上这些token对应的一段时间跨度。从这个视角看，每个事件都以一段时间跨度的形式分布在时间线上，并且跨度之间互不重合。

We visualize this understanding as below.
```
    ...  >|< event1 occupied 3 time spans. that is it occupied 3 tokens >|<    event2 occupied 2 time spans       >|<  ...  >
----------|--------------------|--------------------|--------------------|--------------------|--------------------|---> timeline
```

From this perspective, it can also be clearly found that it is wrong to consider `reserved tokens` when returning tokens in the reservation cancellation in the `golang.org/x/time/rate` package, **because the token (essentially the time span) occupied by each event is independent of each other**.


**If an event occupies n additional time spans forward at a certain point in time, then the earliest time occupancy starting point of other events will be postponed by n time spans. In other words, other events need to wait for the n time span to end before starting their own time spans.** 
For example, event2 need to wait for event1's time spans to end before starting its own time spans.
一个事件在某个时间点额外向前占用n个时间跨度，那么其他事件的最早时间占用起点就会随之顺延n个时间跨度。换句话说，其他事件需要等待这n个时间跨度结束后，才能开始属于自己的时间