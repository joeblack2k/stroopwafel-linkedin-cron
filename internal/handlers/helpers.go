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
	"unicode"

	"stroopwafel/internal/config"
	"stroopwafel/internal/db"
	"stroopwafel/internal/model"
	"stroopwafel/internal/scheduler"
	"stroopwafel/internal/views"
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
	contextKeyAuthUser   contextKey = "auth_user"
	contextKeyAPIKeyID   contextKey = "api_key_id"
	contextKeyAPIKeyName contextKey = "api_key_name"
)

const (
	requestAuthMethodHeader = "X-SW-Auth-Method"
	requestAPIKeyIDHeader   = "X-SW-API-Key-ID"
	requestAPIKeyNameHeader = "X-SW-API-Key-Name"
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
	DataDir             string `json:"data_dir"`
	ConfigPath          string `json:"config_path"`
	DBPath              string `json:"db_path"`
	Timezone            string `json:"timezone"`
	MigrationStatus     string `json:"migration_status"`
	WebhookConfigured   bool   `json:"webhook_configured"`
	WebhookTargets      int    `json:"webhook_targets"`
	MaskedWebhookKey    string `json:"masked_webhook_secret"`
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
			authUser, ok := basicAuthUserIfValid(r, username, password)
			if !ok {
				logger.LogAttrs(
					r.Context(),
					slog.LevelWarn,
					"basic auth failed",
					slog.String("component", "http"),
					slog.String("path", r.URL.Path),
				)
				w.Header().Set("WWW-Authenticate", `Basic realm="stroopwafel"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			r.Header.Set(requestAuthMethodHeader, "basic")
			ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "basic")
			ctx = context.WithValue(ctx, contextKeyAuthUser, authUser)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func APIAuthMiddleware(username, password string, store *db.Store, staticAPIKeys map[string]string, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authUser, ok := basicAuthUserIfValid(r, username, password); ok {
				r.Header.Set(requestAuthMethodHeader, "basic")
				ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "basic")
				ctx = context.WithValue(ctx, contextKeyAuthUser, authUser)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			token := extractAPIKeyToken(r)
			if token != "" {
				if name, ok := staticAPIKeys[token]; ok {
					r.Header.Set(requestAuthMethodHeader, "api-key-env")
					r.Header.Set(requestAPIKeyNameHeader, name)
					ctx := context.WithValue(r.Context(), contextKeyAuthMethod, "api-key-env")
					ctx = context.WithValue(ctx, contextKeyAPIKeyName, name)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}

				if store != nil {
					apiKey, err := store.AuthenticateAPIKey(r.Context(), token)
					if err == nil {
						r.Header.Set(requestAuthMethodHeader, "api-key")
						r.Header.Set(requestAPIKeyIDHeader, strconv.FormatInt(apiKey.ID, 10))
						r.Header.Set(requestAPIKeyNameHeader, apiKey.Name)
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

			w.Header().Set("WWW-Authenticate", `Basic realm="stroopwafel"`)
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

func authUserFromContext(ctx context.Context) string {
	value, _ := ctx.Value(contextKeyAuthUser).(string)
	return strings.TrimSpace(value)
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
		DataDir:             a.Config.DataDir,
		ConfigPath:          a.Config.ConfigPath,
		DBPath:              a.Config.DBPath,
		Timezone:            a.Config.Timezone,
		MigrationStatus:     a.MigrationStatus,
		WebhookConfigured:   len(a.Config.WebhookURLs) > 0,
		WebhookTargets:      len(a.Config.WebhookURLs),
		MaskedWebhookKey:    config.MaskSecret(a.Config.WebhookSecret),
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
	errorCode := apiErrorCode(status, message)
	writeJSON(w, status, map[string]string{"error": message, "error_code": errorCode})
}

func apiErrorCode(status int, message string) string {
	trimmed := strings.TrimSpace(strings.ToLower(message))
	if trimmed == "" {
		return statusCodePrefix(status)
	}

	known := map[string]string{
		"unauthorized":          "auth_unauthorized",
		"invalid json payload":  "request_invalid_json",
		"invalid post id":       "request_invalid_post_id",
		"post not found":        "post_not_found",
		"invalid channel id":    "request_invalid_channel_id",
		"channel not found":     "channel_not_found",
		"invalid status filter": "request_invalid_status_filter",
		"invalid type filter":   "request_invalid_type_filter",
		"invalid channel_id":    "request_invalid_channel_id",
	}
	if code, ok := known[trimmed]; ok {
		return code
	}

	slug := slugify(trimmed)
	if slug == "" {
		return statusCodePrefix(status)
	}
	return statusCodePrefix(status) + "_" + slug
}

func statusCodePrefix(status int) string {
	switch {
	case status >= 500:
		return "internal_error"
	case status == http.StatusUnauthorized:
		return "auth_error"
	case status == http.StatusForbidden:
		return "forbidden"
	case status == http.StatusNotFound:
		return "not_found"
	case status == http.StatusConflict:
		return "conflict"
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "bad_request"
	}
}

func slugify(value string) string {
	runes := make([]rune, 0, len(value))
	lastUnderscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			runes = append(runes, r)
			lastUnderscore = false
			continue
		}
		if lastUnderscore || len(runes) == 0 {
			continue
		}
		runes = append(runes, '_')
		lastUnderscore = true
	}
	for len(runes) > 0 && runes[len(runes)-1] == '_' {
		runes = runes[:len(runes)-1]
	}
	return strings.TrimSpace(string(runes))
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
