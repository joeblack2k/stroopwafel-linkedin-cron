package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"linkedin-cron/internal/config"
	"linkedin-cron/internal/db"
	"linkedin-cron/internal/facebook"
	"linkedin-cron/internal/handlers"
	"linkedin-cron/internal/linkedin"
	"linkedin-cron/internal/publisher"
	"linkedin-cron/internal/scheduler"
	"linkedin-cron/internal/views"
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

	auth := handlers.BasicAuthMiddleware(cfg.BasicAuthUser, cfg.BasicAuthPass, logger)
	registerProtectedRoutes(mux, auth, app)

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

func registerProtectedRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, app *handlers.App) {
	mux.Handle("GET /", auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/calendar", http.StatusSeeOther)
	})))

	staticDir := filepath.Join("web", "static")
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir)))
	mux.Handle("GET /static/", auth(staticHandler))

	mux.Handle("GET /calendar", auth(http.HandlerFunc(app.Calendar)))
	mux.Handle("GET /posts/new", auth(http.HandlerFunc(app.NewPost)))
	mux.Handle("POST /posts", auth(http.HandlerFunc(app.CreatePost)))
	mux.Handle("GET /posts/{id}/edit", auth(http.HandlerFunc(app.EditPost)))
	mux.Handle("POST /posts/{id}", auth(http.HandlerFunc(app.UpdatePost)))
	mux.Handle("POST /posts/{id}/delete", auth(http.HandlerFunc(app.DeletePost)))
	mux.Handle("POST /posts/{id}/send-now", auth(http.HandlerFunc(app.SendNowPost)))
	mux.Handle("GET /settings", auth(http.HandlerFunc(app.Settings)))

	mux.Handle("GET /api/v1/posts", auth(http.HandlerFunc(app.APIListPosts)))
	mux.Handle("GET /api/v1/posts/{id}", auth(http.HandlerFunc(app.APIGetPost)))
	mux.Handle("POST /api/v1/posts", auth(http.HandlerFunc(app.APICreatePost)))
	mux.Handle("PUT /api/v1/posts/{id}", auth(http.HandlerFunc(app.APIUpdatePost)))
	mux.Handle("DELETE /api/v1/posts/{id}", auth(http.HandlerFunc(app.APIDeletePost)))
	mux.Handle("POST /api/v1/posts/{id}/send-now", auth(http.HandlerFunc(app.APISendNowPost)))
	mux.Handle("GET /api/v1/settings/status", auth(http.HandlerFunc(app.APISettingsStatus)))
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

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		logger.LogAttrs(
			r.Context(),
			slog.LevelInfo,
			"http request",
			slog.String("component", "http"),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
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
