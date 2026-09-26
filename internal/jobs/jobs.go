// Package jobs runs periodic background tasks (usage flushing, partition
// maintenance, retention) with jitter-free fixed intervals.
package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Task is one periodic job.
type Task struct {
	Name     string
	Interval time.Duration
	// RunAtStart runs the task once immediately.
	RunAtStart bool
	Run        func(ctx context.Context) error
}

// Runner executes tasks until its context ends.
type Runner struct {
	log   *slog.Logger
	tasks []Task
	wg    sync.WaitGroup
}

// NewRunner creates a runner.
func NewRunner(log *slog.Logger, tasks ...Task) *Runner {
	return &Runner{log: log, tasks: tasks}
}

// Start launches every task in its own goroutine.
func (r *Runner) Start(ctx context.Context) {
	for _, t := range r.tasks {
		r.wg.Add(1)
		go r.loop(ctx, t)
	}
}

// Wait blocks until all task loops returned (after ctx is cancelled).
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) loop(ctx context.Context, t Task) {
	defer r.wg.Done()
	if t.RunAtStart {
		r.run(ctx, t)
	}
	tick := time.NewTicker(t.Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.run(ctx, t)
		}
	}
}

func (r *Runner) run(ctx context.Context, t Task) {
	defer func() {
		if rec := recover(); rec != nil {
			r.log.Error("background task panicked", slog.String("task", t.Name), slog.Any("panic", rec))
		}
	}()
	start := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, max(t.Interval, time.Minute))
	defer cancel()
	if err := t.Run(runCtx); err != nil && ctx.Err() == nil {
		r.log.Warn("background task failed", slog.String("task", t.Name), slog.Any("err", err), slog.Duration("took", time.Since(start)))
		return
	}
	r.log.Debug("background task done", slog.String("task", t.Name), slog.Duration("took", time.Since(start)))
}
