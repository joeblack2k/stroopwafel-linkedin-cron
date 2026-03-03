package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"linkedin-cron/internal/db"
)

func TestAPIAuthMiddlewareWithAPIKey(t *testing.T) {
	t.Parallel()

	store := setupAuthStore(t)
	_, token, err := store.CreateAPIKey(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	middleware := APIAuthMiddleware("admin", "admin", store, logger)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AuthMethodForLog(r.Context()) != "api-key" {
			t.Fatalf("expected api-key auth method, got %s", AuthMethodForLog(r.Context()))
		}
		if APIKeyIDForLog(r.Context()) == 0 {
			t.Fatal("expected api key id in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("X-API-Key", token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestAPIAuthMiddlewareRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()

	store := setupAuthStore(t)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	middleware := APIAuthMiddleware("admin", "admin", store, logger)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil)
	req.Header.Set("X-API-Key", "invalid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}

func setupAuthStore(t *testing.T) *db.Store {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "auth.db")
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db.NewStore(database)
}
