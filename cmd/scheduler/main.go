package main

import (
	"context"
	"log/slog"
	"os"

	"linkedin-cron/internal/config"
	"linkedin-cron/internal/db"
	"linkedin-cron/internal/linkedin"
	"linkedin-cron/internal/publisher"
	"linkedin-cron/internal/scheduler"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.LogAttrs(context.Background(), slog.LevelError, "failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := db.EnsureDBDir(cfg.DBPath); err != nil {
		logger.LogAttrs(context.Background(), slog.LevelError, "failed to ensure db directory", slog.String("db_path", cfg.DBPath), slog.String("error", err.Error()))
		os.Exit(1)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		logger.LogAttrs(context.Background(), slog.LevelError, "failed to open db", slog.String("db_path", cfg.DBPath), slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer database.Close()

	if _, err := db.Migrate(context.Background(), database); err != nil {
		logger.LogAttrs(context.Background(), slog.LevelError, "failed to migrate db", slog.String("error", err.Error()))
		os.Exit(1)
	}

	pub := buildPublisher(cfg, logger)
	service := scheduler.NewService(db.NewStore(database), pub, logger)

	processed, err := service.RunDue(context.Background())
	if err != nil {
		logger.LogAttrs(context.Background(), slog.LevelError, "scheduler run failed", slog.String("component", "scheduler"), slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.LogAttrs(context.Background(), slog.LevelInfo, "scheduler run completed", slog.String("component", "scheduler"), slog.Int("processed", processed), slog.String("publisher", pub.Mode()))
}

func buildPublisher(cfg config.Config, logger *slog.Logger) publisher.Publisher {
	if cfg.PublisherMode == "linkedin" {
		linkedInPublisher := linkedin.NewPublisher(cfg.LinkedInAPIBase, cfg.LinkedInToken, cfg.LinkedInAuthorURN, logger)
		if linkedInPublisher.Configured() {
			return linkedInPublisher
		}
		logger.LogAttrs(context.Background(), slog.LevelWarn, "linkedin publisher not configured; falling back to dry-run")
	}
	return publisher.NewDryRunPublisher(logger)
}
