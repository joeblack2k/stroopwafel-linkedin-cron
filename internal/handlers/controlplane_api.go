package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
)

type auditEventResponse struct {
	ID        int64   `json:"id"`
	Action    string  `json:"action"`
	Resource  string  `json:"resource"`
	AuthActor string  `json:"auth_actor"`
	Source    string  `json:"source"`
	Metadata  *string `json:"metadata,omitempty"`
	CreatedAt string  `json:"created_at"`
}

type auditEventsListResponse struct {
	Items      []auditEventResponse `json:"items"`
	Pagination paginationResponse   `json:"pagination"`
}

type channelHealthSummaryResponse struct {
	ChannelID     int64   `json:"channel_id"`
	ChannelType   string  `json:"channel_type"`
	DisplayName   string  `json:"display_name"`
	Configured    bool    `json:"configured"`
	LastSuccessAt *string `json:"last_success_at,omitempty"`
	LastAttemptAt *string `json:"last_attempt_at,omitempty"`
	SentCount     int     `json:"sent_count"`
	FailedCount   int     `json:"failed_count"`
	RetryCount    int     `json:"retry_count"`
}

type channelHealthListResponse struct {
	Items []channelHealthSummaryResponse `json:"items"`
}

type bulkChannelStatusPayload struct {
	ChannelIDs []int64 `json:"channel_ids"`
}

type bulkChannelStatusResponse struct {
	Requested int               `json:"requested"`
	Succeeded int               `json:"succeeded"`
	Failed    int               `json:"failed"`
	Errors    []string          `json:"errors,omitempty"`
	Updated   []channelResponse `json:"updated"`
}

func (a *App) APIListAuditEvents(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	filter := db.AuditEventFilter{
		Action:    strings.TrimSpace(r.URL.Query().Get("action")),
		Resource:  strings.TrimSpace(r.URL.Query().Get("resource")),
		AuthActor: strings.TrimSpace(r.URL.Query().Get("auth_actor")),
		Source:    strings.TrimSpace(r.URL.Query().Get("source")),
	}

	total, err := a.Store.CountAuditEvents(r.Context(), filter)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to count audit events")
		return
	}

	events, err := a.Store.ListAuditEvents(r.Context(), filter, limit, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list audit events")
		return
	}

	items := make([]auditEventResponse, 0, len(events))
	for _, event := range events {
		items = append(items, auditEventResponse{
			ID:        event.ID,
			Action:    event.Action,
			Resource:  event.Resource,
			AuthActor: event.AuthActor,
			Source:    event.Source,
			Metadata:  event.Metadata,
			CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	writeJSON(w, http.StatusOK, auditEventsListResponse{
		Items:      items,
		Pagination: buildPagination(limit, offset, total),
	})
}

func (a *App) APIListPublishAttempts(w http.ResponseWriter, r *http.Request) {
	filter := db.PublishAttemptFilter{Status: normalizeAttemptStatus(r.URL.Query().Get("status"))}

	if postIDRaw := strings.TrimSpace(r.URL.Query().Get("post_id")); postIDRaw != "" {
		postID, err := parseID(postIDRaw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid post_id")
			return
		}
		filter.PostID = &postID
	}

	if channelIDRaw := strings.TrimSpace(r.URL.Query().Get("channel_id")); channelIDRaw != "" {
		channelID, err := parseID(channelIDRaw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid channel_id")
			return
		}
		filter.ChannelID = &channelID
	}

	atFrom, atTo, err := parseAttemptedRangeRFC3339(r.URL.Query().Get("attempted_from"), r.URL.Query().Get("attempted_to"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	filter.AttemptedFrom = atFrom
	filter.AttemptedTo = atTo

	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	total, err := a.Store.CountPublishAttempts(r.Context(), filter)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to count publish attempts")
		return
	}

	attempts, err := a.Store.ListPublishAttempts(r.Context(), filter, limit, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list publish attempts")
		return
	}

	channels, err := a.Store.ListChannels(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load channels")
		return
	}
	channelMap := make(map[int64]model.Channel, len(channels))
	for _, channel := range channels {
		channelMap[channel.ID] = channel
	}

	items := make([]attemptResponse, 0, len(attempts))
	for _, attempt := range attempts {
		channel := channelMap[attempt.ChannelID]
		item := attemptResponse{
			ID:            attempt.ID,
			PostID:        attempt.PostID,
			ChannelID:     attempt.ChannelID,
			ChannelName:   channel.DisplayName,
			ChannelType:   string(channel.Type),
			AttemptNo:     attempt.AttemptNo,
			AttemptedAt:   attempt.AttemptedAt.UTC().Format(time.RFC3339),
			Status:        attempt.Status,
			Error:         attempt.Error,
			ErrorCategory: attempt.ErrorCategory,
			ExternalID:    attempt.ExternalID,
			Permalink:     attempt.Permalink,
			ScreenshotURL: attempt.ScreenshotURL,
		}
		if strings.TrimSpace(item.ChannelName) == "" {
			item.ChannelName = fmt.Sprintf("channel-%d", attempt.ChannelID)
		}
		if strings.TrimSpace(item.ChannelType) == "" {
			item.ChannelType = string(model.ChannelTypeDryRun)
		}
		if attempt.RetryAt != nil {
			value := attempt.RetryAt.UTC().Format(time.RFC3339)
			item.RetryAt = &value
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, attemptsListResponse{
		Items:      items,
		Pagination: buildPagination(limit, offset, total),
	})
}

func (a *App) APIListChannelHealth(w http.ResponseWriter, r *http.Request) {
	summaries, err := a.Store.ListChannelHealthSummaries(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list channel health")
		return
	}

	items := make([]channelHealthSummaryResponse, 0, len(summaries))
	for _, summary := range summaries {
		item := channelHealthSummaryResponse{
			ChannelID:   summary.ChannelID,
			ChannelType: string(summary.ChannelType),
			DisplayName: summary.DisplayName,
			Configured:  summary.Configured,
			SentCount:   summary.SentCount,
			FailedCount: summary.FailedCount,
			RetryCount:  summary.RetryCount,
		}
		if summary.LastSuccessAt != nil {
			value := summary.LastSuccessAt.UTC().Format(time.RFC3339)
			item.LastSuccessAt = &value
		}
		if summary.LastAttemptAt != nil {
			value := summary.LastAttemptAt.UTC().Format(time.RFC3339)
			item.LastAttemptAt = &value
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, channelHealthListResponse{Items: items})
}

func (a *App) APIBulkDisableChannels(w http.ResponseWriter, r *http.Request) {
	a.apiBulkSetChannelStatus(w, r, model.ChannelStatusDisabled)
}

func (a *App) APIBulkEnableChannels(w http.ResponseWriter, r *http.Request) {
	a.apiBulkSetChannelStatus(w, r, model.ChannelStatusActive)
}

func (a *App) apiBulkSetChannelStatus(w http.ResponseWriter, r *http.Request, status string) {
	var payload bulkChannelStatusPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	channelIDs := dedupeChannelIDs(payload.ChannelIDs)
	if len(channelIDs) == 0 {
		writeAPIError(w, http.StatusBadRequest, "channel_ids must include at least one id")
		return
	}

	channels, err := a.Store.ListChannelsByIDs(r.Context(), channelIDs)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load selected channels")
		return
	}

	found := make(map[int64]struct{}, len(channels))
	for _, channel := range channels {
		found[channel.ID] = struct{}{}
	}
	for _, channelID := range channelIDs {
		if _, ok := found[channelID]; !ok {
			writeAPIError(w, http.StatusBadRequest, "one or more channel_ids were not found")
			return
		}
	}

	result := bulkChannelStatusResponse{
		Requested: len(channelIDs),
		Errors:    make([]string, 0),
		Updated:   make([]channelResponse, 0, len(channelIDs)),
	}

	for _, channelID := range channelIDs {
		updated, updateErr := a.Store.SetChannelStatus(r.Context(), channelID, status, nil)
		if updateErr != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("channel %d: %v", channelID, updateErr))
			continue
		}

		if status == model.ChannelStatusActive {
			tested, testErr := a.runChannelTest(r.Context(), channelID)
			if testErr != nil {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("channel %d: %v", channelID, testErr))
				continue
			}
			updated = tested
		}

		result.Succeeded++
		result.Updated = append(result.Updated, mapChannelResponse(updated))
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *App) WithAPIAuditTrail(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}

		capture := &idempotencyCaptureWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(capture, r)

		metadata := map[string]any{
			"method":                  r.Method,
			"path":                    r.URL.Path,
			"status_code":             capture.status,
			"auth_method":             authMethodFromContext(r.Context()),
			"idempotency_key_present": strings.TrimSpace(r.Header.Get(idempotencyKeyHeader)) != "",
			"idempotent_replay":       strings.EqualFold(strings.TrimSpace(capture.Header().Get(idempotentReplayHeader)), "true"),
		}

		a.recordAuditEvent(r.Context(), apiAuditAction(r), apiAuditResource(r), metadata)
	})
}

func (a *App) recordAuditEvent(ctx context.Context, action, resource string, metadata map[string]any) {
	var metadataValue *string
	if len(metadata) > 0 {
		encoded, err := json.Marshal(metadata)
		if err == nil {
			value := string(encoded)
			metadataValue = &value
		}
	}

	_, err := a.Store.CreateAuditEvent(ctx, db.AuditEventInput{
		Action:    action,
		Resource:  resource,
		AuthActor: apiAuditActor(ctx),
		Source:    "api",
		Metadata:  metadataValue,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		a.Logger.Warn("failed to write audit event", "component", "api", "action", action, "resource", resource, "error", err.Error())
	}
}

func apiAuditAction(r *http.Request) string {
	path := strings.TrimSpace(r.URL.Path)
	switch path {
	case "/api/v1/channels/bulk/enable":
		return "channels.bulk_enable"
	case "/api/v1/channels/bulk/disable":
		return "channels.bulk_disable"
	default:
		method := strings.ToLower(strings.TrimSpace(r.Method))
		if method == "" {
			method = "unknown"
		}
		return "api." + method
	}
}

func apiAuditResource(r *http.Request) string {
	path := strings.TrimSpace(r.URL.Path)
	if path == "/api/v1/channels/bulk/enable" || path == "/api/v1/channels/bulk/disable" {
		return "channels"
	}
	if path == "" {
		return "unknown"
	}
	return path
}

func apiAuditActor(ctx context.Context) string {
	if apiKeyID := apiKeyIDFromContext(ctx); apiKeyID > 0 {
		return "api-key:" + strconv.FormatInt(apiKeyID, 10)
	}
	if apiKeyName := strings.TrimSpace(apiKeyNameFromContext(ctx)); apiKeyName != "" {
		return "api-key-name:" + apiKeyName
	}
	if authUser := strings.TrimSpace(authUserFromContext(ctx)); authUser != "" {
		return authUser
	}
	return "unknown"
}
