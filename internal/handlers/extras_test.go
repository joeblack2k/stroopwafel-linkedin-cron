package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
)

func TestAPIBulkSetPostChannelsRejectsClearingScheduledPosts(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)

	post, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:        "scheduled",
		Status:      model.StatusScheduled,
		ScheduledAt: ptrTimeForTest(time.Now().UTC().Add(30 * time.Minute)),
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := app.Store.ReplacePostChannels(context.Background(), post.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("assign channels: %v", err)
	}

	payload := map[string]any{
		"post_ids":    []int64{post.ID},
		"channel_ids": []int64{},
	}
	recorder := performJSONHandlerRequest(t, http.MethodPost, "/api/v1/posts/bulk/channels", payload, app.APIBulkSetPostChannels)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", recorder.Code, recorder.Body.String())
	}
}

func TestAPIListPostAttemptsReturnsChannelAttempts(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)

	now := time.Now().UTC().Add(-1 * time.Minute)
	post, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:        "attempt me",
		Status:      model.StatusScheduled,
		ScheduledAt: &now,
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := app.Store.ReplacePostChannels(context.Background(), post.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("assign channels: %v", err)
	}

	if err := app.Scheduler.SendNow(context.Background(), post.ID); err != nil {
		t.Fatalf("send now: %v", err)
	}

	path := "/api/v1/posts/" + strconv.FormatInt(post.ID, 10) + "/attempts"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("id", strconv.FormatInt(post.ID, 10))
	recorder := httptest.NewRecorder()
	app.APIListPostAttempts(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("expected at least one attempt entry")
	}
	if got := payload[0]["status"]; got == nil || strings.TrimSpace(got.(string)) == "" {
		t.Fatalf("expected attempt status, got %+v", payload[0])
	}
}

func performJSONHandlerRequest(t *testing.T, method, path string, payload any, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if strings.HasPrefix(path, "/api/v1/posts/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/v1/posts/"), "/")
		if len(parts) > 0 && parts[0] != "" {
			request.SetPathValue("id", parts[0])
		}
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func ptrTimeForTest(value time.Time) *time.Time {
	copyValue := value.UTC()
	return &copyValue
}
