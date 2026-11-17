package main

import (
	"fmt"
	"networks/05_retry/internal/config"
	"networks/05_retry/internal/httpx"
	"networks/05_retry/internal/report"
	"networks/05_retry/internal/retry"
	"os"
)

func main() {
	cfg, err := config.ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	r := report.New(os.Stdout)
	c := httpx.New(cfg.URL, cfg.Method, cfg.Key)
	ok, attempts := retry.Run(cfg, c, r)
	r.Result(ok, attempts)

	if !ok {
		os.Exit(1)
	}
}
