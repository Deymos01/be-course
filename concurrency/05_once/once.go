package once

import (
	"primitives/internal/futex"
	"sync/atomic"
)

const (
	notStarted uint32 = iota
	inProgress
	done
)

type Once struct {
	state uint32
}

func (o *Once) Do(f func()) {
	for {
		state := atomic.LoadUint32(&o.state)

		switch state {
		case done:
			return
		case inProgress:
			futex.Wait(&o.state, inProgress)
		case notStarted:
			if !atomic.CompareAndSwapUint32(&o.state, notStarted, inProgress) {
				continue
			}
			defer func() {
				atomic.StoreUint32(&o.state, done)
				futex.WakeAll(&o.state)
			}()

			f()
			return
		default:
			panic("invalid state")
		}
	}
}

func (o *Once) Done() bool {
	return atomic.LoadUint32(&o.state) == done
}
