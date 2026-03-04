package heartbeat

import (
	"context"
	"time"

	"github.com/huhuhudia/lobster-go/internal/bus"
)

// Service periodically publishes heartbeat messages.
type Service struct {
	Bus     *bus.MessageBus
	Interval time.Duration
}

func (s *Service) Start(ctx context.Context) {
	if s.Bus == nil {
		return
	}
	interval := s.Interval
	if interval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Bus.PublishOutbound(bus.OutboundMessage{
				Channel: "heartbeat",
				ChatID:  "system",
				Content: "heartbeat",
			})
		}
	}
}
