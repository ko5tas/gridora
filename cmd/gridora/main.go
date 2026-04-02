package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ko5tas/gridora/internal/collector"
	"github.com/ko5tas/gridora/internal/config"
	"github.com/ko5tas/gridora/internal/exporter"
	"github.com/ko5tas/gridora/internal/myenergi"
	"github.com/ko5tas/gridora/internal/server"
	"github.com/ko5tas/gridora/internal/store"
)

var version = "dev"

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("gridora", version)
		os.Exit(0)
	}

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Setup logger
	logger := setupLogger(cfg.Logging)
	logger.Info("starting gridora", "version", version)

	// Open database
	db, err := store.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	ctx := context.Background()
	if err := db.Migrate(ctx); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("database ready", "path", cfg.Database.Path)

	// Create myenergi client
	client := myenergi.NewClient(cfg.MyEnergi.HubSerial, cfg.MyEnergi.APIKey, cfg.MyEnergi.RateLimit, logger)

	// Discover server
	if err := client.Discover(ctx); err != nil {
		logger.Error("failed to discover myenergi server", "error", err)
		os.Exit(1)
	}

	// Setup graceful shutdown
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start export scheduler in background
	if cfg.Export.Enabled {
		expSched := exporter.NewScheduler(db, exporter.SchedulerConfig{
			Path:     cfg.Export.Path,
			Time:     cfg.Export.Time,
			DBBackup: cfg.Export.DBBackup,
		}, logger)
		go expSched.Run(ctx)
		logger.Info("export scheduler started", "path", cfg.Export.Path, "time", cfg.Export.Time)
	}

	// Start HTTP server in background
	srv := server.New(db, logger)
	httpServer := &http.Server{Addr: cfg.Server.Listen, Handler: srv.Handler()}
	go func() {
		logger.Info("starting web server", "listen", cfg.Server.Listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("web server failed", "error", err)
		}
	}()

	// Start data collection (blocks until ctx is cancelled)
	sched := collector.NewScheduler(client, db, collector.SchedulerConfig{
		PollInterval:        cfg.MyEnergi.PollInterval,
		BackfillRateLimit:   cfg.Collection.BackfillRateLimit,
		DailyCollectionTime: cfg.Collection.DailyCollectionTime,
		BackfillOnStartup:   cfg.Collection.BackfillOnStartup,
	}, logger)

	logger.Info("gridora is running", "listen", cfg.Server.Listen)

	if err := sched.Run(ctx); err != nil {
		if ctx.Err() != nil {
			logger.Info("shutting down gracefully")
		} else {
			logger.Error("scheduler failed", "error", err)
			os.Exit(1)
		}
	}

	// Graceful HTTP shutdown
	httpServer.Shutdown(context.Background())
}

func setupLogger(cfg config.LoggingConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}

func defaultConfigPath() string {
	if v := os.Getenv("GRIDORA_CONFIG"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/etc/gridora/config.yaml"
	}
	return filepath.Join(home, ".config", "gridora", "config.yaml")
}
