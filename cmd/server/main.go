package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"stroopwafel/internal/config"
	"stroopwafel/internal/db"
	"stroopwafel/internal/facebook"
	"stroopwafel/internal/handlers"
	"stroopwafel/internal/instagram"
	"stroopwafel/internal/linkedin"
	"stroopwafel/internal/model"
	"stroopwafel/internal/publisher"
	"stroopwafel/internal/scheduler"
	"stroopwafel/internal/views"
	"stroopwafel/internal/webhooks"
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

	migrationStatus, err := db.Migrate(context.Background(), database)
	if err != nil {
		logger.LogAttrs(context.Background(), slog.LevelError, "failed to migrate db", slog.String("error", err.Error()))
		os.Exit(1)
	}

	store := db.NewStore(database)
	pub, activeMode := buildPublisher(cfg, logger)
	schedulerService := scheduler.NewService(store, pub, logger)
	schedulerService.SetChannelPublisherResolver(func(channel model.Channel) publisher.Publisher {
		return buildChannelPublisher(channel, logger)
	})

	webhookDispatcher := webhooks.NewDispatcher(cfg.WebhookURLs, cfg.WebhookSecret, "server", logger)
	schedulerService.SetEventNotifier(func(ctx context.Context, eventName string, payload map[string]any) {
		outcomes := webhookDispatcher.Emit(ctx, eventName, payload)
		persistWebhookOutcomes(ctx, store, logger, outcomes)
	})

	renderer, err := views.NewRenderer(filepath.Join("web", "templates", "*.html"))
	if err != nil {
		logger.LogAttrs(context.Background(), slog.LevelError, "failed to parse templates", slog.String("error", err.Error()))
		os.Exit(1)
	}

	app := &handlers.App{
		Config:             cfg,
		Store:              store,
		Scheduler:          schedulerService,
		Renderer:           renderer,
		Logger:             logger,
		MigrationStatus:    migrationStatus,
		RequestedPublisher: cfg.PublisherMode,
		ActivePublisher:    activeMode,
		LinkedInConfigured: cfg.LinkedInConfigured(),
		FacebookConfigured: cfg.FacebookConfigured(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.Healthz)
	mux.HandleFunc("GET /api/v1/healthz", app.APIHealthz)
	mux.HandleFunc("GET /login", app.LoginPage)
	mux.HandleFunc("POST /login", app.LoginSubmit)
	mux.HandleFunc("GET /logout", app.Logout)
	mux.HandleFunc("POST /logout", app.Logout)

	uiAuth := handlers.UIAuthMiddleware(cfg, logger)
	apiAuth := handlers.APIAuthMiddleware(cfg.BasicAuthUser, cfg.BasicAuthPass, store, cfg.StaticAPIKeys, logger)
	registerProtectedRoutes(mux, uiAuth, apiAuth, app)

	handler := requestLogger(logger, mux)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.LogAttrs(
		context.Background(),
		slog.LevelInfo,
		"server starting",
		slog.String("component", "server"),
		slog.String("addr", cfg.Addr),
		slog.String("db_path", cfg.DBPath),
		slog.String("publisher", activeMode),
	)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.LogAttrs(context.Background(), slog.LevelError, "server failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.LogAttrs(context.Background(), slog.LevelError, "graceful shutdown failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func registerProtectedRoutes(mux *http.ServeMux, uiAuth func(http.Handler) http.Handler, apiAuth func(http.Handler) http.Handler, app *handlers.App) {
	mux.Handle("GET /", uiAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/calendar", http.StatusSeeOther)
	})))

	staticDir := filepath.Join("web", "static")
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir)))
	mux.Handle("GET /static/", staticHandler)

	mediaDir := filepath.Join(app.Config.DataDir, "uploads")
	_ = os.MkdirAll(mediaDir, 0o755)
	mediaHandler := http.StripPrefix("/media/", http.FileServer(http.Dir(mediaDir)))
	mux.Handle("GET /media/", mediaHandler)

	mux.Handle("GET /calendar", uiAuth(http.HandlerFunc(app.Calendar)))
	mux.Handle("GET /analytics", uiAuth(http.HandlerFunc(app.Analytics)))
	mux.Handle("GET /analytics/data", uiAuth(http.HandlerFunc(app.AnalyticsData)))
	mux.Handle("GET /posts/new", uiAuth(http.HandlerFunc(app.NewPost)))
	mux.Handle("GET /posts/bulk", uiAuth(http.HandlerFunc(app.BulkPosts)))
	mux.Handle("POST /posts/bulk/channels", uiAuth(http.HandlerFunc(app.BulkSetPostChannels)))
	mux.Handle("POST /posts/bulk/send-now", uiAuth(http.HandlerFunc(app.BulkSendNowPosts)))
	mux.Handle("POST /posts", uiAuth(http.HandlerFunc(app.CreatePost)))
	mux.Handle("POST /media/upload", uiAuth(http.HandlerFunc(app.UploadMediaUI)))
	mux.Handle("GET /posts/{id}", uiAuth(http.HandlerFunc(app.ViewPost)))
	mux.Handle("GET /posts/{id}/edit", uiAuth(http.HandlerFunc(app.EditPost)))
	mux.Handle("GET /posts/{id}/history", uiAuth(http.HandlerFunc(app.PostHistory)))
	mux.Handle("POST /posts/{id}", uiAuth(http.HandlerFunc(app.UpdatePost)))
	mux.Handle("POST /posts/{id}/delete", uiAuth(http.HandlerFunc(app.DeletePost)))
	mux.Handle("POST /posts/{id}/send-now", uiAuth(http.HandlerFunc(app.SendNowPost)))
	mux.Handle("POST /posts/{id}/send-and-delete", uiAuth(http.HandlerFunc(app.SendAndDeletePost)))
	mux.Handle("POST /posts/{id}/reschedule", uiAuth(http.HandlerFunc(app.ReschedulePost)))
	mux.Handle("GET /settings", uiAuth(http.HandlerFunc(app.Settings)))
	mux.Handle("GET /settings/webhooks/replays", uiAuth(http.HandlerFunc(app.WebhookReplays)))
	mux.Handle("POST /settings/webhooks/replays/replay-failed", uiAuth(http.HandlerFunc(app.ReplayFailedWebhooks)))
	mux.Handle("POST /settings/webhooks/replays/{id}/replay", uiAuth(http.HandlerFunc(app.ReplayWebhook)))
	mux.Handle("POST /settings/webhooks/replays/{id}/cancel", uiAuth(http.HandlerFunc(app.CancelWebhookReplay)))
	mux.Handle("GET /settings/channels", uiAuth(http.HandlerFunc(app.Channels)))
	mux.Handle("POST /settings/api-keys", uiAuth(http.HandlerFunc(app.CreateAPIKey)))
	mux.Handle("POST /settings/api-keys/bot-handoff", uiAuth(http.HandlerFunc(app.CreateBotHandoff)))
	mux.Handle("POST /settings/api-keys/{id}/revoke", uiAuth(http.HandlerFunc(app.RevokeAPIKey)))
	mux.Handle("POST /settings/channels", uiAuth(http.HandlerFunc(app.CreateChannel)))
	mux.Handle("GET /settings/channels/{id}/edit", uiAuth(http.HandlerFunc(app.EditChannel)))
	mux.Handle("POST /settings/channels/{id}", uiAuth(http.HandlerFunc(app.UpdateChannel)))
	mux.Handle("POST /settings/channels/{id}/rules", uiAuth(http.HandlerFunc(app.UpdateChannelRules)))
	mux.Handle("POST /settings/channels/{id}/delete", uiAuth(http.HandlerFunc(app.DeleteChannel)))
	mux.Handle("POST /settings/channels/{id}/test", uiAuth(http.HandlerFunc(app.TestChannel)))
	mux.Handle("POST /settings/channels/{id}/disable", uiAuth(http.HandlerFunc(app.DisableChannel)))
	mux.Handle("POST /settings/channels/{id}/enable", uiAuth(http.HandlerFunc(app.EnableChannel)))

	apiMutating := func(handler http.HandlerFunc) http.Handler {
		return apiAuth(app.WithAPIAuditTrail(http.HandlerFunc(app.WithAPIIdempotency(handler))))
	}

	mux.Handle("GET /api/v1/posts", apiAuth(http.HandlerFunc(app.APIListPosts)))
	mux.Handle("GET /api/v1/posts/{id}", apiAuth(http.HandlerFunc(app.APIGetPost)))
	mux.Handle("POST /api/v1/posts", apiMutating(app.APICreatePost))
	mux.Handle("POST /api/v1/media/upload", apiAuth(http.HandlerFunc(app.APIUploadMedia)))
	mux.Handle("POST /api/v1/posts/guardrails", apiMutating(app.APICheckPostGuardrails))
	mux.Handle("PUT /api/v1/posts/{id}", apiMutating(app.APIUpdatePost))
	mux.Handle("DELETE /api/v1/posts/{id}", apiMutating(app.APIDeletePost))
	mux.Handle("POST /api/v1/posts/{id}/send-now", apiMutating(app.APISendNowPost))
	mux.Handle("POST /api/v1/posts/{id}/send-and-delete", apiMutating(app.APISendAndDeletePost))
	mux.Handle("POST /api/v1/posts/{id}/reschedule", apiMutating(app.APIReschedulePost))
	mux.Handle("GET /api/v1/posts/{id}/attempts", apiAuth(http.HandlerFunc(app.APIListPostAttempts)))
	mux.Handle("GET /api/v1/publish-attempts", apiAuth(http.HandlerFunc(app.APIListPublishAttempts)))
	mux.Handle("POST /api/v1/posts/{id}/attempts/{attempt_id}/screenshot", apiMutating(app.APISetPostAttemptScreenshot))
	mux.Handle("POST /api/v1/posts/{id}/attempts/{attempt_id}/retry", apiMutating(app.APIRetryPostAttempt))
	mux.Handle("POST /api/v1/posts/bulk/send-now", apiMutating(app.APIBulkSendNowPosts))
	mux.Handle("POST /api/v1/posts/bulk/channels", apiMutating(app.APIBulkSetPostChannels))
	mux.Handle("GET /api/v1/settings/status", apiAuth(http.HandlerFunc(app.APISettingsStatus)))
	mux.Handle("GET /api/v1/settings/webhooks", apiAuth(http.HandlerFunc(app.APISettingsWebhookHealth)))
	mux.Handle("GET /api/v1/webhooks/replays", apiAuth(http.HandlerFunc(app.APIListWebhookReplays)))
	mux.Handle("GET /api/v1/webhooks/dead-letters", apiAuth(http.HandlerFunc(app.APIListWebhookDeadLetters)))
	mux.Handle("GET /api/v1/webhooks/dead-letters/alerts", apiAuth(http.HandlerFunc(app.APIWebhookDeadLetterAlerts)))
	mux.Handle("GET /api/v1/audit-events", apiAuth(http.HandlerFunc(app.APIListAuditEvents)))
	mux.Handle("POST /api/v1/webhooks/replays/{id}/replay", apiMutating(app.APIReplayWebhook))
	mux.Handle("POST /api/v1/webhooks/replays/{id}/cancel", apiMutating(app.APICancelWebhookReplay))
	mux.Handle("POST /api/v1/webhooks/replays/replay-failed", apiMutating(app.APIReplayFailedWebhooks))
	mux.Handle("GET /api/v1/meta/openapi", apiAuth(http.HandlerFunc(app.APIOpenAPI)))
	mux.Handle("GET /api/v1/meta/error-codes", apiAuth(http.HandlerFunc(app.APIErrorCatalog)))
	mux.Handle("GET /api/v1/analytics/overview", apiAuth(http.HandlerFunc(app.APIAnalyticsOverview)))
	mux.Handle("GET /api/v1/analytics/weekly-snapshot", apiAuth(http.HandlerFunc(app.APIWeeklySnapshot)))
	mux.Handle("GET /api/v1/analytics/channels", apiAuth(http.HandlerFunc(app.APIAnalyticsChannelDelivery)))
	mux.Handle("POST /api/v1/settings/bot-handoff", apiMutating(app.APICreateBotHandoff))
	mux.Handle("GET /api/v1/media/assets", apiAuth(http.HandlerFunc(app.APIListMediaAssets)))
	mux.Handle("GET /api/v1/media/assets/{id}", apiAuth(http.HandlerFunc(app.APIGetMediaAsset)))
	mux.Handle("POST /api/v1/media/assets", apiMutating(app.APICreateMediaAsset))
	mux.Handle("PUT /api/v1/media/assets/{id}", apiMutating(app.APIUpdateMediaAsset))
	mux.Handle("DELETE /api/v1/media/assets/{id}", apiMutating(app.APIDeleteMediaAsset))
	mux.Handle("GET /api/v1/templates", apiAuth(http.HandlerFunc(app.APIListContentTemplates)))
	mux.Handle("GET /api/v1/templates/{id}", apiAuth(http.HandlerFunc(app.APIGetContentTemplate)))
	mux.Handle("POST /api/v1/templates", apiMutating(app.APICreateContentTemplate))
	mux.Handle("PUT /api/v1/templates/{id}", apiMutating(app.APIUpdateContentTemplate))
	mux.Handle("DELETE /api/v1/templates/{id}", apiMutating(app.APIDeleteContentTemplate))
	mux.Handle("GET /api/v1/channels", apiAuth(http.HandlerFunc(app.APIListChannels)))
	mux.Handle("GET /api/v1/channels/health", apiAuth(http.HandlerFunc(app.APIListChannelHealth)))
	mux.Handle("POST /api/v1/channels", apiMutating(app.APICreateChannel))
	mux.Handle("PUT /api/v1/channels/{id}", apiMutating(app.APIUpdateChannel))
	mux.Handle("POST /api/v1/channels/{id}/rotate-credentials", apiMutating(app.APIRotateChannelCredentials))
	mux.Handle("GET /api/v1/channels/{id}/rules", apiAuth(http.HandlerFunc(app.APIGetChannelRules)))
	mux.Handle("PUT /api/v1/channels/{id}/rules", apiMutating(app.APIUpdateChannelRules))
	mux.Handle("GET /api/v1/channels/{id}/retry-policy", apiAuth(http.HandlerFunc(app.APIGetChannelRetryPolicy)))
	mux.Handle("PUT /api/v1/channels/{id}/retry-policy", apiMutating(app.APIUpdateChannelRetryPolicy))
	mux.Handle("GET /api/v1/channels/{id}/audit", apiAuth(http.HandlerFunc(app.APIListChannelAuditEvents)))
	mux.Handle("DELETE /api/v1/channels/{id}", apiMutating(app.APIDeleteChannel))
	mux.Handle("POST /api/v1/channels/{id}/test", apiMutating(app.APITestChannel))
	mux.Handle("POST /api/v1/channels/{id}/disable", apiMutating(app.APIDisableChannel))
	mux.Handle("POST /api/v1/channels/{id}/enable", apiMutating(app.APIEnableChannel))
	mux.Handle("POST /api/v1/channels/bulk/disable", apiMutating(app.APIBulkDisableChannels))
	mux.Handle("POST /api/v1/channels/bulk/enable", apiMutating(app.APIBulkEnableChannels))
}

func buildPublisher(cfg config.Config, logger *slog.Logger) (publisher.Publisher, string) {
	if cfg.PublisherMode == "linkedin" {
		linkedInPublisher := linkedin.NewPublisher(cfg.LinkedInAPIBase, cfg.LinkedInToken, cfg.LinkedInAuthorURN, logger)
		if linkedInPublisher.Configured() {
			return linkedInPublisher, linkedInPublisher.Mode()
		}
		logger.LogAttrs(context.Background(), slog.LevelWarn, "linkedin publisher not configured; falling back to dry-run")
	}

	if cfg.PublisherMode == "facebook-page" {
		facebookPublisher := facebook.NewPublisher(cfg.FacebookAPIBase, cfg.FacebookPageToken, cfg.FacebookPageID, logger)
		if facebookPublisher.Configured() {
			return facebookPublisher, facebookPublisher.Mode()
		}
		logger.LogAttrs(context.Background(), slog.LevelWarn, "facebook page publisher not configured; falling back to dry-run")
	}

	dryRun := publisher.NewDryRunPublisher(logger)
	return dryRun, dryRun.Mode()
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
	case model.ChannelTypeDryRun:
		return publisher.NewDryRunPublisher(logger)
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

func persistWebhookOutcomes(ctx context.Context, store *db.Store, logger *slog.Logger, outcomes []webhooks.DeliveryOutcome) {
	if store == nil || len(outcomes) == 0 {
		return
	}

	for _, outcome := range outcomes {
		status := "failed"
		if outcome.Delivered {
			status = "delivered"
		}

		delivery, err := store.InsertWebhookDelivery(ctx, db.WebhookDeliveryInput{
			EventID:     outcome.EventID,
			EventName:   outcome.EventName,
			TargetURL:   outcome.TargetURL,
			Status:      status,
			HTTPStatus:  outcome.HTTPStatus,
			Error:       outcome.Error,
			Source:      outcome.Source,
			DurationMS:  outcome.DurationMS,
			OccurredAt:  outcome.OccurredAt,
			DeliveredAt: outcome.DeliveredAt,
		})
		if err != nil {
			logger.LogAttrs(
				ctx,
				slog.LevelWarn,
				"failed to persist webhook delivery",
				slog.String("component", "webhook"),
				slog.String("event", outcome.EventName),
				slog.String("url", outcome.TargetURL),
				slog.String("error", err.Error()),
			)
			continue
		}

		if outcome.Delivered {
			continue
		}

		payloadJSON := "{}"
		if len(outcome.Payload) > 0 {
			encodedPayload, marshalErr := json.Marshal(outcome.Payload)
			if marshalErr != nil {
				logger.LogAttrs(
					ctx,
					slog.LevelWarn,
					"failed to marshal webhook replay payload",
					slog.String("component", "webhook"),
					slog.String("event", outcome.EventName),
					slog.String("url", outcome.TargetURL),
					slog.String("error", marshalErr.Error()),
				)
			} else {
				payloadJSON = string(encodedPayload)
			}
		}

		deliveryID := delivery.ID
		if _, replayErr := store.InsertWebhookReplay(ctx, db.WebhookReplayInput{
			DeliveryID: &deliveryID,
			EventID:    outcome.EventID,
			EventName:  outcome.EventName,
			TargetURL:  outcome.TargetURL,
			Source:     outcome.Source,
			Payload:    payloadJSON,
			Headers:    "{}",
			Status:     db.WebhookReplayStatusQueued,
			LastError:  outcome.Error,
			HTTPStatus: outcome.HTTPStatus,
		}); replayErr != nil {
			logger.LogAttrs(
				ctx,
				slog.LevelWarn,
				"failed to queue webhook replay",
				slog.String("component", "webhook"),
				slog.String("event", outcome.EventName),
				slog.String("url", outcome.TargetURL),
				slog.String("error", replayErr.Error()),
			)
		}
	}
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		authMethod := handlers.AuthMethodForLog(r.Context())
		if authMethod == "unknown" {
			authMethod = strings.TrimSpace(r.Header.Get("X-SW-Auth-Method"))
			if authMethod == "" {
				authMethod = "unknown"
			}
		}

		apiKeyID := handlers.APIKeyIDForLog(r.Context())
		if apiKeyID == 0 {
			rawAPIKeyID := strings.TrimSpace(r.Header.Get("X-SW-API-Key-ID"))
			if rawAPIKeyID != "" {
				if parsed, err := strconv.ParseInt(rawAPIKeyID, 10, 64); err == nil {
					apiKeyID = parsed
				}
			}
		}

		apiKeyName := handlers.APIKeyNameForLog(r.Context())
		if strings.TrimSpace(apiKeyName) == "" {
			apiKeyName = strings.TrimSpace(r.Header.Get("X-SW-API-Key-Name"))
		}

		logger.LogAttrs(
			r.Context(),
			slog.LevelInfo,
			"http request",
			slog.String("component", "http"),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("auth_method", authMethod),
			slog.Int64("api_key_id", apiKeyID),
			slog.String("api_key_name", apiKeyName),
			slog.String("remote_addr", strings.Split(r.RemoteAddr, ":")[0]),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}
