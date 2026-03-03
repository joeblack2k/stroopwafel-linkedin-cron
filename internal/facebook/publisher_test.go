package facebook

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linkedin-cron/internal/model"
	"linkedin-cron/internal/publisher"
)

func TestPublishSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/123456/feed" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("message"); got != "hello facebook" {
			t.Fatalf("unexpected message: %s", got)
		}
		if got := r.Form.Get("link"); got != "https://example.com/post" {
			t.Fatalf("unexpected link: %s", got)
		}
		if got := r.Form.Get("access_token"); got != "page-token" {
			t.Fatalf("unexpected access token: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"123456_7890"}`))
	}))
	t.Cleanup(server.Close)

	pub := NewPublisher(server.URL, "page-token", "123456", testLogger())
	if !pub.Configured() {
		t.Fatal("expected publisher to be configured")
	}

	mediaURL := "https://example.com/post"
	result, err := pub.Publish(context.Background(), model.Post{ID: 11, Text: "hello facebook", MediaURL: &mediaURL})
	if err != nil {
		t.Fatalf("publish should succeed: %v", err)
	}
	if result.ExternalID != "123456_7890" {
		t.Fatalf("unexpected external ID: %s", result.ExternalID)
	}
}

func TestPublishFailureRetryable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"transient upstream error"}}`))
	}))
	t.Cleanup(server.Close)

	pub := NewPublisher(server.URL, "page-token", "123456", testLogger())
	_, err := pub.Publish(context.Background(), model.Post{ID: 12, Text: "hello"})
	if err == nil {
		t.Fatal("expected publish error")
	}
	if !publisher.IsRetryable(err) {
		t.Fatal("expected error to be retryable")
	}
	if !strings.Contains(err.Error(), "transient upstream error") {
		t.Fatalf("expected parsed API error message, got %q", err.Error())
	}
}

func TestPublishFailureTerminal(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token"}}`))
	}))
	t.Cleanup(server.Close)

	pub := NewPublisher(server.URL, "bad-token", "123456", testLogger())
	_, err := pub.Publish(context.Background(), model.Post{ID: 13, Text: "hello"})
	if err == nil {
		t.Fatal("expected publish error")
	}
	if publisher.IsRetryable(err) {
		t.Fatal("expected error to be terminal")
	}
}

func TestPublishUnconfigured(t *testing.T) {
	t.Parallel()

	pub := NewPublisher("", "", "", testLogger())
	if pub.Configured() {
		t.Fatal("expected publisher to be unconfigured")
	}
	_, err := pub.Publish(context.Background(), model.Post{ID: 14, Text: "hello"})
	if err == nil {
		t.Fatal("expected error for unconfigured publisher")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not configured error, got %q", err.Error())
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
