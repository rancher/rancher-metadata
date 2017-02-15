package kicker

import "sync"

type Kicker struct {
	sync.Mutex
	f          func()
	running    bool
	kicked     bool
	generation int
	cond       *sync.Cond
}

func New(f func()) *Kicker {
	return &Kicker{
		f:    f,
		cond: sync.NewCond(&sync.Mutex{}),
	}
}

func (k *Kicker) Kick() int {
	k.Lock()
	defer k.Unlock()

	k.cond.L.Lock()
	v := k.generation
	k.cond.L.Unlock()

	if k.running {
		k.kicked = true
		return v
	}

	k.running = true
	go k.run()

	return v
}

func (k *Kicker) Wait(v int) {
	k.cond.L.Lock()
	for {
		if v < k.generation {
			break
		}
		k.cond.Wait()
	}
	k.cond.L.Unlock()
}

func (k *Kicker) run() {
	k.f()

	k.cond.L.Lock()
	k.generation++
	k.cond.Broadcast()
	k.cond.L.Unlock()

	k.Lock()
	defer k.Unlock()

	if k.kicked {
		k.kicked = false
		go k.run()
	} else {
		k.running = false
	}
}
