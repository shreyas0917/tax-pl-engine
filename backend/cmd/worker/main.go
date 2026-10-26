// Command worker runs the River job workers: upload parsing, recompute, reports, purge.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shreyas0917/taxpl/internal/app"
	"github.com/shreyas0917/taxpl/internal/jobs"
	_ "github.com/shreyas0917/taxpl/internal/parser/all"
)

func main() {
	if err := run(); err != nil {
		println("worker: " + err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := jobs.Migrate(ctx, a.Sys); err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	client, err := jobs.NewWorkerClient(a.Pool, a.Jobs, a.Cfg.WorkerCount, a.Log)
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}
	if err := client.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}
	a.Log.Info("worker started", "workers", a.Cfg.WorkerCount, "raw_file_ttl", a.Cfg.RawFileTTL.String())
	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = client.Stop(stopCtx)
	a.Log.Info("worker stopped")
	return nil
}
