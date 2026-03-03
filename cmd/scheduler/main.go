package main

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"linkedin-cron/internal/config"
	"linkedin-cron/internal/db"
	"linkedin-cron/internal/facebook"
	"linkedin-cron/internal/instagram"
	"linkedin-cron/internal/linkedin"
	"linkedin-cron/internal/model"
	"linkedin-cron/internal/publisher"
	"linkedin-cron/internal/scheduler"
	"linkedin-cron/internal/webhooks"
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
	service.SetChannelPublisherResolver(func(channel model.Channel) publisher.Publisher {
		return buildChannelPublisher(channel, logger)
	})

	webhookDispatcher := webhooks.NewDispatcher(cfg.WebhookURLs, cfg.WebhookSecret, "scheduler", logger)
	service.SetEventNotifier(func(ctx context.Context, eventName string, payload map[string]any) {
		webhookDispatcher.Emit(ctx, eventName, payload)
	})

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

	if cfg.PublisherMode == "facebook-page" {
		facebookPublisher := facebook.NewPublisher(cfg.FacebookAPIBase, cfg.FacebookPageToken, cfg.FacebookPageID, logger)
		if facebookPublisher.Configured() {
			return facebookPublisher
		}
		logger.LogAttrs(context.Background(), slog.LevelWarn, "facebook page publisher not configured; falling back to dry-run")
	}

	return publisher.NewDryRunPublisher(logger)
}

func buildChannelPublisher(channel model.Channel, logger *slog.Logger) publisher.Publisher {
	switch channel.Type {
	case model.ChannelTypeLinkedIn:
		baseURL := strings.TrimSpace(derefNullableString(channel.LinkedInAPIBaseURL))
		if baseURL == "" {
			baseURL = "https://api.linkedin.com"
		}
		return linkedin.NewPublisher(
			baseURL,
			derefNullableString(channel.LinkedInAccessToken),
			derefNullableString(channel.LinkedInAuthorURN),
			logger,
		)
	case model.ChannelTypeFacebook:
		baseURL := strings.TrimSpace(derefNullableString(channel.FacebookAPIBaseURL))
		if baseURL == "" {
			baseURL = "https://graph.facebook.com/v22.0"
		}
		return facebook.NewPublisher(
			baseURL,
			derefNullableString(channel.FacebookPageAccessToken),
			derefNullableString(channel.FacebookPageID),
			logger,
		)
	case model.ChannelTypeInstagram:
		baseURL := strings.TrimSpace(derefNullableString(channel.InstagramAPIBaseURL))
		if baseURL == "" {
			baseURL = "https://graph.facebook.com/v22.0"
		}
		return instagram.NewPublisher(
			baseURL,
			derefNullableString(channel.InstagramAccessToken),
			derefNullableString(channel.InstagramBusinessAccountID),
			logger,
		)
	case model.ChannelTypeDryRun:
		return publisher.NewDryRunPublisher(logger)
	default:
		return nil
	}
}

func derefNullableString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
