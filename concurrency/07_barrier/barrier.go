package barrier

import (
	"math"
	"primitives/internal/futex"
	"sync/atomic"
)

type Barrier struct {
	need    uint32
	arrived uint32
	round   uint32
	state   uint32
}

func New(n int) *Barrier {
	if n <= 0 {
		panic("barrier: n must be positive")
	}

	if n > math.MaxUint32 {
		panic("barrier: n exceeds uint32")
	}

	return &Barrier{
		need:    uint32(n),
		arrived: 0,
		round:   0,
		state:   0,
	}
}

func (b *Barrier) Wait() {
	for {
		if atomic.CompareAndSwapUint32(&b.state, 0, 1) {
			break
		}

		futex.Wait(&b.state, 1)
	}

	round := atomic.LoadUint32(&b.round)
	b.arrived++
	last := b.arrived == b.need

	if last {
		b.arrived = 0
		atomic.AddUint32(&b.round, 1)
	}

	atomic.StoreUint32(&b.state, 0)
	futex.Wake(&b.state)

	if last {
		futex.WakeAll(&b.round)
		return
	}

	for atomic.LoadUint32(&b.round) == round {
		futex.Wait(&b.round, round)
	}
}
