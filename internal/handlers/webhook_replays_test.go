package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"linkedin-cron/internal/db"
)

func TestAPIListWebhookReplays(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	if _, err := app.Store.InsertWebhookReplay(context.Background(), db.WebhookReplayInput{
		EventID:   "evt_failed",
		EventName: "post.state.changed",
		TargetURL: "https://example.com/hook",
		Source:    "scheduler",
		Payload:   `{"post_id":1}`,
		Status:    db.WebhookReplayStatusFailed,
	}); err != nil {
		t.Fatalf("insert replay: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/replays?status=failed", nil)
	recorder := httptest.NewRecorder()
	app.APIListWebhookReplays(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Items      []map[string]any `json:"items"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Pagination.Total != 1 || len(payload.Items) != 1 {
		t.Fatalf("unexpected replay list payload: %+v", payload)
	}
}

func TestAPIReplayWebhookMarksDelivered(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	replay, err := app.Store.InsertWebhookReplay(context.Background(), db.WebhookReplayInput{
		EventID:   "evt_retry",
		EventName: "publish.attempt.created",
		TargetURL: target.URL,
		Source:    "server",
		Payload:   `{"post_id":123}`,
		Status:    db.WebhookReplayStatusFailed,
	})
	if err != nil {
		t.Fatalf("insert replay: %v", err)
	}

	path := "/api/v1/webhooks/replays/" + strconv.FormatInt(replay.ID, 10) + "/replay"
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.SetPathValue("id", strconv.FormatInt(replay.ID, 10))
	recorder := httptest.NewRecorder()
	app.APIReplayWebhook(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	updated, err := app.Store.GetWebhookReplay(context.Background(), replay.ID)
	if err != nil {
		t.Fatalf("get updated replay: %v", err)
	}
	if updated.Status != db.WebhookReplayStatusDelivered {
		t.Fatalf("expected replay status delivered, got %q", updated.Status)
	}
	if updated.AttemptCount != 1 {
		t.Fatalf("expected attempt_count 1, got %d", updated.AttemptCount)
	}

	deliveries, err := app.Store.ListRecentWebhookDeliveries(context.Background(), 20)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) == 0 {
		t.Fatalf("expected persisted webhook delivery after replay")
	}
}

func TestAPICancelWebhookReplay(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	replay, err := app.Store.InsertWebhookReplay(context.Background(), db.WebhookReplayInput{
		EventID:   "evt_cancel",
		EventName: "post.state.changed",
		TargetURL: "https://example.com/hook",
		Source:    "server",
		Payload:   `{"post_id":456}`,
		Status:    db.WebhookReplayStatusQueued,
	})
	if err != nil {
		t.Fatalf("insert replay: %v", err)
	}

	path := "/api/v1/webhooks/replays/" + strconv.FormatInt(replay.ID, 10) + "/cancel"
	request := httptest.NewRequest(http.MethodPost, path, nil)
	request.SetPathValue("id", strconv.FormatInt(replay.ID, 10))
	recorder := httptest.NewRecorder()
	app.APICancelWebhookReplay(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	updated, err := app.Store.GetWebhookReplay(context.Background(), replay.ID)
	if err != nil {
		t.Fatalf("get replay: %v", err)
	}
	if updated.Status != db.WebhookReplayStatusCancelled {
		t.Fatalf("expected cancelled status, got %q", updated.Status)
	}
}
