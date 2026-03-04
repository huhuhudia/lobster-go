package cron

import (
	"context"
	"sync"
	"time"

	"github.com/huhuhudia/lobster-go/pkg/logging"
)

// Job represents a scheduled action.
type Job struct {
	Name     string
	Interval time.Duration
	Task     func(context.Context)
}

// Service runs jobs on intervals.
type Service struct {
	jobs   []Job
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a service with jobs.
func New(jobs []Job) *Service {
	return &Service{jobs: jobs}
}

// Start begins all job loops.
func (s *Service) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	for _, job := range s.jobs {
		j := job
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(j.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if j.Task != nil {
						j.Task(ctx)
					}
				}
			}
		}()
	}
}

// Stop cancels all jobs.
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

// AddJob appends a job (no running jobs are started for the new entry).
func (s *Service) AddJob(job Job) {
	s.jobs = append(s.jobs, job)
}

// RunOnce is a helper to execute all jobs immediately (used in tests).
func (s *Service) RunOnce(ctx context.Context) {
	for _, j := range s.jobs {
		if j.Task != nil {
			j.Task(ctx)
		}
	}
}

func LogTask(name string) func(context.Context) {
	return func(ctx context.Context) {
		logging.Default.Info("cron: running %s", name)
	}
}
