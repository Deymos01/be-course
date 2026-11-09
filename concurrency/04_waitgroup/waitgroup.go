package waitgroup

import (
	"primitives/internal/futex"
	"sync/atomic"
)

type WaitGroup struct {
	count uint32
}

func (wg *WaitGroup) Add(delta int) {
	if delta == 0 {
		return
	}

	for {
		prev := atomic.LoadUint32(&wg.count)

		var next uint32

		if delta > 0 {
			next = prev + uint32(delta)
		} else {
			if prev < uint32(-delta) {
				panic("negative WaitGroup counter")
			}

			next = prev - uint32(-delta)
		}

		if atomic.CompareAndSwapUint32(&wg.count, prev, next) {
			if next == 0 {
				futex.WakeAll(&wg.count)
			}

			return
		}
	}
}

func (wg *WaitGroup) Done() {
	wg.Add(-1)
}

func (wg *WaitGroup) Wait() {
	for {
		count := atomic.LoadUint32(&wg.count)

		if count == 0 {
			return
		}

		futex.Wait(&wg.count, count)
	}
}
