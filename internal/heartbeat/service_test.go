package heartbeat

import (
	"context"
	"testing"
	"time"

	"github.com/huhuhudia/lobster-go/internal/bus"
)

func TestHeartbeatPublishes(t *testing.T) {
	b := bus.New(1)
	svc := Service{Bus: b, Interval: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	go svc.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	cancel()
	_, err := b.ConsumeOutbound(context.Background())
	if err != nil {
		t.Fatalf("expected heartbeat message")
	}
}
