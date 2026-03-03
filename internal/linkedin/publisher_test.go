package linkedin

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"linkedin-cron/internal/publisher"
)

func TestProbeSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v2/userinfo" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("missing bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"abc"}`))
	}))
	t.Cleanup(server.Close)

	pub := NewPublisher(server.URL, "token", "urn:li:person:abc", testLogger())
	if err := pub.Probe(context.Background()); err != nil {
		t.Fatalf("probe should succeed: %v", err)
	}
}

func TestProbeFailureRetryable(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`temporary outage`))
	}))
	t.Cleanup(server.Close)

	pub := NewPublisher(server.URL, "token", "urn:li:person:abc", testLogger())
	err := pub.Probe(context.Background())
	if err == nil {
		t.Fatal("expected probe error")
	}
	if !publisher.IsRetryable(err) {
		t.Fatal("expected retryable probe error")
	}
}

func TestProbeUnconfigured(t *testing.T) {
	t.Parallel()

	pub := NewPublisher("", "", "", testLogger())
	err := pub.Probe(context.Background())
	if err == nil {
		t.Fatal("expected probe error")
	}
	if publisher.IsRetryable(err) {
		t.Fatal("expected non-retryable probe error")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
