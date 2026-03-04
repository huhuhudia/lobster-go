package bus

import (
	"context"
	"sync"
)

// MessageBus is an in-memory queue for inbound/outbound traffic.
type MessageBus struct {
	inbound  chan InboundMessage
	outbound chan OutboundMessage
	once     sync.Once
	closed   chan struct{}
}

// New creates a MessageBus with buffered channels.
func New(buffer int) *MessageBus {
	if buffer <= 0 {
		buffer = 100
	}
	return &MessageBus{
		inbound:  make(chan InboundMessage, buffer),
		outbound: make(chan OutboundMessage, buffer),
		closed:   make(chan struct{}),
	}
}

// PublishInbound pushes a message into the inbound queue.
func (b *MessageBus) PublishInbound(msg InboundMessage) error {
	select {
	case <-b.closed:
		return ErrClosed
	default:
	}
	select {
	case b.inbound <- msg:
		return nil
	case <-b.closed:
		return ErrClosed
	}
}

// ConsumeInbound blocks until an inbound message arrives or ctx cancels.
func (b *MessageBus) ConsumeInbound(ctx context.Context) (InboundMessage, error) {
	select {
	case <-b.closed:
		return InboundMessage{}, ErrClosed
	case msg := <-b.inbound:
		return msg, nil
	case <-ctx.Done():
		return InboundMessage{}, ctx.Err()
	}
}

// PublishOutbound pushes a message into the outbound queue.
func (b *MessageBus) PublishOutbound(msg OutboundMessage) error {
	select {
	case <-b.closed:
		return ErrClosed
	default:
	}
	select {
	case b.outbound <- msg:
		return nil
	case <-b.closed:
		return ErrClosed
	}
}

// ConsumeOutbound blocks until an outbound message arrives or ctx cancels.
func (b *MessageBus) ConsumeOutbound(ctx context.Context) (OutboundMessage, error) {
	select {
	case <-b.closed:
		return OutboundMessage{}, ErrClosed
	case msg := <-b.outbound:
		return msg, nil
	case <-ctx.Done():
		return OutboundMessage{}, ctx.Err()
	}
}

// Close signals both queues to stop; further publishes will fail.
func (b *MessageBus) Close() {
	b.once.Do(func() {
		close(b.closed)
	})
}

// InboundSize returns approximate buffered inbound count.
func (b *MessageBus) InboundSize() int { return len(b.inbound) }

// OutboundSize returns approximate buffered outbound count.
func (b *MessageBus) OutboundSize() int { return len(b.outbound) }

// ErrClosed indicates the bus has been closed.
var ErrClosed = context.Canceled
