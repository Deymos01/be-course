package config

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strings"
)

const tokenChars = "!#$%&'*+-.^_`|~"

type Config struct {
	URL         string
	Method      string
	MaxAttempts int
	Key         string
}

func ParseArgs(args []string) (Config, error) {
	if len(args) == 0 {
		return Config{}, errors.New("expected url")
	}
	cfg := Config{URL: args[0]}

	fs := flag.NewFlagSet("retry", flag.ContinueOnError)
	fs.StringVar(&cfg.Method, "method", "GET", "HTTP method")
	fs.IntVar(&cfg.MaxAttempts, "max-attempts", 5, "maximum number of attempts")
	fs.StringVar(&cfg.Key, "idempotency-key", "", "idempotency key")

	if err := fs.Parse(args[1:]); err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}

	cfg.Method = strings.ToUpper(strings.TrimSpace(cfg.Method))

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func validateConfig(cfg Config) error {
	if cfg.URL == "" {
		return errors.New("URL is empty")
	}
	if cfg.Method == "" {
		return errors.New("method is empty")
	}
	if !validMethod(cfg.Method) {
		return fmt.Errorf("invalid method %q", cfg.Method)
	}

	u, err := url.Parse(cfg.URL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", cfg.URL, err)
	}
	if u.Scheme != "http" {
		return fmt.Errorf("unsupported scheme %q: only http is allowed", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL %q has no host", cfg.URL)
	}

	if cfg.MaxAttempts < 1 {
		return fmt.Errorf("max-attempts must be >= 1, got %d", cfg.MaxAttempts)
	}
	return nil
}

func validMethod(method string) bool {
	if method == "" {
		return false
	}
	for i := 0; i < len(method); i++ {
		c := method[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case strings.IndexByte(tokenChars, c) >= 0:
		default:
			return false
		}
	}
	return true
}
