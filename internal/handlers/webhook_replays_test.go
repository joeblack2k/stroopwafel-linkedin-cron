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

func TestAPIListWebhookDeadLetters(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)

	if _, err := app.Store.InsertWebhookReplay(context.Background(), db.WebhookReplayInput{
		EventID:      "evt_dead",
		EventName:    "post.state.changed",
		TargetURL:    "https://example.com/hook",
		Source:       "scheduler",
		Payload:      `{"post_id":99}`,
		Status:       db.WebhookReplayStatusFailed,
		AttemptCount: 3,
	}); err != nil {
		t.Fatalf("insert dead-letter replay: %v", err)
	}

	nextAttempt := ptrTimeForTest(time.Now().UTC().Add(10 * time.Minute))
	if _, err := app.Store.InsertWebhookReplay(context.Background(), db.WebhookReplayInput{
		EventID:      "evt_retryable",
		EventName:    "post.state.changed",
		TargetURL:    "https://example.com/hook",
		Source:       "scheduler",
		Payload:      `{"post_id":100}`,
		Status:       db.WebhookReplayStatusFailed,
		AttemptCount: 4,
		NextAttempt:  nextAttempt,
	}); err != nil {
		t.Fatalf("insert retryable replay: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/dead-letters?min_attempts=3", nil)
	recorder := httptest.NewRecorder()
	app.APIListWebhookDeadLetters(recorder, request)
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
		t.Fatalf("unexpected dead-letter payload: %+v", payload)
	}

	if _, ok := payload.Items[0]["id"].(float64); !ok {
		t.Fatalf("expected id in dead-letter item")
	}
}

func TestAPIWebhookDeadLetterAlerts(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)

	for index := 0; index < 2; index++ {
		if _, err := app.Store.InsertWebhookReplay(context.Background(), db.WebhookReplayInput{
			EventID:      "evt_alert_" + strconv.Itoa(index),
			EventName:    "post.state.changed",
			TargetURL:    "https://example.com/hook-alert",
			Source:       "scheduler",
			Payload:      `{"post_id":123}`,
			Status:       db.WebhookReplayStatusFailed,
			AttemptCount: 3,
		}); err != nil {
			t.Fatalf("insert dead-letter replay %d: %v", index, err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks/dead-letters/alerts?threshold=2&min_attempts=3", nil)
	recorder := httptest.NewRecorder()
	app.APIWebhookDeadLetterAlerts(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Threshold   int  `json:"threshold"`
		Current     int  `json:"current"`
		Alert       bool `json:"alert"`
		MinAttempts int  `json:"min_attempts"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode dead-letter alerts payload: %v", err)
	}
	if payload.Threshold != 2 {
		t.Fatalf("expected threshold=2, got %d", payload.Threshold)
	}
	if payload.MinAttempts != 3 {
		t.Fatalf("expected min_attempts=3, got %d", payload.MinAttempts)
	}
	if payload.Current != 2 {
		t.Fatalf("expected current=2, got %d", payload.Current)
	}
	if !payload.Alert {
		t.Fatalf("expected alert=true")
	}
}
