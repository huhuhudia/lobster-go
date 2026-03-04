package channels

import (
	"context"

	"github.com/huhuhudia/lobster-go/internal/bus"
)

// BaseChannel provides common functionality for all channel implementations.
type BaseChannel struct {
	Name    string
	Bus     *bus.MessageBus
	Running bool
}

// IsAllowed checks if senderID is in the allow list.
// Empty list denies all; "*" allows all.
func (b *BaseChannel) IsAllowed(allowFrom []string, senderID string) bool {
	if len(allowFrom) == 0 {
		return false
	}
	for _, v := range allowFrom {
		if v == "*" {
			return true
		}
		if v == senderID {
			return true
		}
	}
	return false
}

// PublishInbound forwards a message to the bus.
func (b *BaseChannel) PublishInbound(ctx context.Context, senderID, chatID, content string, media []string, metadata map[string]string) error {
	return b.Bus.PublishInbound(bus.InboundMessage{
		Channel:   b.Name,
		SenderID:  senderID,
		ChatID:    chatID,
		Content:   content,
		Media:     media,
		Metadata:  metadata,
	})
}

// Channel interface that all adapters must implement.
type Channel interface {
	Start(ctx context.Context) error
	Stop() error
	Send(ctx context.Context, msg bus.OutboundMessage) error
}