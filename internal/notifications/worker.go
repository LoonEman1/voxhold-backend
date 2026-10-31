package notifications

import (
	"context"
	"log"
	"time"
	"voxhold-backend/internal/message"
)

// Enqueue is called by producers until HTTP and scheduled delivery are stopped.
// Saturation applies backpressure rather than silently discarding alerts.
func (s *Service) Enqueue(value message.Message) {
	select {
	case s.queue <- value:
	default:
		if err := s.Deliver(value); err != nil {
			log.Printf("deliver message notification: %v", err)
		}
	}
}

func (s *Service) Run(ctx context.Context) {
	work, cancel := context.WithCancel(context.Background())
	drained := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(drained)
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			cancel()
		case <-work.Done():
		}
	})
	defer func() {
		cancel()
		if !stop() {
			<-drained
		}
	}()
	deliver := func(value message.Message) {
		if err := s.deliver(work, value); err != nil {
			log.Printf("deliver message notification: %v", err)
		}
	}
	for work.Err() == nil {
		select {
		case value := <-s.queue:
			deliver(value)
		case <-ctx.Done():
			for work.Err() == nil {
				select {
				case value := <-s.queue:
					deliver(value)
				default:
					return
				}
			}
			return
		}
	}
}
