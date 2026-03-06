package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/huhuhudia/lobster-go/internal/bus"
	"github.com/huhuhudia/lobster-go/internal/channels"
)

func TestGatewayDispatchesOutbound(t *testing.T) {
	b := bus.New(10)
	mock := channels.NewMockChannel(channels.MockConfig{Enabled: true, Name: "mock"}, b)
	svc := &Service{
		Bus:      b,
		Channels: map[string]channels.Channel{"mock": mock},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = svc.Run(ctx)
	}()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if mock.Running {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !mock.Running {
		t.Fatalf("mock channel not running")
	}

	if err := b.PublishOutbound(bus.OutboundMessage{
		Channel: "mock",
		ChatID:  "c1",
		Content: "hello",
	}); err != nil {
		t.Fatalf("publish outbound: %v", err)
	}

	deadline = time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(mock.SentMessages()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(mock.SentMessages()) != 1 {
		t.Fatalf("expected 1 outbound message, got %d", len(mock.SentMessages()))
	}
}
