package handlers

import (
	"context"
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
	"linkedin-cron/internal/model"
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
	FacebookConfigured bool
}

type contextKey string

const (
	contextKeyAuthMethod contextKey = "auth_method"
	contextKeyAPIKeyID   contextKey = "api_key_id"
	contextKeyAPIKeyName contextKey = "api_key_name"
)

type SettingsStatus struct {
	BasicAuthConfigured bool   `json:"basic_auth_configured"`
	MaskedAuthUser      string `json:"masked_auth_user"`
	MaskedAuthPass      string `json:"masked_auth_pass"`
	StaticAPIKeysCount  int    `json:"static_api_keys_count"`
	PublisherMode       string `json:"publisher_mode"`
	RequestedMode       string `json:"requested_mode"`
	LinkedInConfigured  bool   `json:"linkedin_configured"`
	MaskedLinkedInToken string `json:"masked_linkedin_token"`
	MaskedAuthorURN     string `json:"masked_author_urn"`
	FacebookConfigured  bool   `json:"facebook_configured"`
	MaskedFacebookToken string `json:"masked_facebook_token"`
	MaskedFacebookPage  string `json:"masked_facebook_page_id"`
	DBPath              string `json:"db_path"`
	Timezone            string `json:"timezone"`
	MigrationStatus     string `json:"migration_status"`
}

type paginationResponse struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	Total   int  `json:"total"`
	HasNext bool `json:"has_next"`
	HasPrev bool `json:"has_prev"`
}

func BasicAuthMiddleware(username, password string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isValidBasicAuth(r, username, password) {
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

			ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "basic")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func APIAuthMiddleware(username, password string, store *db.Store, staticAPIKeys map[string]string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isValidBasicAuth(r, username, password) {
				ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "basic")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			token := extractAPIKeyToken(r)
			if token != "" {
				if name, ok := staticAPIKeys[token]; ok {
					ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "api-key-env")
					ctx = context.WithValue(ctx, contextKeyAPIKeyName, name)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				if store != nil {
					apiKey, err := store.AuthenticateAPIKey(r.Context(), token)
					if err == nil {
						ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "api-key")
						ctx = context.WithValue(ctx, contextKeyAPIKeyID, apiKey.ID)
						ctx = context.WithValue(ctx, contextKeyAPIKeyName, apiKey.Name)
						next.ServeHTTP(w, r.WithContext(ctx))
						return
					}
					logger.LogAttrs(
						r.Context(),
						slog.LevelWarn,
						"api key authentication failed",
						slog.String("component", "http"),
						slog.String("path", r.URL.Path),
						slog.String("error", err.Error()),
					)
				}
			}

			w.Header().Set("WWW-Authenticate", `Basic realm="linkedin-cron"`)
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		})
	}
}

func isValidBasicAuth(r *http.Request, username, password string) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(username)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
	return userOK && passOK
}

func extractAPIKeyToken(r *http.Request) string {
	fromHeader := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if fromHeader != "" {
		return fromHeader
	}
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authorization), "bearer ") {
		return strings.TrimSpace(authorization[len("bearer "):])
	}
	return ""
}

func authMethodFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeyAuthMethod).(string)
	if value == "" {
		return "unknown"
	}
	return value
}

func apiKeyNameFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeyAPIKeyName).(string)
	return value
}

func apiKeyIDFromContext(ctx context.Context) int64 {
	value, _ := ctx.Value(contextKeyAPIKeyID).(int64)
	return value
}

func AuthMethodForLog(ctx context.Context) string {
	return authMethodFromContext(ctx)
}

func APIKeyIDForLog(ctx context.Context) int64 {
	return apiKeyIDFromContext(ctx)
}

func APIKeyNameForLog(ctx context.Context) string {
	return apiKeyNameFromContext(ctx)
}

func sanitizeAPIKeys(items []model.APIKey) []model.APIKey {
	keys := make([]model.APIKey, 0, len(items))
	for _, item := range items {
		copyItem := item
		if strings.TrimSpace(copyItem.KeyPrefix) != "" {
			copyItem.KeyPrefix = copyItem.KeyPrefix + "..."
		}
		keys = append(keys, copyItem)
	}
	return keys
}

func (a *App) settingsStatus() SettingsStatus {
	return SettingsStatus{
		BasicAuthConfigured: a.Config.BasicAuthConfigured(),
		MaskedAuthUser:      config.MaskSecret(a.Config.BasicAuthUser),
		MaskedAuthPass:      config.MaskSecret(a.Config.BasicAuthPass),
		StaticAPIKeysCount:  len(a.Config.StaticAPIKeys),
		PublisherMode:       a.ActivePublisher,
		RequestedMode:       a.RequestedPublisher,
		LinkedInConfigured:  a.LinkedInConfigured,
		MaskedLinkedInToken: config.MaskSecret(a.Config.LinkedInToken),
		MaskedAuthorURN:     config.MaskSecret(a.Config.LinkedInAuthorURN),
		FacebookConfigured:  a.FacebookConfigured,
		MaskedFacebookToken: config.MaskSecret(a.Config.FacebookPageToken),
		MaskedFacebookPage:  config.MaskSecret(a.Config.FacebookPageID),
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

func parseLimit(value string, fallback int) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > 500 {
		return 500
	}
	return parsed
}

func parseOffset(value string, fallback int) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func buildPagination(limit, offset, total int) paginationResponse {
	if limit <= 0 {
		limit = 1
	}
	if offset < 0 {
		offset = 0
	}
	if total < 0 {
		total = 0
	}

	return paginationResponse{
		Limit:   limit,
		Offset:  offset,
		Total:   total,
		HasNext: offset+limit < total,
		HasPrev: offset > 0,
	}
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

func parseAttemptedRangeRFC3339(fromValue, toValue string) (*time.Time, *time.Time, error) {
	attemptedFrom, err := parseRFC3339(fromValue)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid attempted_from; expected RFC3339")
	}
	attemptedTo, err := parseRFC3339(toValue)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid attempted_to; expected RFC3339")
	}
	if attemptedFrom != nil && attemptedTo != nil && attemptedFrom.After(*attemptedTo) {
		return nil, nil, fmt.Errorf("attempted_from must be before or equal to attempted_to")
	}
	return attemptedFrom, attemptedTo, nil
}

func parseAttemptedRangeLocal(fromValue, toValue string, location *time.Location) (*time.Time, *time.Time, error) {
	attemptedFrom, err := parseDateTimeLocal(fromValue, location)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid attempted_from; expected YYYY-MM-DDTHH:MM")
	}
	attemptedTo, err := parseDateTimeLocal(toValue, location)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid attempted_to; expected YYYY-MM-DDTHH:MM")
	}
	if attemptedFrom != nil && attemptedTo != nil && attemptedFrom.After(*attemptedTo) {
		return nil, nil, fmt.Errorf("attempted_from must be before or equal to attempted_to")
	}
	return attemptedFrom, attemptedTo, nil
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
