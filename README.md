# 1. Overview
Package `watchdog` provides an easy-to-use and reliable rate limiter based on "token bucket" algorithm. 

It is implemented using **ZERO POINT movement mechanism**. 

`watchdog` has **2** advantages:

- **Simpler implementation**. Thanks to ZERO POINT movement mechanism, the Structs and Token Calculation Methods provided by `watchdog` are both simpler than `golang.org/x/time/rate`, the official implementation.

- **More reliable limiting mechanism**. `watchdog` calculates the token correctly, providing reliability. The logic of `golang.org/x/time/rate` is wrong because it considers `reserved tokens` when returning tokens back to the limiter. This bug has been easily fixed by `watchdog`'s ZERO POINT movement mechanism.

At the usage level, `watchdog` completely covers the functions of `golang.org/x/time/rate` and maintains a similar interface, so it does not require too much mental burden to use.

（`watchdog`最大的优点是使用了零点移动机制，这使得它比官方实现`golang.org/x/time/rate`更简单，更容易理解，并且计算token也更加可靠）

# 2. Original impetus of development
### 2.1 First, to make "token bucket" algorithm implementation simple and stupid

The official implementation `golang.org/x/time/rate` is a little bit complicated due to its dizzying calculations.

So, `watchdog` proposes 2 core concepts: **[OCCUPIED TIME SPAN]** & **[ZERO POINT]**, to make things simple.

Based on these two concepts, ***the complex token calculation is transformed into a simple ZERO POINT movement mechanism***.

（`watchdog`提出了2个核心概念：OCCUPIED TIME SPAN 和 ZERO POINT，并基于这两个概念将复杂的token计算转化为了简单的零点移动机制）

For specific design ideas, see [My understanding of "token bucket" algorithm] below.

### 2.2 Second, to fix calculation bugs of `golang.org/x/time/rate`

The method of calculating the token in `golang.org/x/time/rate` is wrong. Related discussions can refer to:

  >https://github.com/golang/go/issues/56924

  >https://stackoverflow.com/questions/70993567/rate-limiting-cancellation-token-restore

  >https://www.v2ex.com/t/877175

  >https://lailin.xyz/post/go-training-week6-3-token-bucket-2.html#comments

  >https://learnku.com/go/t/71323

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
After `r` cancelled and tokens returned back at t3 moment, the bucket of the limiter should keep 6 tokens rather than 4: 
- On the one hand, `r` occupied 10 tokens before cancellation, so it's a common sense that `r` should return 10 tokens after its cancellation.
- On the other hand, 23 tokens in total were generated from t0 to t3, and 17 tokens in total have been consumed during the same period, so 6 left exactly.

`watchdog` fixes this calculation bug. In `watchdog`, **how many tokens are taken and how many will be returned if cancellation happens, as if nothing happened**.

# 3. My understand of "token bucket" algorithm

### 3.1 Pre-knowledge preparation: Events are distributed on the timeline in the form of time spans
**We say that an event requires several tokens, but what is actually required is the time span corresponding to these tokens on the timeline**. The tokens generated within this time span are all occupied by this event. 

（我们说一个事件需求若干token，实际需求的是时间线上这些token对应的一段时间跨度。这个时间跨度内产生的token，都被该事件占有）

**From this perspective, each event is distributed on the timeline in the form of a time span**. And the time spans do not overlap with each other.

（从这个视角看，每个事件都以一段时间跨度的形式分布在时间线上，并且跨度之间互不重合）

We visualize this understanding as below.
```
    ...  >|< event1 occupied this time span, which generates 3 tokens   >|<   event2 occupied this span, with 2 t >|<  ...  >
----------|--------------------|--------------------|--------------------|--------------------|--------------------|---> timeline
```

Here, it can also be clearly found that it is wrong to consider `reserved tokens` when returning tokens in the reservation cancellation in `golang.org/x/time/rate` package, **because the token (essentially the time span) occupied by each event is independent of each other**.

### 3.2 OCCUPIED TIME SPAN

**OCCUPIED TIME SPAN** indicates an event's occupation of a time span on the timeline. An event holds an OCCUPIED TIME SPAN, which means **that all tokens generated within the time span will be used by the event**.

（OCCUPIED TIME SPAN 表示事件对时间线上一个时间跨度的占有。一个事件持有一个OCCUPIED TIME SPAN，表示该时间跨度内产生的所有token都将归该事件使用）

### 3.3 DEPRECATED TIME SPAN

Because the capacity of the bucket is limited, if there are no new events for a long period of time, the tokens that exceed the capacity of the bucket among all tokens produced during this period will be discarded(deprecated tokens). The time span corresponding to deprecated tokens is called **DEPRECATED TIME SPAN**, which is useless.

（因为bucket的容量有限，如果一段较长的时间内没有新事件，那么这段时间内生产出的所有token中超出bucket容量的那部分token会被丢弃（deprecated tokens）。deprecated tokens 对应的时间跨度称为 DEPRECATED TIME SPAN，这个跨度内的时间都认为是无用的。
注：我们可以将bucket视为队列，DEPRECATED TIME SPAN内产生的tokens根据先进先出原则都被出队丢弃了）

### 3.4 ZERO POINT

time spans are continuous. That is, there is no gaps between these time spans on the timeline.

Let's focus on the time point that marks the start of a new useful time span. We call this time point **ZERO POINT**. This is because the tokens that can be applied for at this time point is **always 0**, and the OCCUPIED TIME SPAN of a new event **can only start from this point at the earliest**.

（让我们关注标志着当前时间线上最近一个【新的、可用的时间跨度】开始的时间点。 
我们称这个时间点为ZERO POINT。 这是因为此刻可申请的tokens总是为0，而新事件的 OCCUPIED TIME SPAN 最早只能从该点开始）

eg.
```
                    ZERO POINT  
...-----------------------|---------------------------------|----------------|->  timeline
...last OccupiedTimeSpan >|<  new time span to  occupy     >|
```

### 3.5 ZERO POINT movement logic

Here, we are freed from complex calculations of time and tokens(how many tokens are there at what time point), and implement the token bucket algorithm by simply moving ZERO POINT on the timeline.

（在这里，我们从时间和token的复杂计算关系（在什么时间点有多少token）中解放出来，通过ZERO POINT在时间线上的简单移动来实现token bucket算法）

There are only **3** situations where ZERO POINT can be moved:
- **[occupying tokens]**. 
  
  Occupying tokens is occupying the related time span, which is to **move the ZERO POINT forward by the same length of OCCUPIED TIME SPAN**. 

- **[cancelling occupied tokens]**. 
  
  Cancelling occupied tokens is cancelling the OCCUPIED TIME SPAN, which is to **move the ZERO POINT backward by the same length of OCCUPIED TIME SPAN**. And this is the only way to move the ZERO POINT backward.

- **[DEPRECATED TIME SPAN appearing]**. 
  
  Obviously, the appearance of DEPRECATED TIME SPAN will **move ZERO POINT forward by the same length of DEPRECATED TIME SPAN**.

  （显然，DEPRECATED TIME SPAN 的出现会将 ZERO POINT 沿着时间线的未来方向前移相同的时间长度）


-------
# Appendix

`watchdog` can calculate tokens correctly, just as the example shows below:
```
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
```
