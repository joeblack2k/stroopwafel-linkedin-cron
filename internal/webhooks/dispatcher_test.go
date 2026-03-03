package webhooks

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestDispatcherDeliversWebhookEvent(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		received []EventEnvelope
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		var envelope EventEnvelope
		if err := json.Unmarshal(body, &envelope); err == nil {
			mu.Lock()
			received = append(received, envelope)
			mu.Unlock()
		}
		if signature := strings.TrimSpace(r.Header.Get("X-Stroopwafel-Signature")); signature == "" {
			t.Fatalf("expected signature header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dispatcher := NewDispatcher([]string{server.URL}, "super-secret", "test", slog.New(slog.NewJSONHandler(io.Discard, nil)))
	dispatcher.Emit(context.Background(), "publish.attempt.created", map[string]any{"post_id": int64(1)})

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected one received event, got %d", len(received))
	}
	if received[0].Event != "publish.attempt.created" {
		t.Fatalf("expected event name publish.attempt.created, got %q", received[0].Event)
	}
	if received[0].Payload["post_id"] == nil {
		t.Fatalf("expected payload post_id field")
	}
}
