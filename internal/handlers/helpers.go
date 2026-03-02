package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"linkedin-cron/internal/config"
	"linkedin-cron/internal/db"
	"linkedin-cron/internal/scheduler"
	"linkedin-cron/internal/views"
)

type App struct {
	Config             config.Config
	Store              *db.Store
	Scheduler          *scheduler.Service
	Renderer           *views.Renderer
	Logger             *slog.Logger
	MigrationStatus    string
	RequestedPublisher string
	ActivePublisher    string
	LinkedInConfigured bool
}

type SettingsStatus struct {
	BasicAuthConfigured bool   `json:"basic_auth_configured"`
	MaskedAuthUser      string `json:"masked_auth_user"`
	MaskedAuthPass      string `json:"masked_auth_pass"`
	PublisherMode       string `json:"publisher_mode"`
	RequestedMode       string `json:"requested_mode"`
	LinkedInConfigured  bool   `json:"linkedin_configured"`
	MaskedLinkedInToken string `json:"masked_linkedin_token"`
	MaskedAuthorURN     string `json:"masked_author_urn"`
	DBPath              string `json:"db_path"`
	Timezone            string `json:"timezone"`
	MigrationStatus     string `json:"migration_status"`
}

func BasicAuthMiddleware(username, password string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			if !ok {
				w.Header().Set("WWW-Authenticate", `Basic realm="linkedin-cron"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			userOK := subtle.ConstantTimeCompare([]byte(user), []byte(username)) == 1
			passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
			if !(userOK && passOK) {
				logger.LogAttrs(
					r.Context(),
					slog.LevelWarn,
					"basic auth failed",
					slog.String("component", "http"),
					slog.String("path", r.URL.Path),
				)
				w.Header().Set("WWW-Authenticate", `Basic realm="linkedin-cron"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (a *App) settingsStatus() SettingsStatus {
	return SettingsStatus{
		BasicAuthConfigured: a.Config.BasicAuthConfigured(),
		MaskedAuthUser:      config.MaskSecret(a.Config.BasicAuthUser),
		MaskedAuthPass:      config.MaskSecret(a.Config.BasicAuthPass),
		PublisherMode:       a.ActivePublisher,
		RequestedMode:       a.RequestedPublisher,
		LinkedInConfigured:  a.LinkedInConfigured,
		MaskedLinkedInToken: config.MaskSecret(a.Config.LinkedInToken),
		MaskedAuthorURN:     config.MaskSecret(a.Config.LinkedInAuthorURN),
		DBPath:              a.Config.DBPath,
		Timezone:            a.Config.Timezone,
		MigrationStatus:     a.MigrationStatus,
	}
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request, mode string) {
	err := a.Store.Ping(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"mode":           mode,
		"db_ok":          err == nil,
		"publisher_mode": a.ActivePublisher,
	})
}

func parseID(pathValue string) (int64, error) {
	id, err := strconv.ParseInt(pathValue, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

func parseDateTimeLocal(value string, location *time.Location) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if location == nil {
		location = time.UTC
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", trimmed, location)
	if err != nil {
		return nil, fmt.Errorf("parse datetime-local value: %w", err)
	}
	utc := parsed.UTC()
	return &utc, nil
}

func parseRFC3339(value string) (*time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse RFC3339 value: %w", err)
	}
	utc := parsed.UTC()
	return &utc, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readJSONBody(r *http.Request, out any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("body must contain a single JSON object")
	}
	return nil
}
