package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"attic/internal/ai"
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
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "check-ai" {
			return errors.New("usage: attic [check-ai]")
		}
		return runAICheck(cfg)
	}
	return runServer(cfg)
}

func runServer(cfg config.Config) error {

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

const compatibilityImage = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func runAICheck(cfg config.Config) error {
	client, err := ai.NewClient(ai.Config{
		BaseURL:             cfg.AI.BaseURL,
		APIKey:              cfg.AI.APIKey,
		Model:               cfg.AI.Model,
		ReasoningEffort:     cfg.AI.ReasoningEffort,
		OmitReasoningEffort: cfg.AI.OmitReasoningEffort,
		OmitResponseFormat:  cfg.AI.OmitResponseFormat,
		Timeout:             cfg.AI.Timeout,
	})
	if err != nil {
		return fmt.Errorf("AI compatibility check configuration failed: %s", ai.CodeOf(err))
	}
	fixtureText := strings.Repeat("This is a synthetic Attic compatibility article containing ordinary prose for protocol validation. ", 12)
	_, attempts, analyzeErr := client.Analyze(context.Background(), ai.AnalyzeRequest{
		SourceURL:         "https://example.invalid/attic-compatibility-fixture",
		CandidateText:     fixtureText,
		CandidateHTML:     "<article><h1>Attic compatibility fixture</h1><p>" + fixtureText + "</p></article>",
		Title:             "Attic compatibility fixture",
		SiteName:          "Attic",
		Language:          "en",
		ScreenshotDataURL: compatibilityImage,
	})
	if analyzeErr != nil && !validCompatibilityRejection(attempts) {
		code := ai.CodeOf(analyzeErr)
		if code == "" {
			code = ai.CodeAIUnavailable
		}
		return fmt.Errorf("AI compatibility check failed: %s", code)
	}
	if len(attempts) == 0 {
		return errors.New("AI compatibility check failed: no provider attempt was recorded")
	}
	last := attempts[len(attempts)-1]
	log.Printf("AI compatibility check passed: model=%s attempts=%d status=%s", last.Model, len(attempts), last.Status)
	return nil
}

func validCompatibilityRejection(attempts []ai.Attempt) bool {
	return len(attempts) > 0 && attempts[len(attempts)-1].Status == ai.AttemptRejected
}

func waitForWorker(workerDone <-chan error) error {
	select {
	case workerErr := <-workerDone:
		return workerErr
	case <-time.After(10 * time.Second):
		return errors.New("worker shutdown timed out")
	}
}
