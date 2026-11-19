package retry

import (
	"networks/05_retry/internal/backoff"
	"networks/05_retry/internal/config"
	"networks/05_retry/internal/httpx"
	"networks/05_retry/internal/policy"
	"networks/05_retry/internal/report"
	"time"
)

func Run(cfg config.Config, c *httpx.Client, r report.Reporter) (ok bool, attempts int) {
	var last httpx.Outcome
	attempt := 0

	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}

	for attempt < cfg.MaxAttempts {
		attempt++
		last = c.Do()
		r.Attempt(attempt, last.Status, last.Err)

		if !policy.ShouldRetry(last, cfg, attempt) {
			break
		}

		d := backoff.Pause(attempt+1, last.RetryAfter)
		r.Sleep(d)
		time.Sleep(d)
	}

	return last.Success(), attempt
}
