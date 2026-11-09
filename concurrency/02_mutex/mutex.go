package mutex

import (
	"primitives/internal/futex"
	"sync/atomic"
)

const (
	free = iota
	held
	contended
)

type Mutex struct {
	state uint32
}

const spinCount = 32

func (m *Mutex) Lock() {
	if atomic.CompareAndSwapUint32(&m.state, free, held) {
		return
	}

	for range spinCount {
		if atomic.LoadUint32(&m.state) == free && atomic.CompareAndSwapUint32(&m.state, free, held) {
			return
		}
	}

	for {
		if atomic.SwapUint32(&m.state, contended) == free {
			return
		}

		futex.Wait(&m.state, contended)
	}
}

func (m *Mutex) TryLock() bool {
	return atomic.CompareAndSwapUint32(&m.state, free, held)
}

func (m *Mutex) Unlock() {
	prev := atomic.SwapUint32(&m.state, free)

	switch prev {
	case held:
		return
	case contended:
		futex.Wake(&m.state)
	case free:
		panic("unlock of unlocked mutex")
	default:
		panic("invalid mutex state")
	}
}
