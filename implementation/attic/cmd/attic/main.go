package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"attic/internal/application"
	"attic/internal/config"
	"attic/internal/filesystem"
	"attic/internal/httpapi"
	"attic/internal/postgres"
)

const (
	serverReadTimeout       = 30 * time.Second
	serverWriteTimeout      = 5 * time.Minute
	serverIdleTimeout       = 60 * time.Second
	serverReadHeaderTimeout = 10 * time.Second
	serverMaxHeaderBytes    = 1 << 20
)

func main() {
	if err := run(); err != nil {
		log.Printf("attic stopped: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store, err := postgres.Open(context.Background(), cfg.DatabaseURL, postgres.Options{
		MaxOpenConns: cfg.DBPoolMax,
		MaxIdleConns: cfg.DBPoolMin,
	})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			log.Printf("database close failed: %v", closeErr)
		}
	}()

	migrations, err := postgres.NewMigrationRunnerForStore(store, cfg.MigrationsDir)
	if err != nil {
		return err
	}
	if err := migrations.Run(context.Background()); err != nil {
		return err
	}

	artifacts, err := filesystem.NewArtifactStore(cfg.ArtifactRoot)
	if err != nil {
		return err
	}
	readiness, err := postgres.NewReadiness(store, migrations, cfg.ArtifactRoot)
	if err != nil {
		return err
	}
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		DefaultProfile: cfg.DefaultProfile,
		Profiles:       cfg.Profiles,
	})
	if err != nil {
		return err
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{})
	if err != nil {
		return err
	}
	server := httpapi.NewServer(archive, readiness, cfg.BearerToken)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	workerDone := make(chan error, 1)
	go func() {
		err := worker.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("worker stopped: %v", err)
			stop()
		}
		workerDone <- err
	}()

	httpServer := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           server,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("http shutdown failed: %v", err)
		}
	}()

	log.Printf("attic listening on %s", cfg.ListenAddress)
	listenErr := httpServer.ListenAndServe()
	if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
		stop()
		waitForWorker(workerDone)
		return listenErr
	}
	stop()
	workerErr := waitForWorker(workerDone)
	if workerErr != nil && !errors.Is(workerErr, context.Canceled) && !errors.Is(workerErr, context.DeadlineExceeded) {
		return workerErr
	}
	return nil
}

func waitForWorker(workerDone <-chan error) error {
	select {
	case workerErr := <-workerDone:
		return workerErr
	case <-time.After(10 * time.Second):
		return errors.New("worker shutdown timed out")
	}
}
