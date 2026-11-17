package backoff

import (
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
)

const (
	baseMs = 200
	factor = 2
	capMs  = 2000
)

func Pause(attempt int, retryAfter string) int {
	if ms, ok := retryAfterMs(retryAfter); ok {
		return ms
	}
	return rand.IntN(Ceiling(attempt) + 1)
}

func Ceiling(attempt int) int {
	interval := baseMs
	for n := 2; n <= attempt; n++ {
		interval *= factor
		if interval >= capMs {
			return capMs
		}
	}
	return interval
}

func retryAfterMs(value string) (int, bool) {
	value = strings.TrimSpace(value)
	v, err := strconv.Atoi(value)
	if err != nil || v < 0 || v > math.MaxInt/1000 {
		return 0, false
	}
	return v * 1000, true
}
