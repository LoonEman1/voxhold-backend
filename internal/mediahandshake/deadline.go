// Package mediahandshake bounds initial negotiation without timing out viewers
// that are still waiting for a publisher to produce their first media track.
package mediahandshake

import (
	"sync"
	"time"
)

const (
	AnswerTimeout  = 20 * time.Second
	ConnectTimeout = 30 * time.Second
)

type Deadline struct {
	mu     sync.Mutex
	stage  uint8 // 0 waiting for offer, 1 answer, 2 connection, 3 finished
	timer  *time.Timer
	expire func(string)
}

func New(expire func(string)) *Deadline { return &Deadline{expire: expire} }

func (d *Deadline) Offer() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stage != 0 {
		return
	}
	d.stage = 1
	d.arm(1, AnswerTimeout, "initial WebRTC answer timed out")
}

func (d *Deadline) Answer() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stage != 1 {
		return
	}
	d.timer.Stop()
	d.stage = 2
	d.arm(2, ConnectTimeout, "initial WebRTC connection timed out")
}

// Called with mu held; stage checks invalidate a timer already firing when the
// next stage, successful connection, or close cancels it.
func (d *Deadline) arm(stage uint8, timeout time.Duration, reason string) {
	d.timer = time.AfterFunc(timeout, func() {
		d.mu.Lock()
		if d.stage != stage {
			d.mu.Unlock()
			return
		}
		d.stage = 3
		d.mu.Unlock()
		d.expire(reason)
	})
}

func (d *Deadline) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stage = 3
	if d.timer != nil {
		d.timer.Stop()
	}
}
