package kicker

import "sync"

type Kicker struct {
	sync.Mutex
	f       func()
	running bool
	kicked  bool
}

func New(f func()) *Kicker {
	return &Kicker{
		f: f,
	}
}

func (k *Kicker) Kick() {
	k.Lock()
	defer k.Unlock()

	if k.running {
		k.kicked = true
		return
	}

	k.running = true
	go k.run()
}

func (k *Kicker) run() {
	k.f()
	k.Lock()
	defer k.Unlock()

	if k.kicked {
		k.kicked = false
		go k.run()
	} else {
		k.running = false
	}
}
