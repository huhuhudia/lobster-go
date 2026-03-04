package bus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSessionKey(t *testing.T) {
	msg := InboundMessage{Channel: "tg", ChatID: "123"}
	if got := msg.SessionKey(); got != "tg:123" {
		t.Fatalf("session key mismatch: %s", got)
	}
	msg.SessionKeyOverride = "custom"
	if got := msg.SessionKey(); got != "custom" {
		t.Fatalf("override session key mismatch: %s", got)
	}
}

func TestPublishConsumeInbound(t *testing.T) {
	bus := New(1)
	defer bus.Close()

	in := InboundMessage{Channel: "tg", ChatID: "1", Content: "hi"}
	if err := bus.PublishInbound(in); err != nil {
		t.Fatalf("publish inbound: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := bus.ConsumeInbound(ctx)
	if err != nil {
		t.Fatalf("consume inbound: %v", err)
	}
	if got.Content != "hi" {
		t.Fatalf("content mismatch: %s", got.Content)
	}
}

func TestPublishConsumeOutbound(t *testing.T) {
	bus := New(1)
	defer bus.Close()

	out := OutboundMessage{Channel: "tg", ChatID: "1", Content: "pong"}
	if err := bus.PublishOutbound(out); err != nil {
		t.Fatalf("publish outbound: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := bus.ConsumeOutbound(ctx)
	if err != nil {
		t.Fatalf("consume outbound: %v", err)
	}
	if got.Content != "pong" {
		t.Fatalf("content mismatch: %s", got.Content)
	}
}

func TestConcurrentPublishConsume(t *testing.T) {
	bus := New(10)
	defer bus.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_ = bus.PublishInbound(InboundMessage{Channel: "tg", ChatID: "c", Content: "msg"})
			_ = bus.PublishOutbound(OutboundMessage{Channel: "tg", ChatID: "c", Content: "msg"})
		}(i)
	}
	wg.Wait()

	// Drain and count
	inCount := 0
	for i := 0; i < 5; i++ {
		if _, err := bus.ConsumeInbound(ctx); err != nil {
			t.Fatalf("consume inbound: %v", err)
		}
		inCount++
	}
	outCount := 0
	for i := 0; i < 5; i++ {
		if _, err := bus.ConsumeOutbound(ctx); err != nil {
			t.Fatalf("consume outbound: %v", err)
		}
		outCount++
	}
	if inCount != 5 || outCount != 5 {
		t.Fatalf("unexpected counts in=%d out=%d", inCount, outCount)
	}
}

func TestClosePreventsPublish(t *testing.T) {
	bus := New(1)
	bus.Close()
	if err := bus.PublishInbound(InboundMessage{}); err == nil {
		t.Fatalf("expected error after close")
	}
}
