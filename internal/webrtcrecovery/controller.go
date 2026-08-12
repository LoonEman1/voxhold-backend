package webrtcrecovery

import (
	"sync"
	"time"
)

type Policy struct {
	InitialDelay time.Duration
	RetryDelay   time.Duration
	MaxAttempts  int
}

type Controller struct {
	policy Policy

	mu     sync.Mutex
	cancel chan struct{}
}

func New(policy Policy) *Controller {
	if policy.InitialDelay < 0 {
		policy.InitialDelay = 0
	}
	if policy.RetryDelay <= 0 {
		policy.RetryDelay = time.Second
	}
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	return &Controller{policy: policy}
}

func (c *Controller) Start(
	immediate bool,
	attempt func() error,
	exhausted func(),
) {
	if attempt == nil || exhausted == nil {
		return
	}

	c.mu.Lock()
	if c.cancel != nil {
		if !immediate {
			c.mu.Unlock()
			return
		}
		close(c.cancel)
	}
	cancel := make(chan struct{})
	c.cancel = cancel
	c.mu.Unlock()

	delay := c.policy.InitialDelay
	if immediate {
		delay = 0
	}
	go c.run(cancel, delay, attempt, exhausted)
}

func (c *Controller) Stop() {
	c.mu.Lock()
	if c.cancel != nil {
		close(c.cancel)
		c.cancel = nil
	}
	c.mu.Unlock()
}

func (c *Controller) run(
	cancel <-chan struct{},
	delay time.Duration,
	attempt func() error,
	exhausted func(),
) {
	if !wait(cancel, delay) {
		return
	}
	for index := 0; index < c.policy.MaxAttempts; index++ {
		select {
		case <-cancel:
			return
		default:
		}

		_ = attempt()
		if !wait(cancel, c.policy.RetryDelay) {

			return
		}
	}

	select {
	case <-cancel:
		return
	default:
	}
	exhausted()

	c.mu.Lock()
	if c.cancel == cancel {
		c.cancel = nil
	}
	c.mu.Unlock()
}

func wait(cancel <-chan struct{}, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-cancel:
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-cancel:
		return false
	}
}
