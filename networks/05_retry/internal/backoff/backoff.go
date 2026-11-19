package backoff

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

const (
	baseDelay         = 200 * time.Millisecond
	factor            = 2
	capDelay          = 2000 * time.Millisecond
	maxRetryAfterSecs = int64(math.MaxInt64) / int64(time.Second)
)

func Pause(attempt int, retryAfter string) time.Duration {
	if d, ok := retryAfterDelay(retryAfter); ok {
		return d
	}
	ceilingMs := int(Ceiling(attempt) / time.Millisecond)
	return time.Duration(rand.IntN(ceilingMs+1)) * time.Millisecond
}

func Ceiling(attempt int) time.Duration {
	interval := baseDelay
	for n := 2; n <= attempt; n++ {
		interval *= factor
		if interval >= capDelay {
			return capDelay
		}
	}
	return interval
}

func retryAfterDelay(value string) (time.Duration, bool) {
	secs, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || secs < 0 || secs > maxRetryAfterSecs {
		return 0, false
	}
	return time.Duration(secs) * time.Second, true
}
