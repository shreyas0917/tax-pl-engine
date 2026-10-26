// Command api serves the REST API.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shreyas0917/taxpl/internal/app"
	"github.com/shreyas0917/taxpl/internal/httpapi"
	"github.com/shreyas0917/taxpl/internal/jobs"
	_ "github.com/shreyas0917/taxpl/internal/parser/all"
)

func main() {
	if err := run(); err != nil {
		println("api: " + err.Error())
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
	if err := jobs.Migrate(ctx, a.Pool); err != nil {
		return fmt.Errorf("river migrate: %w", err)
	}
	client, err := jobs.NewInsertOnlyClient(a.Pool)
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}
	a.Jobs.River = client

	srv := httpapi.New(a.Cfg, a.Log, a.Pool, a.Blob, a.Jobs, a.Redis)
	httpServer := &http.Server{Addr: a.Cfg.HTTPAddr, Handler: srv.Router(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 15 * time.Minute, IdleTimeout: time.Minute}
	go func() {
		a.Log.Info("api listening", "addr", a.Cfg.HTTPAddr, "env", a.Cfg.AppEnv, "blob", a.Cfg.BlobDriver)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.Log.Error("listen", "err", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	a.Log.Info("api stopped")
	return nil
}
