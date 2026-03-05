package channels

import (
	"context"
	"testing"
	"time"

	"github.com/huhuhudia/lobster-go/internal/bus"
)

func TestMockChannelInjectInbound(t *testing.T) {
	b := bus.New(2)
	ch := NewMockChannel(MockConfig{Enabled: true}, b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ch.Start(ctx)

	if err := ch.InjectInbound(context.Background(), "u1", "c1", "hello"); err != nil {
		t.Fatalf("inject inbound: %v", err)
	}
	msg, err := b.ConsumeInbound(context.Background())
	if err != nil {
		t.Fatalf("consume inbound: %v", err)
	}
	if msg.Channel != "mock" || msg.Content != "hello" {
		t.Fatalf("unexpected inbound: %+v", msg)
	}
}

func TestMockChannelSendCapture(t *testing.T) {
	b := bus.New(2)
	ch := NewMockChannel(MockConfig{Enabled: true}, b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ch.Start(ctx)
	time.Sleep(10 * time.Millisecond)

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "mock",
		ChatID:  "c1",
		Content: "pong",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	got := ch.SentMessages()
	if len(got) != 1 || got[0].Content != "pong" {
		t.Fatalf("unexpected sent messages: %+v", got)
	}
}

func TestMockChannelAllowFrom(t *testing.T) {
	b := bus.New(2)
	ch := NewMockChannel(MockConfig{
		Enabled:   true,
		AllowFrom: []string{"u-ok"},
	}, b)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ch.Start(ctx)

	if err := ch.InjectInbound(context.Background(), "u-deny", "c1", "hello"); err != nil {
		t.Fatalf("inject inbound: %v", err)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer waitCancel()
	if _, err := b.ConsumeInbound(waitCtx); err == nil {
		t.Fatalf("expected no inbound message for denied sender")
	}
}
