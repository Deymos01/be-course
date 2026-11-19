package report

import (
	"fmt"
	"io"
	"os"
	"time"
)

type Reporter struct{ W io.Writer }

func New(w io.Writer) Reporter {
	if w == nil {
		w = os.Stdout
	}
	return Reporter{W: w}
}

func (r Reporter) Attempt(n int, status int, err error) {
	if err != nil {
		fmt.Fprintf(r.W, "attempt %d error %s\n", n, err)
	} else {
		fmt.Fprintf(r.W, "attempt %d status %d\n", n, status)
	}
}

func (r Reporter) Sleep(d time.Duration) {
	fmt.Fprintf(r.W, "sleep_ms %d\n", d.Milliseconds())
}

func (r Reporter) Result(ok bool, attempts int) {
	if ok {
		fmt.Fprintf(r.W, "result success attempts %d\n", attempts)
	} else {
		fmt.Fprintf(r.W, "result failure attempts %d\n", attempts)
	}
}
