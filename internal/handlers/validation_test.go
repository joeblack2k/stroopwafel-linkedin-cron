package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"stroopwafel/internal/config"
	"stroopwafel/internal/db"
	"stroopwafel/internal/model"
	"stroopwafel/internal/publisher"
	"stroopwafel/internal/scheduler"
)

func TestAPICreatePostValidation(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)

	invalidPayload := map[string]any{
		"text":   "post body",
		"status": "sent",
	}
	recorder := performJSONRequest(t, http.MethodPost, "/api/v1/posts", invalidPayload, app.APICreatePost)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid create payload, got %d", recorder.Code)
	}

	invalidScheduledPayload := map[string]any{
		"text":   "post body",
		"status": "scheduled",
	}
	recorder = performJSONRequest(t, http.MethodPost, "/api/v1/posts", invalidScheduledPayload, app.APICreatePost)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing scheduled_at, got %d", recorder.Code)
	}

	missingChannelPayload := map[string]any{
		"text":         "hello world",
		"status":       "scheduled",
		"scheduled_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
	}
	recorder = performJSONRequest(t, http.MethodPost, "/api/v1/posts", missingChannelPayload, app.APICreatePost)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing channel_ids on scheduled post, got %d", recorder.Code)
	}

	validPayload := map[string]any{
		"text":         "hello world",
		"status":       "scheduled",
		"scheduled_at": time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
		"channel_ids":  []int64{channel.ID},
	}
	recorder = performJSONRequest(t, http.MethodPost, "/api/v1/posts", validPayload, app.APICreatePost)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid create payload, got %d", recorder.Code)
	}
}

func TestAPIUpdatePostValidation(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)
	created, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:        "draft post",
		Status:      model.StatusDraft,
		ScheduledAt: nil,
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	invalidUpdate := map[string]any{
		"text":      "updated",
		"status":    "draft",
		"media_url": "not-a-url",
	}
	path := "/api/v1/posts/" + strconv.FormatInt(created.ID, 10)
	recorder := performJSONRequest(t, http.MethodPut, path, invalidUpdate, app.APIUpdatePost)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid update payload, got %d", recorder.Code)
	}

	missingChannelUpdate := map[string]any{
		"text":         "updated",
		"status":       "scheduled",
		"scheduled_at": time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339),
	}
	recorder = performJSONRequest(t, http.MethodPut, path, missingChannelUpdate, app.APIUpdatePost)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for scheduled update without channels, got %d", recorder.Code)
	}

	validUpdate := map[string]any{
		"text":      "updated",
		"status":    "draft",
		"media_url": "https://example.com/article",
	}
	recorder = performJSONRequest(t, http.MethodPut, path, validUpdate, app.APIUpdatePost)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid update payload, got %d", recorder.Code)
	}

	validScheduledUpdate := map[string]any{
		"text":         "scheduled update",
		"status":       "scheduled",
		"scheduled_at": time.Now().UTC().Add(60 * time.Minute).Format(time.RFC3339),
		"channel_ids":  []int64{channel.ID},
	}
	recorder = performJSONRequest(t, http.MethodPut, path, validScheduledUpdate, app.APIUpdatePost)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid scheduled update payload, got %d", recorder.Code)
	}
}

func newAPIApp(t *testing.T) *App {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "handlers.db")
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := db.Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	store := db.NewStore(database)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return &App{
		Config: config.Config{Location: time.UTC, Timezone: "UTC"},
		Store:  store,
		Scheduler: scheduler.NewService(
			store,
			publisher.NewDryRunPublisher(logger),
			logger,
		),
		Logger:          logger,
		ActivePublisher: "dry-run",
	}
}

func mustCreateDryRunChannel(t *testing.T, store *db.Store) model.Channel {
	t.Helper()
	channel, err := store.CreateChannel(context.Background(), db.ChannelInput{
		Type:        model.ChannelTypeDryRun,
		DisplayName: "Dry Run",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return channel
}

func performJSONRequest(t *testing.T, method, target string, payload any, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(target, "/api/v1/posts/") {
		parts := strings.Split(strings.TrimPrefix(target, "/api/v1/posts/"), "/")
		if len(parts) > 0 && parts[0] != "" {
			request.SetPathValue("id", parts[0])
		}
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}
