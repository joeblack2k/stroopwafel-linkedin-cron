package handlers

import (
	"bytes"
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

func TestAPIListAuditEventsSupportsFiltersAndPagination(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)

	createdAt := time.Date(2026, 3, 4, 9, 0, 0, 0, time.UTC)
	_, err := app.Store.CreateAuditEvent(context.Background(), db.AuditEventInput{
		Action:    "channels.bulk_enable",
		Resource:  "channels",
		AuthActor: "api-key:1",
		Source:    "api",
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create first audit event: %v", err)
	}
	_, err = app.Store.CreateAuditEvent(context.Background(), db.AuditEventInput{
		Action:    "channels.bulk_disable",
		Resource:  "channels",
		AuthActor: "api-key:2",
		Source:    "api",
		CreatedAt: createdAt.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create second audit event: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit-events?action=channels.bulk_enable&limit=10&offset=0", nil)
	recorder := httptest.NewRecorder()
	app.APIListAuditEvents(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Pagination.Total != 1 {
		t.Fatalf("expected filtered total=1, got %d", payload.Pagination.Total)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected one filtered item, got %d", len(payload.Items))
	}
	if payload.Items[0].Action != "channels.bulk_enable" {
		t.Fatalf("expected channels.bulk_enable action, got %q", payload.Items[0].Action)
	}
}

func TestAPIListPublishAttemptsGlobalSupportsFilters(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channelA := mustCreateDryRunChannel(t, app.Store)
	channelB := mustCreateDryRunChannel(t, app.Store)

	postA, err := app.Store.CreatePost(context.Background(), db.PostInput{Text: "A", Status: model.StatusScheduled, ScheduledAt: ptrTimeForTest(time.Now().UTC().Add(5 * time.Minute))})
	if err != nil {
		t.Fatalf("create post A: %v", err)
	}
	postB, err := app.Store.CreatePost(context.Background(), db.PostInput{Text: "B", Status: model.StatusScheduled, ScheduledAt: ptrTimeForTest(time.Now().UTC().Add(10 * time.Minute))})
	if err != nil {
		t.Fatalf("create post B: %v", err)
	}

	base := time.Date(2026, 3, 4, 8, 0, 0, 0, time.UTC)
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      postA.ID,
		ChannelID:   channelA.ID,
		AttemptNo:   1,
		AttemptedAt: base,
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert attempt A: %v", err)
	}
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      postB.ID,
		ChannelID:   channelB.ID,
		AttemptNo:   1,
		AttemptedAt: base.Add(2 * time.Minute),
		Status:      model.PublishAttemptStatusFailed,
	}); err != nil {
		t.Fatalf("insert attempt B: %v", err)
	}

	url := "/api/v1/publish-attempts?status=failed&channel_id=" + strconv.FormatInt(channelB.ID, 10)
	request := httptest.NewRequest(http.MethodGet, url, nil)
	recorder := httptest.NewRecorder()
	app.APIListPublishAttempts(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items []struct {
			ChannelID int64  `json:"channel_id"`
			Status    string `json:"status"`
		} `json:"items"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Pagination.Total != 1 {
		t.Fatalf("expected filtered total=1, got %d", payload.Pagination.Total)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected one attempt item, got %d", len(payload.Items))
	}
	if payload.Items[0].ChannelID != channelB.ID {
		t.Fatalf("expected channel_id=%d, got %d", channelB.ID, payload.Items[0].ChannelID)
	}
	if payload.Items[0].Status != model.PublishAttemptStatusFailed {
		t.Fatalf("expected failed status, got %q", payload.Items[0].Status)
	}
}

func TestAPIListChannelHealthReturnsConfiguredAndCounters(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	configured, err := app.Store.CreateChannel(context.Background(), db.ChannelInput{
		Type:                model.ChannelTypeLinkedIn,
		DisplayName:         "LinkedIn Main",
		LinkedInAccessToken: ptrString("token"),
		LinkedInAuthorURN:   ptrString("urn:li:person:1"),
	})
	if err != nil {
		t.Fatalf("create configured channel: %v", err)
	}
	unconfigured, err := app.Store.CreateChannel(context.Background(), db.ChannelInput{
		Type:        model.ChannelTypeLinkedIn,
		DisplayName: "LinkedIn Missing",
	})
	if err != nil {
		t.Fatalf("create unconfigured channel: %v", err)
	}

	post, err := app.Store.CreatePost(context.Background(), db.PostInput{Text: "health", Status: model.StatusDraft})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	base := time.Date(2026, 3, 4, 7, 0, 0, 0, time.UTC)
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   configured.ID,
		AttemptNo:   1,
		AttemptedAt: base,
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert sent attempt: %v", err)
	}
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   configured.ID,
		AttemptNo:   2,
		AttemptedAt: base.Add(1 * time.Minute),
		Status:      model.PublishAttemptStatusRetry,
	}); err != nil {
		t.Fatalf("insert retry attempt: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/channels/health", nil)
	recorder := httptest.NewRecorder()
	app.APIListChannelHealth(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items []struct {
			ChannelID   int64 `json:"channel_id"`
			Configured  bool  `json:"configured"`
			SentCount   int   `json:"sent_count"`
			RetryCount  int   `json:"retry_count"`
			FailedCount int   `json:"failed_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(payload.Items) < 2 {
		t.Fatalf("expected at least two channels, got %d", len(payload.Items))
	}

	foundConfigured := false
	foundUnconfigured := false
	for _, item := range payload.Items {
		if item.ChannelID == configured.ID {
			foundConfigured = true
			if !item.Configured {
				t.Fatalf("expected configured channel to be configured")
			}
			if item.SentCount != 1 || item.RetryCount != 1 || item.FailedCount != 0 {
				t.Fatalf("unexpected counters for configured channel: %+v", item)
			}
		}
		if item.ChannelID == unconfigured.ID {
			foundUnconfigured = true
			if item.Configured {
				t.Fatalf("expected unconfigured channel to be configured=false")
			}
		}
	}

	if !foundConfigured {
		t.Fatalf("configured channel %d not found in health payload", configured.ID)
	}
	if !foundUnconfigured {
		t.Fatalf("unconfigured channel %d not found in health payload", unconfigured.ID)
	}
}

func TestAPIBulkChannelStatusAndAuditTrail(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)

	disableRecorder := performJSONAuditTrailRequest(t, app, http.MethodPost, "/api/v1/channels/bulk/disable", map[string]any{
		"channel_ids": []int64{channel.ID},
	}, app.APIBulkDisableChannels)
	if disableRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for bulk disable, got %d (%s)", disableRecorder.Code, disableRecorder.Body.String())
	}

	updated, err := app.Store.GetChannel(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("reload channel after disable: %v", err)
	}
	if updated.Status != model.ChannelStatusDisabled {
		t.Fatalf("expected disabled status, got %s", updated.Status)
	}

	enableRecorder := performJSONAuditTrailRequest(t, app, http.MethodPost, "/api/v1/channels/bulk/enable", map[string]any{
		"channel_ids": []int64{channel.ID},
	}, app.APIBulkEnableChannels)
	if enableRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for bulk enable, got %d (%s)", enableRecorder.Code, enableRecorder.Body.String())
	}

	updated, err = app.Store.GetChannel(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("reload channel after enable: %v", err)
	}
	if updated.Status != model.ChannelStatusActive {
		t.Fatalf("expected active status, got %s", updated.Status)
	}

	enableCount, err := app.Store.CountAuditEvents(context.Background(), db.AuditEventFilter{Action: "channels.bulk_enable", Source: "api"})
	if err != nil {
		t.Fatalf("count bulk enable audit events: %v", err)
	}
	if enableCount < 1 {
		t.Fatalf("expected at least one channels.bulk_enable audit event, got %d", enableCount)
	}

	disableCount, err := app.Store.CountAuditEvents(context.Background(), db.AuditEventFilter{Action: "channels.bulk_disable", Source: "api"})
	if err != nil {
		t.Fatalf("count bulk disable audit events: %v", err)
	}
	if disableCount < 1 {
		t.Fatalf("expected at least one channels.bulk_disable audit event, got %d", disableCount)
	}
}

func performJSONAuditTrailRequest(t *testing.T, app *App, method, path string, payload any, handler func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(request.Context(), contextKeyAuthMethod, "basic"))
	request = request.WithContext(context.WithValue(request.Context(), contextKeyAuthUser, "admin"))

	recorder := httptest.NewRecorder()
	auditWrapped := app.WithAPIAuditTrail(http.HandlerFunc(handler))
	auditWrapped.ServeHTTP(recorder, request)
	return recorder
}
