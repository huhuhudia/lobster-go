package channels

import (
	"context"
	"errors"
	"sync"

	"github.com/huhuhudia/lobster-go/internal/bus"
)

// MockConfig configures the lightweight mock channel.
type MockConfig struct {
	Enabled   bool
	Name      string
	AllowFrom []string
}

// MockChannel is a simple in-process adapter for local integration tests.
type MockChannel struct {
	BaseChannel
	Config MockConfig

	mu   sync.Mutex
	sent []bus.OutboundMessage
}

func NewMockChannel(cfg MockConfig, b *bus.MessageBus) *MockChannel {
	name := cfg.Name
	if name == "" {
		name = "mock"
	}
	return &MockChannel{
		BaseChannel: BaseChannel{
			Name: name,
			Bus:  b,
		},
		Config: cfg,
		sent:   make([]bus.OutboundMessage, 0, 8),
	}
}

func (m *MockChannel) Start(ctx context.Context) error {
	if !m.Config.Enabled {
		return nil
	}
	m.Running = true
	<-ctx.Done()
	m.Running = false
	return nil
}

func (m *MockChannel) Stop() error {
	m.Running = false
	return nil
}

func (m *MockChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !m.Running {
		return errors.New("mock channel not running")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

// InjectInbound simulates an incoming message from the channel side.
func (m *MockChannel) InjectInbound(ctx context.Context, senderID, chatID, content string) error {
	if len(m.Config.AllowFrom) > 0 && !m.IsAllowed(m.Config.AllowFrom, senderID) {
		return nil
	}
	return m.PublishInbound(ctx, senderID, chatID, content, nil, nil)
}

// SentMessages returns a copy of captured outbound messages.
func (m *MockChannel) SentMessages() []bus.OutboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]bus.OutboundMessage, len(m.sent))
	copy(out, m.sent)
	return out
}
