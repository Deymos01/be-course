package policy

import (
	"networks/05_retry/internal/config"
	"networks/05_retry/internal/httpx"
)

func Retryable(status int, err error) bool {
	if err != nil {
		return true
	}

	switch status {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func Idempotent(method string) bool {
	switch method {
	case "GET", "HEAD", "PUT", "DELETE", "OPTIONS", "TRACE":
		return true
	default:
		return false
	}
}

func MayRetry(method, key string) bool {
	return Idempotent(method) || key != ""
}

func ShouldRetry(o httpx.Outcome, cfg config.Config, attempt int) bool {
	return attempt < cfg.MaxAttempts &&
		MayRetry(cfg.Method, cfg.Key) &&
		Retryable(o.Status, o.Err)
}
