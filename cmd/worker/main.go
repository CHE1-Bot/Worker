package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/che1/worker/internal/config"
	"github.com/che1/worker/internal/db"
	"github.com/che1/worker/internal/external"
	"github.com/che1/worker/internal/grpcapi"
	"github.com/che1/worker/internal/httpapi"
	"github.com/che1/worker/internal/logging"
	"github.com/che1/worker/internal/pubsub"
	"github.com/che1/worker/internal/service"
	"github.com/che1/worker/internal/ws"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log := logging.New(cfg.Log.Level, cfg.Log.Format)
	log.Info("starting worker")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Database
	pool, err := db.New(ctx, cfg.Database.URL, cfg.Database.MaxConns, log)
	if err != nil {
		log.Error("db init", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	taskRepo := db.NewTaskRepo(pool)
	migrateCtx, cancelMigrate := context.WithTimeout(ctx, 10*time.Second)
	if err := taskRepo.Migrate(migrateCtx); err != nil {
		cancelMigrate()
		log.Error("db migrate", "err", err)
		os.Exit(1)
	}
	cancelMigrate()

	// Redis pub/sub (optional)
	var pub *pubsub.Publisher
	if cfg.Redis.Addr != "" {
		p, err := pubsub.NewPublisher(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB, cfg.Redis.Channel, log)
		if err != nil {
			log.Warn("redis unavailable, continuing without pub/sub", "err", err)
		} else {
			pub = p
			defer pub.Close()
		}
	}

	// External API client (available for future handlers).
	_ = external.NewClient(cfg.External.BaseURL, cfg.External.APIKey, cfg.External.Timeout, log)

	// WebSocket hub for frontend.
	hub := ws.NewHub(log)

	// Business-logic layer wired to both REST and gRPC.
	tasks := service.NewTasks(taskRepo, pub, hub, log)

	httpSrv := httpapi.NewServer(tasks, cfg.Inbound.APIKey, log)
	grpcSrv := grpcapi.NewServer(tasks, cfg.Inbound.APIKey, log)

	errCh := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Add(3)

	go func() { defer wg.Done(); errCh <- runNamed("ws", hub.Serve(ctx, cfg.WS.ListenAddr, cfg.WS.Path)) }()
	go func() { defer wg.Done(); errCh <- runNamed("http", httpSrv.Serve(ctx, cfg.HTTP.ListenAddr)) }()
	go func() { defer wg.Done(); errCh <- runNamed("grpc", grpcSrv.Serve(ctx, cfg.GRPC.ListenAddr)) }()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Info("shutdown signal received", "sig", sig.String())
	case err := <-errCh:
		if err != nil {
			log.Error("server exited", "err", err)
		}
	}
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		log.Warn("shutdown timeout exceeded")
	}
	log.Info("worker stopped")
}

func runNamed(name string, err error) error {
	if err != nil && !errors.Is(err, context.Canceled) {
		return wrap(name, err)
	}
	return nil
}

type namedErr struct {
	name string
	err  error
}

func (e *namedErr) Error() string { return e.name + ": " + e.err.Error() }
func (e *namedErr) Unwrap() error { return e.err }

func wrap(name string, err error) error { return &namedErr{name: name, err: err} }
