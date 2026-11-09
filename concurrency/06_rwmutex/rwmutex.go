package rwmutex

import (
	"primitives/internal/futex"
	"sync/atomic"
)

const writer = 1 << 31

type RWMutex struct {
	state uint32
}

func (rw *RWMutex) RLock() {
	for {
		state := atomic.LoadUint32(&rw.state)

		if state&writer != 0 {
			futex.Wait(&rw.state, state)
			continue
		}

		if atomic.CompareAndSwapUint32(&rw.state, state, state+1) {
			return
		}
	}
}

func (rw *RWMutex) RUnlock() {
	for {
		state := atomic.LoadUint32(&rw.state)

		if state&writer != 0 || state == 0 {
			panic("RUnlock of unlocked RWMutex")
		}

		next := state - 1

		if atomic.CompareAndSwapUint32(&rw.state, state, next) {
			if next == 0 {
				futex.WakeAll(&rw.state)
			}
			return
		}
	}
}

func (rw *RWMutex) Lock() {
	for {
		if atomic.CompareAndSwapUint32(&rw.state, 0, writer) {
			return
		}

		state := atomic.LoadUint32(&rw.state)

		if state == 0 {
			continue
		}

		futex.Wait(&rw.state, state)
	}
}

func (rw *RWMutex) Unlock() {
	if !atomic.CompareAndSwapUint32(&rw.state, writer, 0) {
		panic("Unlock of unlocked RWMutex")
	}

	futex.WakeAll(&rw.state)
}
