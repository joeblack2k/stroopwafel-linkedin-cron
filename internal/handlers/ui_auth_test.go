package handlers

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"linkedin-cron/internal/config"
)

func TestUISessionTokenRoundTrip(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	token, _, err := issueUISessionToken("admin", "secret", now)
	if err != nil {
		t.Fatalf("issue session token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/calendar", nil)
	req.AddCookie(&http.Cookie{Name: uiSessionCookieName, Value: token})

	user, ok := validateUISessionCookie(req, "admin", "secret", now.Add(time.Minute))
	if !ok {
		t.Fatal("expected valid session cookie")
	}
	if user != "admin" {
		t.Fatalf("expected user admin, got %q", user)
	}

	if _, ok := validateUISessionCookie(req, "admin", "wrong", now.Add(time.Minute)); ok {
		t.Fatal("expected invalid session when secret changes")
	}
}

func TestUIAuthMiddlewareRedirectsToLogin(t *testing.T) {
	t.Parallel()

	cfg := config.Config{BasicAuthUser: "admin", BasicAuthPass: "secret"}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	middleware := UIAuthMiddleware(cfg, logger)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/calendar?view=week", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect status 303, got %d", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/login?") {
		t.Fatalf("expected login redirect, got %q", location)
	}
	if !strings.Contains(location, "next=%2Fcalendar%3Fview%3Dweek") {
		t.Fatalf("expected encoded next path in redirect, got %q", location)
	}
}

func TestUIAuthMiddlewareWithSessionCookie(t *testing.T) {
	t.Parallel()

	cfg := config.Config{BasicAuthUser: "admin", BasicAuthPass: "secret"}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	middleware := UIAuthMiddleware(cfg, logger)
	token, _, err := issueUISessionToken("admin", "secret", time.Now().UTC())
	if err != nil {
		t.Fatalf("issue session token: %v", err)
	}

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authMethodFromContext(r.Context()) != "session" {
			t.Fatalf("expected auth method session, got %s", authMethodFromContext(r.Context()))
		}
		if authUserFromContext(r.Context()) != "admin" {
			t.Fatalf("expected auth user admin, got %q", authUserFromContext(r.Context()))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/calendar", nil)
	req.AddCookie(&http.Cookie{Name: uiSessionCookieName, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}
