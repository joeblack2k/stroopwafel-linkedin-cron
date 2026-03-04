package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"stroopwafel/internal/db"
	"stroopwafel/internal/model"
)

func TestAPIGetChannelRetryPolicyReturnsDefaultWhenUnset(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)

	path := "/api/v1/channels/" + strconv.FormatInt(channel.ID, 10) + "/retry-policy"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("id", strconv.FormatInt(channel.ID, 10))
	recorder := httptest.NewRecorder()
	app.APIGetChannelRetryPolicy(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if configured, _ := response["configured"].(bool); configured {
		t.Fatalf("expected configured=false for default policy")
	}
	if got := int(response["max_retries"].(float64)); got != model.DefaultChannelMaxRetries {
		t.Fatalf("expected default max_retries=%d, got %d", model.DefaultChannelMaxRetries, got)
	}
}

func TestAPIUpdateChannelRetryPolicy(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)

	path := "/api/v1/channels/" + strconv.FormatInt(channel.ID, 10) + "/retry-policy"
	payload := map[string]any{
		"max_retries":                4,
		"backoff_first_seconds":      120,
		"backoff_second_seconds":     600,
		"backoff_third_seconds":      1800,
		"rate_limit_backoff_seconds": 2400,
		"max_posts_per_day":          2,
	}
	recorder := performJSONHandlerRequest(t, http.MethodPut, path, payload, app.APIUpdateChannelRetryPolicy)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	stored, found, err := app.Store.GetChannelRetryPolicy(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get stored retry policy: %v", err)
	}
	if !found {
		t.Fatal("expected stored retry policy")
	}
	if stored.MaxRetries != 4 {
		t.Fatalf("expected max_retries=4, got %d", stored.MaxRetries)
	}
	if stored.MaxPostsPerDay == nil || *stored.MaxPostsPerDay != 2 {
		t.Fatalf("expected max_posts_per_day=2, got %+v", stored.MaxPostsPerDay)
	}
}

func TestAPICheckPostGuardrailsIncludesChannelDailyLimitWarning(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)
	maxPostsPerDay := 1
	_, err := app.Store.UpsertChannelRetryPolicy(context.Background(), channel.ID, db.ChannelRetryPolicyInput{
		MaxRetries:              3,
		BackoffFirstSeconds:     60,
		BackoffSecondSeconds:    300,
		BackoffThirdSeconds:     900,
		RateLimitBackoffSeconds: 1800,
		MaxPostsPerDay:          &maxPostsPerDay,
	})
	if err != nil {
		t.Fatalf("upsert retry policy: %v", err)
	}

	existingScheduled := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	existing, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:        "existing",
		Status:      model.StatusScheduled,
		ScheduledAt: ptrTimeForTest(existingScheduled),
	})
	if err != nil {
		t.Fatalf("create existing post: %v", err)
	}
	if err := app.Store.ReplacePostChannels(context.Background(), existing.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("assign channel: %v", err)
	}

	payload := map[string]any{
		"scheduled_at": time.Date(2026, 3, 4, 16, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"channel_ids":  []int64{channel.ID},
	}
	recorder := performJSONHandlerRequest(t, http.MethodPost, "/api/v1/posts/guardrails", payload, app.APICheckPostGuardrails)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Warnings []struct {
			Code string `json:"code"`
		} `json:"warnings"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	foundWarning := false
	for _, item := range response.Warnings {
		if item.Code == "channel_daily_limit" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Fatalf("expected channel_daily_limit warning, got %+v", response.Warnings)
	}
}
