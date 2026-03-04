package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"stroopwafel/internal/db"
	"stroopwafel/internal/webhooks"
)

var webhookReplayBackoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

type webhookReplayResponse struct {
	ID             int64   `json:"id"`
	DeliveryID     *int64  `json:"delivery_id,omitempty"`
	EventID        string  `json:"event_id"`
	EventName      string  `json:"event_name"`
	TargetURL      string  `json:"target_url"`
	Source         string  `json:"source"`
	Status         string  `json:"status"`
	AttemptCount   int     `json:"attempt_count"`
	LastError      *string `json:"last_error,omitempty"`
	LastHTTPStatus *int    `json:"last_http_status,omitempty"`
	LastAttemptAt  *string `json:"last_attempt_at,omitempty"`
	NextAttemptAt  *string `json:"next_attempt_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type webhookReplayListResponse struct {
	Items      []webhookReplayResponse `json:"items"`
	Pagination paginationResponse      `json:"pagination"`
}

type webhookReplayBulkPayload struct {
	Limit     int    `json:"limit"`
	TargetURL string `json:"target_url,omitempty"`
	EventName string `json:"event_name,omitempty"`
	EventID   string `json:"event_id,omitempty"`
}

type WebhookReplayPageData struct {
	Title          string
	Rows           []WebhookReplayRow
	SelectedStatus string
	Limit          int
	Offset         int
	Total          int
	HasPrev        bool
	HasNext        bool
	PrevURL        string
	NextURL        string
	Message        string
	Error          string
}

type WebhookReplayRow struct {
	ID             int64
	DeliveryLabel  string
	EventID        string
	EventName      string
	TargetURL      string
	Source         string
	Status         string
	AttemptCount   int
	LastError      string
	LastHTTPStatus string
	LastAttemptAt  string
	NextAttemptAt  string
	CreatedAt      string
	UpdatedAt      string
}

func (a *App) APIListWebhookReplays(w http.ResponseWriter, r *http.Request) {
	filter := db.WebhookReplayFilter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
	}
	if filter.Status != "" && !isWebhookReplayStatus(filter.Status) {
		writeAPIError(w, http.StatusBadRequest, "invalid status filter")
		return
	}
	filter.TargetURL = strings.TrimSpace(r.URL.Query().Get("target_url"))
	filter.EventName = strings.TrimSpace(r.URL.Query().Get("event_name"))
	filter.EventID = strings.TrimSpace(r.URL.Query().Get("event_id"))

	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	total, err := a.Store.CountWebhookReplays(r.Context(), filter)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to count webhook replays")
		return
	}

	replays, err := a.Store.ListWebhookReplays(r.Context(), filter, limit, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list webhook replays")
		return
	}

	items := make([]webhookReplayResponse, 0, len(replays))
	for _, replay := range replays {
		items = append(items, mapWebhookReplayResponse(replay))
	}

	writeJSON(w, http.StatusOK, webhookReplayListResponse{
		Items:      items,
		Pagination: buildPagination(limit, offset, total),
	})
}

func (a *App) APIReplayWebhook(w http.ResponseWriter, r *http.Request) {
	replayID, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid replay id")
		return
	}

	updated, replayErr := a.replayWebhookByID(r.Context(), replayID)
	if replayErr != nil {
		if errors.Is(replayErr, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "webhook replay not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, replayErr.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "webhook replay attempted",
		"replay":  mapWebhookReplayResponse(updated),
	})
}

func (a *App) APICancelWebhookReplay(w http.ResponseWriter, r *http.Request) {
	replayID, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid replay id")
		return
	}

	updated, err := a.Store.UpdateWebhookReplayStatus(r.Context(), replayID, db.WebhookReplayStatusCancelled, nil, nil)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "webhook replay not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to cancel webhook replay")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "webhook replay cancelled",
		"replay":  mapWebhookReplayResponse(updated),
	})
}

func (a *App) APIReplayFailedWebhooks(w http.ResponseWriter, r *http.Request) {
	payload := webhookReplayBulkPayload{Limit: 20}
	if r.ContentLength > 0 {
		if err := readJSONBody(r, &payload); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
			return
		}
	}

	if payload.Limit <= 0 {
		payload.Limit = 20
	}
	if payload.Limit > 200 {
		payload.Limit = 200
	}

	filter := db.WebhookReplayFilter{
		Status:    db.WebhookReplayStatusFailed,
		TargetURL: strings.TrimSpace(payload.TargetURL),
		EventName: strings.TrimSpace(payload.EventName),
		EventID:   strings.TrimSpace(payload.EventID),
	}

	replays, err := a.Store.ListWebhookReplays(r.Context(), filter, payload.Limit, 0)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list failed webhook replays")
		return
	}

	result := bulkOperationResult{Requested: len(replays), Errors: make([]string, 0)}
	for _, replay := range replays {
		updated, replayErr := a.replayWebhookByID(r.Context(), replay.ID)
		if replayErr != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("replay %d: %v", replay.ID, replayErr))
			continue
		}
		if updated.Status == db.WebhookReplayStatusDelivered {
			result.Succeeded++
			continue
		}
		result.Failed++
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *App) WebhookReplays(w http.ResponseWriter, r *http.Request) {
	selectedStatus := strings.TrimSpace(r.URL.Query().Get("status"))
	if selectedStatus != "" && !isWebhookReplayStatus(selectedStatus) {
		selectedStatus = ""
	}

	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	filter := db.WebhookReplayFilter{Status: selectedStatus}
	total, err := a.Store.CountWebhookReplays(r.Context(), filter)
	if err != nil {
		http.Error(w, "failed to count webhook replays", http.StatusInternalServerError)
		return
	}
	replays, err := a.Store.ListWebhookReplays(r.Context(), filter, limit, offset)
	if err != nil {
		http.Error(w, "failed to list webhook replays", http.StatusInternalServerError)
		return
	}

	rows := make([]WebhookReplayRow, 0, len(replays))
	for _, replay := range replays {
		rows = append(rows, mapWebhookReplayRow(replay, a.Config.Location))
	}

	hasPrev := offset > 0
	hasNext := offset+len(replays) < total
	prevOffset := offset - limit
	if prevOffset < 0 {
		prevOffset = 0
	}
	nextOffset := offset + limit

	data := WebhookReplayPageData{
		Title:          "Webhook Replay Dashboard",
		Rows:           rows,
		SelectedStatus: selectedStatus,
		Limit:          limit,
		Offset:         offset,
		Total:          total,
		HasPrev:        hasPrev,
		HasNext:        hasNext,
		Message:        strings.TrimSpace(r.URL.Query().Get("msg")),
		Error:          strings.TrimSpace(r.URL.Query().Get("err")),
	}
	if hasPrev {
		data.PrevURL = buildWebhookReplayURL(selectedStatus, limit, prevOffset, "", "")
	}
	if hasNext {
		data.NextURL = buildWebhookReplayURL(selectedStatus, limit, nextOffset, "", "")
	}

	if err := a.Renderer.Render(w, "webhook_replays.html", data); err != nil {
		http.Error(w, "failed to render webhook replay dashboard", http.StatusInternalServerError)
	}
}

func (a *App) ReplayWebhook(w http.ResponseWriter, r *http.Request) {
	replayID, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "", "invalid replay id"), http.StatusSeeOther)
		return
	}

	updated, replayErr := a.replayWebhookByID(r.Context(), replayID)
	if replayErr != nil {
		http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "", replayErr.Error()), http.StatusSeeOther)
		return
	}

	message := "Webhook replay attempted"
	if updated.Status == db.WebhookReplayStatusDelivered {
		message = "Webhook replay delivered"
	}
	http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, message, ""), http.StatusSeeOther)
}

func (a *App) CancelWebhookReplay(w http.ResponseWriter, r *http.Request) {
	replayID, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "", "invalid replay id"), http.StatusSeeOther)
		return
	}

	if _, err := a.Store.UpdateWebhookReplayStatus(r.Context(), replayID, db.WebhookReplayStatusCancelled, nil, nil); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "", "webhook replay not found"), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "", "failed to cancel webhook replay"), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "Webhook replay cancelled", ""), http.StatusSeeOther)
}

func (a *App) ReplayFailedWebhooks(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "", "invalid form body"), http.StatusSeeOther)
		return
	}

	limit := parseLimit(r.FormValue("limit"), 20)
	if limit > 200 {
		limit = 200
	}

	replays, err := a.Store.ListWebhookReplays(r.Context(), db.WebhookReplayFilter{Status: db.WebhookReplayStatusFailed}, limit, 0)
	if err != nil {
		http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "", "failed to list failed replays"), http.StatusSeeOther)
		return
	}
	if len(replays) == 0 {
		http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, "No failed replays found", ""), http.StatusSeeOther)
		return
	}

	delivered := 0
	failed := 0
	for _, replay := range replays {
		updated, replayErr := a.replayWebhookByID(r.Context(), replay.ID)
		if replayErr != nil {
			failed++
			continue
		}
		if updated.Status == db.WebhookReplayStatusDelivered {
			delivered++
		} else {
			failed++
		}
	}

	message := fmt.Sprintf("Replay run finished: delivered %d, failed %d", delivered, failed)
	http.Redirect(w, r, buildWebhookReplayURL("", 50, 0, message, ""), http.StatusSeeOther)
}

func (a *App) replayWebhookByID(ctx context.Context, replayID int64) (db.WebhookReplay, error) {
	replay, err := a.Store.GetWebhookReplay(ctx, replayID)
	if err != nil {
		return db.WebhookReplay{}, err
	}
	if replay.Status == db.WebhookReplayStatusCancelled {
		return db.WebhookReplay{}, fmt.Errorf("replay %d is cancelled", replayID)
	}

	payload := map[string]any{}
	payloadRaw := strings.TrimSpace(replay.Payload)
	if payloadRaw != "" {
		if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
			nextAttempt := computeWebhookReplayNextAttempt(replay.AttemptCount)
			parseErr := fmt.Sprintf("invalid replay payload JSON: %v", err)
			updated, updateErr := a.Store.UpdateWebhookReplayAfterAttempt(ctx, replayID, db.WebhookReplayStatusFailed, nil, &parseErr, nextAttempt)
			if updateErr != nil {
				return db.WebhookReplay{}, fmt.Errorf("update replay after payload parse failure: %w", updateErr)
			}
			return updated, errors.New(parseErr)
		}
	}

	dispatcher := webhooks.NewDispatcher([]string{replay.TargetURL}, a.Config.WebhookSecret, replay.Source, a.Logger)
	if !dispatcher.Enabled() {
		nextAttempt := computeWebhookReplayNextAttempt(replay.AttemptCount)
		errText := fmt.Sprintf("invalid replay target url: %q", replay.TargetURL)
		updated, updateErr := a.Store.UpdateWebhookReplayAfterAttempt(ctx, replayID, db.WebhookReplayStatusFailed, nil, &errText, nextAttempt)
		if updateErr != nil {
			return db.WebhookReplay{}, fmt.Errorf("update replay for invalid target: %w", updateErr)
		}
		return updated, errors.New(errText)
	}

	outcomes := dispatcher.Emit(ctx, replay.EventName, payload)
	if len(outcomes) == 0 {
		nextAttempt := computeWebhookReplayNextAttempt(replay.AttemptCount)
		errText := "dispatcher emitted no replay outcomes"
		updated, updateErr := a.Store.UpdateWebhookReplayAfterAttempt(ctx, replayID, db.WebhookReplayStatusFailed, nil, &errText, nextAttempt)
		if updateErr != nil {
			return db.WebhookReplay{}, fmt.Errorf("update replay for empty outcomes: %w", updateErr)
		}
		return updated, errors.New(errText)
	}

	outcome := outcomes[0]
	deliveryStatus := "failed"
	replayStatus := db.WebhookReplayStatusFailed
	nextAttempt := computeWebhookReplayNextAttempt(replay.AttemptCount)
	if outcome.Delivered {
		deliveryStatus = "delivered"
		replayStatus = db.WebhookReplayStatusDelivered
		nextAttempt = nil
	}

	delivery, deliveryErr := a.Store.InsertWebhookDelivery(ctx, db.WebhookDeliveryInput{
		EventID:     outcome.EventID,
		EventName:   outcome.EventName,
		TargetURL:   outcome.TargetURL,
		Status:      deliveryStatus,
		HTTPStatus:  outcome.HTTPStatus,
		Error:       outcome.Error,
		Source:      outcome.Source,
		DurationMS:  outcome.DurationMS,
		OccurredAt:  outcome.OccurredAt,
		DeliveredAt: outcome.DeliveredAt,
	})
	if deliveryErr == nil {
		replay.DeliveryID = &delivery.ID
	}

	updated, updateErr := a.Store.UpdateWebhookReplayAfterAttempt(ctx, replayID, replayStatus, outcome.HTTPStatus, outcome.Error, nextAttempt)
	if updateErr != nil {
		return db.WebhookReplay{}, fmt.Errorf("update webhook replay after dispatch: %w", updateErr)
	}
	if deliveryErr != nil {
		return updated, fmt.Errorf("persist replay delivery: %w", deliveryErr)
	}
	if outcome.Delivered {
		return updated, nil
	}
	if outcome.Error != nil {
		return updated, errors.New(*outcome.Error)
	}
	return updated, fmt.Errorf("webhook replay failed")
}

func mapWebhookReplayResponse(replay db.WebhookReplay) webhookReplayResponse {
	response := webhookReplayResponse{
		ID:             replay.ID,
		DeliveryID:     replay.DeliveryID,
		EventID:        replay.EventID,
		EventName:      replay.EventName,
		TargetURL:      replay.TargetURL,
		Source:         replay.Source,
		Status:         replay.Status,
		AttemptCount:   replay.AttemptCount,
		LastError:      replay.LastError,
		LastHTTPStatus: replay.HTTPStatus,
		CreatedAt:      replay.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      replay.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if replay.LastAttempt != nil {
		value := replay.LastAttempt.UTC().Format(time.RFC3339)
		response.LastAttemptAt = &value
	}
	if replay.NextAttempt != nil {
		value := replay.NextAttempt.UTC().Format(time.RFC3339)
		response.NextAttemptAt = &value
	}
	return response
}

func mapWebhookReplayRow(replay db.WebhookReplay, location *time.Location) WebhookReplayRow {
	if location == nil {
		location = time.UTC
	}
	deliveryLabel := "-"
	if replay.DeliveryID != nil {
		deliveryLabel = fmt.Sprintf("#%d", *replay.DeliveryID)
	}
	lastError := "-"
	if replay.LastError != nil && strings.TrimSpace(*replay.LastError) != "" {
		lastError = strings.TrimSpace(*replay.LastError)
	}
	lastHTTPStatus := "-"
	if replay.HTTPStatus != nil {
		lastHTTPStatus = strconv.Itoa(*replay.HTTPStatus)
	}
	lastAttempt := "-"
	if replay.LastAttempt != nil {
		lastAttempt = replay.LastAttempt.In(location).Format("2006-01-02 15:04:05")
	}
	nextAttempt := "-"
	if replay.NextAttempt != nil {
		nextAttempt = replay.NextAttempt.In(location).Format("2006-01-02 15:04:05")
	}

	return WebhookReplayRow{
		ID:             replay.ID,
		DeliveryLabel:  deliveryLabel,
		EventID:        replay.EventID,
		EventName:      replay.EventName,
		TargetURL:      replay.TargetURL,
		Source:         replay.Source,
		Status:         replay.Status,
		AttemptCount:   replay.AttemptCount,
		LastError:      lastError,
		LastHTTPStatus: lastHTTPStatus,
		LastAttemptAt:  lastAttempt,
		NextAttemptAt:  nextAttempt,
		CreatedAt:      replay.CreatedAt.In(location).Format("2006-01-02 15:04:05"),
		UpdatedAt:      replay.UpdatedAt.In(location).Format("2006-01-02 15:04:05"),
	}
}

func buildWebhookReplayURL(status string, limit, offset int, message, renderErr string) string {
	values := url.Values{}
	if strings.TrimSpace(status) != "" {
		values.Set("status", strings.TrimSpace(status))
	}
	if limit > 0 {
		values.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		values.Set("offset", strconv.Itoa(offset))
	}
	if strings.TrimSpace(message) != "" {
		values.Set("msg", strings.TrimSpace(message))
	}
	if strings.TrimSpace(renderErr) != "" {
		values.Set("err", strings.TrimSpace(renderErr))
	}
	encoded := values.Encode()
	if encoded == "" {
		return "/settings/webhooks/replays"
	}
	return "/settings/webhooks/replays?" + encoded
}

func computeWebhookReplayNextAttempt(attemptCount int) *time.Time {
	if attemptCount < 0 {
		attemptCount = 0
	}
	if attemptCount >= len(webhookReplayBackoff) {
		return nil
	}
	next := time.Now().UTC().Add(webhookReplayBackoff[attemptCount])
	return &next
}

func isWebhookReplayStatus(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case db.WebhookReplayStatusQueued,
		db.WebhookReplayStatusProcessing,
		db.WebhookReplayStatusDelivered,
		db.WebhookReplayStatusFailed,
		db.WebhookReplayStatusCancelled:
		return true
	default:
		return false
	}
}
