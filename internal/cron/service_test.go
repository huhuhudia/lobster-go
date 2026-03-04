package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunOnce(t *testing.T) {
	var count int32
	j := Job{
		Name:     "test",
		Interval: 10 * time.Second,
		Task: func(ctx context.Context) {
			atomic.AddInt32(&count, 1)
		},
	}
	s := New([]Job{j})
	s.RunOnce(context.Background())
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}
}

func TestStartStop(t *testing.T) {
	var count int32
	j := Job{
		Name:     "tick",
		Interval: 20 * time.Millisecond,
		Task: func(ctx context.Context) {
			atomic.AddInt32(&count, 1)
		},
	}
	s := New([]Job{j})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	s.Start(ctx)
	time.Sleep(70 * time.Millisecond)
	s.Stop()
	if atomic.LoadInt32(&count) == 0 {
		t.Fatalf("expected job to run at least once")
	}
}
