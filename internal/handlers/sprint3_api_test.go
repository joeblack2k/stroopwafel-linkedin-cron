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

func TestAPIMediaAssetsCRUD(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)

	createPayload := map[string]any{
		"media_url":  "https://cdn.example.com/hero.jpg",
		"media_type": "image",
		"tags":       []string{"hero", "launch"},
	}
	created := performJSONRequest(t, http.MethodPost, "/api/v1/media/assets", createPayload, app.APICreateMediaAsset)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", created.Code, created.Body.String())
	}
	var createdBody map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	id := int64(createdBody["id"].(float64))

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/media/assets/"+strconv.FormatInt(id, 10), nil)
	getReq.SetPathValue("id", strconv.FormatInt(id, 10))
	getRec := httptest.NewRecorder()
	app.APIGetMediaAsset(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on get, got %d (%s)", getRec.Code, getRec.Body.String())
	}

	updatePayload := map[string]any{
		"tags": []string{"featured"},
	}
	updated := performJSONRequestWithPathValues(t, http.MethodPut, "/api/v1/media/assets/"+strconv.FormatInt(id, 10), updatePayload, app.APIUpdateMediaAsset, map[string]string{"id": strconv.FormatInt(id, 10)})
	if updated.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d (%s)", updated.Code, updated.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/media/assets?tag=featured", nil)
	listRec := httptest.NewRecorder()
	app.APIListMediaAssets(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d (%s)", listRec.Code, listRec.Body.String())
	}

	deletedReq := httptest.NewRequest(http.MethodDelete, "/api/v1/media/assets/"+strconv.FormatInt(id, 10), nil)
	deletedReq.SetPathValue("id", strconv.FormatInt(id, 10))
	deletedRec := httptest.NewRecorder()
	app.APIDeleteMediaAsset(deletedRec, deletedReq)
	if deletedRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d (%s)", deletedRec.Code, deletedRec.Body.String())
	}
}

func TestAPIContentTemplatesCRUD(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	media, err := app.Store.CreateMediaAsset(context.Background(), db.MediaAssetInput{
		MediaURL:  "https://cdn.example.com/template.png",
		MediaType: "image",
		Tags:      []string{"template"},
	})
	if err != nil {
		t.Fatalf("create media asset: %v", err)
	}

	createPayload := map[string]any{
		"name":           "Launch Template",
		"body":           "{{headline}}\n\n{{cta}}",
		"channel_type":   "linkedin",
		"media_asset_id": media.ID,
		"tags":           []string{"launch"},
	}
	created := performJSONRequest(t, http.MethodPost, "/api/v1/templates", createPayload, app.APICreateContentTemplate)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", created.Code, created.Body.String())
	}
	var createdBody map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &createdBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	id := int64(createdBody["id"].(float64))

	updatePayload := map[string]any{
		"name":      "Launch Template v2",
		"body":      "{{headline}}\n{{value}}\n{{cta}}",
		"is_active": false,
	}
	updated := performJSONRequestWithPathValues(t, http.MethodPut, "/api/v1/templates/"+strconv.FormatInt(id, 10), updatePayload, app.APIUpdateContentTemplate, map[string]string{"id": strconv.FormatInt(id, 10)})
	if updated.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d (%s)", updated.Code, updated.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/templates?is_active=false&channel_type=linkedin", nil)
	listRec := httptest.NewRecorder()
	app.APIListContentTemplates(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on list, got %d (%s)", listRec.Code, listRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/templates/"+strconv.FormatInt(id, 10), nil)
	deleteReq.SetPathValue("id", strconv.FormatInt(id, 10))
	deleteRec := httptest.NewRecorder()
	app.APIDeleteContentTemplate(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d (%s)", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestAPIAnalyticsChannelDelivery(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel, err := app.Store.CreateChannel(context.Background(), db.ChannelInput{Type: model.ChannelTypeLinkedIn, DisplayName: "LinkedIn"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	post, err := app.Store.CreatePost(context.Background(), db.PostInput{Text: "analytics post", Status: model.StatusScheduled, ScheduledAt: ptrTimeForTest(time.Now().UTC().Add(20 * time.Minute))})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	if _, err := app.Store.InsertPublishAttempt(context.Background(), db.PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   1,
		AttemptedAt: time.Now().UTC(),
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert attempt: %v", err)
	}

	from := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	to := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/channels?from="+from+"&to="+to, nil)
	rec := httptest.NewRecorder()
	app.APIAnalyticsChannelDelivery(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("expected items array, got %#v", body["items"])
	}
	if len(items) == 0 {
		t.Fatalf("expected at least one channel stat row")
	}
}

func performJSONRequestWithPathValues(t *testing.T, method, target string, payload any, handler func(http.ResponseWriter, *http.Request), pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for key, value := range pathValues {
		request.SetPathValue(key, value)
	}
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}
