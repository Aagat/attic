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
	"sync"
	"syscall"
	"time"

	"attic/internal/acquisition"
	"attic/internal/ai"
	"attic/internal/application"
	"attic/internal/capture"
	"attic/internal/config"
	"attic/internal/delivery"
	"attic/internal/filesystem"
	"attic/internal/formatter"
	"attic/internal/httpapi"
	"attic/internal/library"
	"attic/internal/postgres"
	"attic/internal/processing"
	"attic/internal/recovery"
	"attic/internal/search"
	"attic/internal/subscription"
	"path/filepath"
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
	if len(os.Args) == 2 && os.Args[1] == "login-chatgpt" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		err := subscription.NewStore(os.Getenv("CHATGPT_AUTH_FILE")).Login(ctx, func(url, code string) {
			fmt.Printf("Open %s and enter code: %s\n", url, code)
			fmt.Println("Waiting for ChatGPT authorization...")
		})
		if err == nil {
			fmt.Println("ChatGPT subscription connected. Credentials saved privately.")
		}
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "check-ai" {
			return errors.New("usage: attic [check-ai|login-chatgpt]")
		}
		return runAICheck(cfg)
	}
	return runServer(cfg)
}

func runServer(cfg config.Config) error {

	destination := ""
	if cfg.SMTP.Enabled {
		destination = cfg.SMTP.Destination
	}
	store, err := postgres.Open(context.Background(), cfg.DatabaseURL, postgres.Options{
		DeliveryDestination: "",
		MaxOpenConns:        cfg.DBPoolMax,
		MaxIdleConns:        cfg.DBPoolMin,
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
	renderer, err := acquisition.NewChromiumRenderer(acquisition.ChromiumConfig{
		Concurrency: cfg.Browser.Concurrency, Executable: cfg.Browser.Executable, NavigationTimeout: cfg.Browser.NavigationTimeout,
		RenderTimeout: cfg.Browser.RenderTimeout, MaxDOMBytes: cfg.Browser.MaxDOMBytes,
		MaxDOMNodes: cfg.Browser.MaxDOMNodes, MaxScreenshotBytes: cfg.Browser.MaxScreenshotBytes,
		ScreenshotWidth: cfg.Browser.ScreenshotWidth, ScreenshotHeight: cfg.Browser.ScreenshotHeight,
		MaxTransferredBytes: cfg.Browser.MaxTransferredBytes, MaxRedirects: int64(cfg.Browser.MaxRedirects),
	})
	if err != nil {
		return err
	}
	aiClient, err := newAnalyzer(cfg.AI)
	if err != nil {
		return fmt.Errorf("AI configuration failed: %s", ai.CodeOf(err))
	}
	approver, err := ai.NewArticleApprover(aiClient, store)
	if err != nil {
		return fmt.Errorf("AI approval configuration failed: %s", ai.CodeOf(err))
	}
	pdf := formatter.PDF{MaxBytes: cfg.PDF.MaxBytes, Timeout: cfg.PDF.Timeout, PandocPath: "/usr/bin/pandoc",
		MarginMM: cfg.PDF.MarginMM, BodyFontPT: cfg.PDF.BodyFontPT, LineHeight: cfg.PDF.LineHeight}
	saved, err := library.Open(context.Background(), library.Options{DatabaseURL: cfg.DatabaseURL, ArtifactRoot: cfg.ArtifactRoot, Profile: cfg.DefaultProfile, Destination: destination})
	if err != nil {
		return err
	}
	defer saved.Close()
	recoveryBrowser := recovery.New(func(ctx context.Context, raw string) (recovery.Browser, error) {
		return acquisition.OpenBrowserSession(ctx, cfg.Browser.Executable, filepath.Join(cfg.ArtifactRoot, "..", "browser-profiles"), raw)
	}, aiClient, saved.SaveRecoveredPage)
	defer recoveryBrowser.Close()
	processor, err := processing.New(renderer, approver, formatter.Checked{Renderer: pdf}, processing.WithArchives(acquisition.Archives{}), processing.WithSavedPages(saved, renderer), processing.WithBrowserRecovery(recovery.PageRecovery{Browser: recoveryBrowser, Renderer: renderer}))
	if err != nil {
		return err
	}
	archive, err := application.NewArchive(store, artifacts, application.ArchiveOptions{
		DefaultProfile: cfg.DefaultProfile,
		Profiles:       cfg.Profiles,
		Processor:      processor,
	})
	if err != nil {
		return err
	}
	worker, err := application.NewWorker(archive, application.WorkerOptions{Processor: processor, MaxAttempts: cfg.AI.MaxRetries + 1})
	if err != nil {
		return err
	}
	var index search.Index
	if cfg.SearchURL != "" {
		index, err = search.NewMeilisearch(search.Config{URL: cfg.SearchURL, APIKey: cfg.SearchAPIKey, Index: cfg.SearchIndex, Timeout: 15 * time.Second})
		if err != nil {
			return err
		}
	}
	server := httpapi.NewServerWithOptions(archive, readiness, cfg.BearerToken, httpapi.Options{Library: saved, Search: index, Recovery: recoveryBrowser})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var background sync.WaitGroup
	start := func(run func(context.Context) error) {
		background.Add(1)
		go func() { defer background.Done(); run(ctx) }()
	}
	start(func(ctx context.Context) error {
		return saved.RunCaptures(ctx, recovery.Capturer{Base: capture.New(renderer, acquisition.Archives{}), Browser: recoveryBrowser})
	})
	start(saved.RunCleanup)
	start(func(ctx context.Context) error { return saved.RunEnrichment(ctx, aiClient) })
	if index != nil {
		start(func(ctx context.Context) error { return saved.RunIndex(ctx, index) })
	}
	defer func() { stop(); background.Wait() }()
	workerDone := make(chan error, 1)
	go func() {
		err := worker.Run(ctx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			log.Printf("worker stopped: %v", err)
			stop()
		}
		workerDone <- err
	}()

	mailDone := make(chan error, 1)
	if cfg.SMTP.Enabled {
		sender, err := delivery.NewSMTP(cfg.SMTP)
		if err != nil {
			return err
		}
		mailWorker := delivery.Worker{Queue: store, Artifacts: artifacts, Sender: sender, Timeout: cfg.SMTP.Timeout}
		go func() {
			err := mailWorker.Run(ctx)
			if err != nil && ctx.Err() == nil {
				log.Printf("email worker stopped: %v", err)
				stop()
			}
			mailDone <- err
		}()
	} else {
		mailDone <- nil
	}
	defer func() { waitForWorker(mailDone) }()
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
	client, err := newAnalyzer(cfg.AI)
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

func newAnalyzer(cfg config.AIConfig) (*ai.Client, error) {
	options := ai.Config{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model, ReasoningEffort: cfg.ReasoningEffort, OmitReasoningEffort: cfg.OmitReasoningEffort, OmitResponseFormat: cfg.OmitResponseFormat, Timeout: cfg.Timeout}
	if cfg.Provider == "chatgpt" {
		return ai.NewSubscriptionClient(options, subscription.NewStore(cfg.AuthFile))
	}
	return ai.NewClient(options)
}
