package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestAPIListPostAttemptsReturnsPaginatedAttempts(t *testing.T) {
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

	firstAttemptAt := time.Now().UTC().Add(-3 * time.Minute)
	secondAttemptAt := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   1,
		AttemptedAt: firstAttemptAt,
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert first attempt: %v", err)
	}
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   2,
		AttemptedAt: secondAttemptAt,
		Status:      model.PublishAttemptStatusRetry,
	}); err != nil {
		t.Fatalf("insert second attempt: %v", err)
	}

	path := "/api/v1/posts/" + strconv.FormatInt(post.ID, 10) + "/attempts?limit=1&offset=1"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("id", strconv.FormatInt(post.ID, 10))
	recorder := httptest.NewRecorder()
	app.APIListPostAttempts(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items      []map[string]any `json:"items"`
		Pagination struct {
			Limit   int  `json:"limit"`
			Offset  int  `json:"offset"`
			Total   int  `json:"total"`
			HasNext bool `json:"has_next"`
			HasPrev bool `json:"has_prev"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected exactly one attempt entry on page, got %d", len(payload.Items))
	}
	if got := payload.Items[0]["status"]; got == nil || strings.TrimSpace(got.(string)) == "" {
		t.Fatalf("expected attempt status, got %+v", payload.Items[0])
	}
	if payload.Pagination.Total != 2 {
		t.Fatalf("expected pagination total=2, got %d", payload.Pagination.Total)
	}
	if payload.Pagination.Limit != 1 || payload.Pagination.Offset != 1 {
		t.Fatalf("unexpected pagination window: %+v", payload.Pagination)
	}
	if !payload.Pagination.HasPrev {
		t.Fatalf("expected has_prev=true")
	}
	if payload.Pagination.HasNext {
		t.Fatalf("expected has_next=false on last page")
	}
}

func TestAPIListPostAttemptsDateRangeFilter(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)

	now := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	post, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:        "attempt range",
		Status:      model.StatusScheduled,
		ScheduledAt: ptrTimeForTest(now.Add(-1 * time.Minute)),
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := app.Store.ReplacePostChannels(context.Background(), post.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("assign channels: %v", err)
	}

	attemptOne := now.Add(-2 * time.Hour)
	attemptTwo := now.Add(-1 * time.Hour)
	attemptThree := now
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   1,
		AttemptedAt: attemptOne,
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert attempt 1: %v", err)
	}
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   2,
		AttemptedAt: attemptTwo,
		Status:      model.PublishAttemptStatusRetry,
	}); err != nil {
		t.Fatalf("insert attempt 2: %v", err)
	}
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   3,
		AttemptedAt: attemptThree,
		Status:      model.PublishAttemptStatusFailed,
	}); err != nil {
		t.Fatalf("insert attempt 3: %v", err)
	}

	attemptedFrom := now.Add(-90 * time.Minute).Format(time.RFC3339)
	attemptedTo := now.Add(-30 * time.Minute).Format(time.RFC3339)
	path := "/api/v1/posts/" + strconv.FormatInt(post.ID, 10) + "/attempts?attempted_from=" + attemptedFrom + "&attempted_to=" + attemptedTo
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("id", strconv.FormatInt(post.ID, 10))
	recorder := httptest.NewRecorder()
	app.APIListPostAttempts(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items []struct {
			AttemptNo int `json:"attempt_no"`
		} `json:"items"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Pagination.Total != 1 {
		t.Fatalf("expected filtered total=1, got %d", payload.Pagination.Total)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected one filtered attempt, got %d", len(payload.Items))
	}
	if payload.Items[0].AttemptNo != 2 {
		t.Fatalf("expected attempt_no=2 in filtered result, got %d", payload.Items[0].AttemptNo)
	}
}

func TestAPIListPostAttemptsRejectsInvalidDateRange(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	post, err := app.Store.CreatePost(context.Background(), db.PostInput{Text: "range validation", Status: model.StatusDraft})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	path := "/api/v1/posts/" + strconv.FormatInt(post.ID, 10) + "/attempts?attempted_from=2026-03-03T12:00:00Z&attempted_to=2026-03-03T11:00:00Z"
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.SetPathValue("id", strconv.FormatInt(post.ID, 10))
	recorder := httptest.NewRecorder()
	app.APIListPostAttempts(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid range, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	errMessage, _ := payload["error"].(string)
	if !strings.Contains(errMessage, "attempted_from") {
		t.Fatalf("expected attempted_from validation error, got %q", errMessage)
	}
}

func TestAPIListChannelAuditEvents(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	created, err := app.Store.CreateChannel(context.Background(), db.ChannelInput{
		Type:                model.ChannelTypeLinkedIn,
		DisplayName:         "LinkedIn Main",
		LinkedInAccessToken: ptrString("token-old"),
		LinkedInAuthorURN:   ptrString("urn:li:organization:123"),
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	path := "/api/v1/channels/" + strconv.FormatInt(created.ID, 10)
	replacePayload := map[string]any{
		"display_name":                 "LinkedIn Updated",
		"linkedin_access_token_action": "replace",
		"linkedin_access_token":        "token-new",
		"linkedin_author_urn":          "urn:li:organization:123",
	}
	recorder := performJSONHandlerRequest(t, http.MethodPut, path, replacePayload, app.APIUpdateChannel)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for token replace, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	clearPayload := map[string]any{
		"linkedin_access_token_action": "clear",
	}
	recorder = performJSONHandlerRequest(t, http.MethodPut, path, clearPayload, app.APIUpdateChannel)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for token clear, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	auditPath := "/api/v1/channels/" + strconv.FormatInt(created.ID, 10) + "/audit?limit=1&offset=0"
	request := httptest.NewRequest(http.MethodGet, auditPath, nil)
	request.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	recorder = httptest.NewRecorder()
	app.APIListChannelAuditEvents(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for channel audit, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items []struct {
			EventType string  `json:"event_type"`
			Actor     string  `json:"actor"`
			Metadata  *string `json:"metadata"`
		} `json:"items"`
		Pagination struct {
			Total   int  `json:"total"`
			HasNext bool `json:"has_next"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode audit payload: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected one audit item due to limit=1, got %d", len(payload.Items))
	}
	if payload.Items[0].EventType != "channel.updated" {
		t.Fatalf("expected event_type=channel.updated, got %q", payload.Items[0].EventType)
	}
	if strings.TrimSpace(payload.Items[0].Actor) == "" {
		t.Fatalf("expected non-empty actor")
	}
	if payload.Items[0].Metadata == nil || strings.TrimSpace(*payload.Items[0].Metadata) == "" {
		t.Fatalf("expected metadata in audit item")
	}
	if payload.Pagination.Total < 2 {
		t.Fatalf("expected at least 2 audit events, got %d", payload.Pagination.Total)
	}
	if !payload.Pagination.HasNext {
		t.Fatalf("expected has_next=true for first page")
	}
}

func TestAPIListChannelsIncludesSecretPreviewMetadata(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	_, err := app.Store.CreateChannel(context.Background(), db.ChannelInput{
		Type:                    model.ChannelTypeLinkedIn,
		DisplayName:             "LinkedIn Main",
		LinkedInAccessToken:     ptrString("token-123456"),
		LinkedInAuthorURN:       ptrString("urn:li:organization:123456"),
		LinkedInAPIBaseURL:      ptrString("https://api.linkedin.com"),
		FacebookPageAccessToken: ptrString("fb-token-abcdef"),
		FacebookPageID:          ptrString("123456789"),
		FacebookAPIBaseURL:      ptrString("https://graph.facebook.com/v22.0"),
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/channels", nil)
	recorder := httptest.NewRecorder()
	app.APIListChannels(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode channels response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 channel in response, got %d", len(payload))
	}
	item := payload[0]

	if _, exists := item["linkedin_access_token"]; exists {
		t.Fatalf("raw linkedin_access_token must not be present in API response")
	}
	if _, exists := item["facebook_page_access_token"]; exists {
		t.Fatalf("raw facebook_page_access_token must not be present in API response")
	}

	secretPreview, ok := item["secret_preview"].(map[string]any)
	if !ok {
		t.Fatalf("expected secret_preview object in response, got %#v", item["secret_preview"])
	}
	linkedInMasked, _ := secretPreview["linkedin_access_token_masked"].(string)
	if linkedInMasked == "" {
		t.Fatalf("expected linkedin_access_token_masked to be present")
	}
	if linkedInMasked == "token-123456" {
		t.Fatalf("expected linkedin_access_token_masked to hide raw token")
	}
	facebookMasked, _ := secretPreview["facebook_page_access_token_masked"].(string)
	if facebookMasked == "" {
		t.Fatalf("expected facebook_page_access_token_masked to be present")
	}
	if facebookMasked == "fb-token-abcdef" {
		t.Fatalf("expected facebook_page_access_token_masked to hide raw token")
	}

	secretPresence, ok := item["secret_presence"].(map[string]any)
	if !ok {
		t.Fatalf("expected secret_presence object in response, got %#v", item["secret_presence"])
	}
	linkedInTokenPresent, _ := secretPresence["linkedin_access_token_present"].(bool)
	if !linkedInTokenPresent {
		t.Fatalf("expected linkedin_access_token_present=true")
	}
	facebookTokenPresent, _ := secretPresence["facebook_page_access_token_present"].(bool)
	if !facebookTokenPresent {
		t.Fatalf("expected facebook_page_access_token_present=true")
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
	if strings.HasPrefix(path, "/api/v1/channels/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/v1/channels/"), "/")
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

func TestAPIUpdateChannelSecretActions(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	created, err := app.Store.CreateChannel(context.Background(), db.ChannelInput{
		Type:                model.ChannelTypeLinkedIn,
		DisplayName:         "LinkedIn Main",
		LinkedInAccessToken: ptrString("token-old"),
		LinkedInAuthorURN:   ptrString("urn:li:organization:123"),
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	path := "/api/v1/channels/" + strconv.FormatInt(created.ID, 10)

	missingTokenPayload := map[string]any{
		"linkedin_access_token_action": "replace",
	}
	recorder := performJSONHandlerRequest(t, http.MethodPut, path, missingTokenPayload, app.APIUpdateChannel)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for replace without token, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	replacePayload := map[string]any{
		"display_name":                 "LinkedIn Updated",
		"linkedin_access_token_action": "replace",
		"linkedin_access_token":        "token-new",
		"linkedin_author_urn":          "urn:li:organization:123",
	}
	recorder = performJSONHandlerRequest(t, http.MethodPut, path, replacePayload, app.APIUpdateChannel)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for token replace, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	clearPayload := map[string]any{
		"linkedin_access_token_action": "clear",
	}
	recorder = performJSONHandlerRequest(t, http.MethodPut, path, clearPayload, app.APIUpdateChannel)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 for token clear, got %d (%s)", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	configured, _ := response["linkedin_configured"].(bool)
	if configured {
		t.Fatalf("expected linkedin_configured=false after clear")
	}

	secretPreview, ok := response["secret_preview"].(map[string]any)
	if !ok {
		t.Fatalf("expected secret_preview object in response, got %#v", response["secret_preview"])
	}
	maskedToken, _ := secretPreview["linkedin_access_token_masked"].(string)
	if maskedToken != "" {
		t.Fatalf("expected cleared linkedin_access_token_masked to be empty, got %q", maskedToken)
	}

	secretPresence, ok := response["secret_presence"].(map[string]any)
	if !ok {
		t.Fatalf("expected secret_presence object in response, got %#v", response["secret_presence"])
	}
	linkedInTokenPresent, _ := secretPresence["linkedin_access_token_present"].(bool)
	if linkedInTokenPresent {
		t.Fatalf("expected linkedin_access_token_present=false after clear")
	}
}

func ptrString(value string) *string {
	copyValue := value
	return &copyValue
}

func TestAPIReschedulePostDraftTransitionsToScheduled(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	post, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:   "draft post",
		Status: model.StatusDraft,
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	payload := map[string]any{
		"scheduled_at": time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
	}
	path := "/api/v1/posts/" + strconv.FormatInt(post.ID, 10) + "/reschedule"
	recorder := performJSONHandlerRequest(t, http.MethodPost, path, payload, app.APIReschedulePost)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	updated, err := app.Store.GetPost(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("reload post: %v", err)
	}
	if updated.Status != model.StatusScheduled {
		t.Fatalf("expected status scheduled, got %s", updated.Status)
	}
	if updated.ScheduledAt == nil {
		t.Fatal("expected scheduled_at to be set")
	}
}

func TestAPISendAndDeletePost(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)
	post, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:        "send and delete",
		Status:      model.StatusScheduled,
		ScheduledAt: ptrTimeForTest(time.Now().UTC().Add(-1 * time.Minute)),
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if err := app.Store.ReplacePostChannels(context.Background(), post.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("replace channels: %v", err)
	}

	path := "/api/v1/posts/" + strconv.FormatInt(post.ID, 10) + "/send-and-delete"
	recorder := performJSONHandlerRequest(t, http.MethodPost, path, map[string]any{}, app.APISendAndDeletePost)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	if _, err := app.Store.GetPost(context.Background(), post.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("expected deleted post, got err=%v", err)
	}
}

func TestAPICreateBotHandoff(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	recorder := performJSONHandlerRequest(t, http.MethodPost, "/api/v1/settings/bot-handoff", map[string]any{"name": "bot-ui"}, app.APICreateBotHandoff)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	apiKey, _ := payload["api_key"].(string)
	if !strings.HasPrefix(apiKey, "lcak_") {
		t.Fatalf("expected api key prefix lcak_, got %q", apiKey)
	}
	instructions, _ := payload["instructions"].(string)
	if !strings.Contains(instructions, "/api/v1/posts") {
		t.Fatalf("expected handoff instructions to mention posts endpoint")
	}
}
