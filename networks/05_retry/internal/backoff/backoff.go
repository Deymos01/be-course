package backoff

import (
	"errors"
	"fmt"
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

var ErrRetryAfterOutOfRange = errors.New("Retry-After out of range")

func Pause(attempt int, retryAfter string) (time.Duration, error) {
	d, ok, err := retryAfterDelay(retryAfter)
	if err != nil {
		return 0, err
	}
	if ok {
		return d, nil
	}
	ceilingMs := int(Ceiling(attempt) / time.Millisecond)
	return time.Duration(rand.IntN(ceilingMs+1)) * time.Millisecond, nil
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

func retryAfterDelay(value string) (time.Duration, bool, error) {
	trim := strings.TrimSpace(value)
	if !isDecimal(trim) {
		return 0, false, nil
	}
	secs, err := strconv.ParseInt(trim, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%w: %q", ErrRetryAfterOutOfRange, trim)
	}
	if secs > maxRetryAfterSecs {
		return 0, false, fmt.Errorf("%w: %d s", ErrRetryAfterOutOfRange, secs)
	}
	return time.Duration(secs) * time.Second, true, nil
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}