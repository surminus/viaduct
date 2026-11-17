// This example uses viaduct as a library in a program with its own flags. It
// applies a manifest with Apply, sends events to log/slog through a custom
// handler, and records each result as it finishes.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/surminus/viaduct"
	"github.com/surminus/viaduct/resources"
)

// slogHandler sends viaduct events to a slog.Logger, with the resource kind,
// operation and ID as fields.
type slogHandler struct {
	log *slog.Logger
}

func (h slogHandler) Handle(ctx context.Context, e viaduct.Event) {
	level := slog.LevelInfo

	switch e.Level {
	case viaduct.LevelOutput, viaduct.LevelNoop:
		level = slog.LevelDebug
	case viaduct.LevelWarn:
		level = slog.LevelWarn
	case viaduct.LevelError:
		level = slog.LevelError
	}

	attrs := []any{"kind", string(e.Kind), "op", e.Operation}
	if e.ResourceID != "" {
		attrs = append(attrs, "id", string(e.ResourceID))
	}

	for _, f := range e.Fields {
		attrs = append(attrs, f.Key, f.Value)
	}

	h.log.Log(ctx, level, e.Message, attrs...)
}

func main() {
	dryRun := flag.Bool("dry-run", false, "report changes without making them")
	verbose := flag.Bool("verbose", false, "log noops and command output")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	m := viaduct.New()

	dir := m.AddNamed("dir", resources.Dir("/tmp/viaduct-library"))
	file := m.AddNamed("file", resources.CreateFile("/tmp/viaduct-library/hello", "hello\n"), dir)

	// A step that may fail at first, retried with backoff
	ready := m.AddNamed("ready", resources.Exec("test -f /tmp/viaduct-library/hello"), file)
	m.WithRetry(ready, viaduct.RetryPolicy{Attempts: 3, Initial: time.Second})

	// Something that is nice to have: a failure is reported, but does not
	// fail the run
	m.AllowFailure(m.AddNamed("tuning", resources.Exec("test -w /proc/sys/vm/swappiness"), file))

	// Ctrl-C or SIGTERM cancels the run, stopping the resources that are
	// running and leaving the rest unstarted
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := m.Apply(ctx, viaduct.Options{
		Env:     viaduct.Env{DryRun: *dryRun},
		Handler: slogHandler{log: log},
		OnResult: func(r viaduct.Result) {
			log.Info("result",
				"id", string(r.ID),
				"status", string(r.Status),
				"changed", r.Changed,
				"attempt", r.Attempt,
				"final", r.Final,
				"allow_failure", r.AllowFailure,
				"dry_run", *dryRun,
				"duration", r.Duration,
			)
		},
	})

	switch {
	case errors.Is(err, viaduct.ErrRunFailed):
		for _, r := range report.Failed() {
			log.Error("failed", "id", string(r.ID), "error", r.Err)
		}

		os.Exit(1)
	case err != nil:
		log.Error("could not run", "error", err)
		os.Exit(1)
	}
}
