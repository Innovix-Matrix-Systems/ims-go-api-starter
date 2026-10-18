package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/cache/memory"
)

func testRunner(t *testing.T, registry *Registry) (*Runner, *memory.Store) {
	t.Helper()
	store := memory.New("sched-")
	t.Cleanup(store.Close)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner, err := NewRunner(registry, logger, store, "UTC")
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	return runner, store
}

func TestRegistryValidation(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(Task{ID: "", Spec: "* * * * *", Run: noopRun}); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := registry.Register(Task{ID: "x", Spec: "not-a-cron", Run: noopRun}); err == nil {
		t.Fatal("invalid spec accepted")
	}
	if err := registry.Register(Task{ID: "x", Spec: "* * * * *", Run: nil}); err == nil {
		t.Fatal("missing handler accepted")
	}
	if err := registry.Register(Task{ID: "x", Spec: "* * * * *", Run: noopRun}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(Task{ID: "x", Spec: "* * * * *", Run: noopRun}); err == nil {
		t.Fatal("duplicate id accepted")
	}
}

func TestRunNowUnknownTask(t *testing.T) {
	runner, _ := testRunner(t, NewRegistry())
	if err := runner.RunNow(context.Background(), "missing"); err == nil {
		t.Fatal("RunNow(unknown) = nil error")
	}
}

func TestOverlapSkip(t *testing.T) {
	registry := NewRegistry()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var runs int32

	if err := registry.Register(Task{
		ID:      "slow",
		Spec:    "* * * * *",
		Overlap: OverlapSkip,
		Run: func(ctx context.Context) error {
			atomic.AddInt32(&runs, 1)
			started <- struct{}{}
			<-release
			return nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	runner, _ := testRunner(t, registry)
	go runner.Execute(context.Background(), mustTask(t, registry, "slow"))

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first run did not start")
	}

	// Second execution while the first is running must be skipped.
	runner.Execute(context.Background(), mustTask(t, registry, "slow"))
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("runs = %d, want 1 (overlap skipped)", got)
	}

	close(release)
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("runs after release = %d, want 1", got)
	}
}

func TestHeartbeatAndHealth(t *testing.T) {
	runner, store := testRunner(t, NewRegistry())
	ctx := context.Background()

	check := HealthCheck{Store: store}
	if result := check.Run(ctx); result.Status != "warning" {
		t.Fatalf("missing heartbeat status = %q, want warning", result.Status)
	}

	runner.Heartbeat(ctx)
	if result := check.Run(ctx); result.Status != "ok" {
		t.Fatalf("fresh heartbeat status = %q, want ok", result.Status)
	}

	// Stale heartbeat.
	_ = store.Set(ctx, HeartbeatKey, time.Now().Add(-10*time.Minute).Unix(), 0)
	if result := check.Run(ctx); result.Status != "warning" {
		t.Fatalf("stale heartbeat status = %q, want warning", result.Status)
	}
}

func TestRunNowPropagatesError(t *testing.T) {
	registry := NewRegistry()
	wantErr := errors.New("task failed")
	if err := registry.Register(Task{
		ID:   "failing",
		Spec: "* * * * *",
		Run: func(ctx context.Context) error {
			return wantErr
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	runner, _ := testRunner(t, registry)
	if err := runner.RunNow(context.Background(), "failing"); !errors.Is(err, wantErr) {
		t.Fatalf("RunNow() error = %v, want %v", err, wantErr)
	}
}

func mustTask(t *testing.T, registry *Registry, id string) Task {
	t.Helper()
	task, ok := registry.Get(id)
	if !ok {
		t.Fatalf("task %q not found", id)
	}
	return task
}

func noopRun(ctx context.Context) error { return nil }
