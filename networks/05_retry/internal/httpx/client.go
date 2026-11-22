package httpx

import (
	"io"
	"net/http"
	"time"
)

type Outcome struct {
	Status     int
	Err        error
	RetryAfter string
	Local      bool
}

func (o Outcome) Success() bool {
	return o.Err == nil && o.Status >= 200 && o.Status < 400
}

type Client struct {
	url    string
	method string
	key    string
	inner  *http.Client
}

func New(URL, method, key string) *Client {
	return &Client{
		url:    URL,
		method: method,
		key:    key,
		inner: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
			Transport: &http.Transport{DisableKeepAlives: true},
		},
	}
}

func (c *Client) Do() Outcome {
	req, err := http.NewRequest(c.method, c.url, nil)
	if err != nil {
		return Outcome{Err: err, Local: true}
	}
	if c.key != "" {
		req.Header.Set("Idempotency-Key", c.key)
	}
	resp, err := c.inner.Do(req)
	if err != nil {
		return Outcome{Err: err}
	}
	defer resp.Body.Close()

	retryAfter := resp.Header.Get("Retry-After")

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return Outcome{Err: err, RetryAfter: retryAfter}
	}

	return Outcome{
		Status:     resp.StatusCode,
		RetryAfter: retryAfter,
	}
}
