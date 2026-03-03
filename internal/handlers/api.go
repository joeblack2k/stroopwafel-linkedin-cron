package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
)

type postPayload struct {
	ScheduledAt *string `json:"scheduled_at"`
	Text        string  `json:"text"`
	Status      string  `json:"status"`
	MediaType   *string `json:"media_type"`
	MediaURL    *string `json:"media_url"`
	ChannelIDs  []int64 `json:"channel_ids,omitempty"`
}

type postReschedulePayload struct {
	ScheduledAt string `json:"scheduled_at"`
}

type botHandoffPayload struct {
	Name string `json:"name"`
}

type postResponse struct {
	ID          int64   `json:"id"`
	ScheduledAt *string `json:"scheduled_at,omitempty"`
	Text        string  `json:"text"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	SentAt      *string `json:"sent_at,omitempty"`
	FailCount   int     `json:"fail_count"`
	LastError   *string `json:"last_error,omitempty"`
	MediaType   *string `json:"media_type,omitempty"`
	MediaURL    *string `json:"media_url,omitempty"`
	NextRetryAt *string `json:"next_retry_at,omitempty"`
	ChannelIDs  []int64 `json:"channel_ids,omitempty"`
}

func (a *App) APIHealthz(w http.ResponseWriter, r *http.Request) {
	a.handleHealth(w, r, "api")
}

func (a *App) APISettingsStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.settingsStatus())
}

func (a *App) APIListPosts(w http.ResponseWriter, r *http.Request) {
	posts, err := a.Store.ListPosts(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list posts")
		return
	}

	response := make([]postResponse, 0, len(posts))
	for _, post := range posts {
		mapped, mapErr := a.mapPostResponse(r.Context(), post)
		if mapErr != nil {
			writeAPIError(w, http.StatusInternalServerError, "failed to load post channels")
			return
		}
		response = append(response, mapped)
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) APIGetPost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid post id")
		return
	}

	post, err := a.Store.GetPost(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to fetch post")
		return
	}

	mapped, mapErr := a.mapPostResponse(r.Context(), post)
	if mapErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load post channels")
		return
	}
	writeJSON(w, http.StatusOK, mapped)
}

func (a *App) APICreatePost(w http.ResponseWriter, r *http.Request) {
	var payload postPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	input, channelIDs, err := parsePostPayload(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	if validationErr := a.validatePostAgainstChannelRules(r.Context(), input.Text, channelIDs); validationErr != nil {
		writeAPIError(w, http.StatusBadRequest, validationErr.Error())
		return
	}
	if validationErr := a.validatePostAgainstChannelCapabilities(r.Context(), input.MediaType, input.MediaURL, channelIDs); validationErr != nil {
		writeAPIError(w, http.StatusBadRequest, validationErr.Error())
		return
	}

	created, err := a.Store.CreatePost(r.Context(), input)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to create post")
		return
	}

	if err := a.Store.ReplacePostChannels(r.Context(), created.ID, channelIDs); err != nil {
		_ = a.Store.DeletePost(r.Context(), created.ID)
		writeAPIError(w, http.StatusInternalServerError, "failed to save post channels")
		return
	}

	mapped, mapErr := a.mapPostResponse(r.Context(), created)
	if mapErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load post channels")
		return
	}

	warnings := make([]scheduleWarning, 0)
	if input.Status == model.StatusScheduled && input.ScheduledAt != nil {
		guardrails, warnErr := a.computeSchedulingWarnings(r.Context(), *input.ScheduledAt, channelIDs, created.ID)
		if warnErr == nil {
			warnings = guardrails
		}
	}
	writeJSON(w, http.StatusCreated, postMutationResponse{Post: mapped, Warnings: warnings})
}

func (a *App) APIUpdatePost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid post id")
		return
	}

	var payload postPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	input, channelIDs, err := parsePostPayload(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	if validationErr := a.validatePostAgainstChannelRules(r.Context(), input.Text, channelIDs); validationErr != nil {
		writeAPIError(w, http.StatusBadRequest, validationErr.Error())
		return
	}
	if validationErr := a.validatePostAgainstChannelCapabilities(r.Context(), input.MediaType, input.MediaURL, channelIDs); validationErr != nil {
		writeAPIError(w, http.StatusBadRequest, validationErr.Error())
		return
	}

	updated, err := a.Store.UpdatePost(r.Context(), id, input)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to update post")
		return
	}

	if err := a.Store.ReplacePostChannels(r.Context(), updated.ID, channelIDs); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to save post channels")
		return
	}

	mapped, mapErr := a.mapPostResponse(r.Context(), updated)
	if mapErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load post channels")
		return
	}

	warnings := make([]scheduleWarning, 0)
	if input.Status == model.StatusScheduled && input.ScheduledAt != nil {
		guardrails, warnErr := a.computeSchedulingWarnings(r.Context(), *input.ScheduledAt, channelIDs, updated.ID)
		if warnErr == nil {
			warnings = guardrails
		}
	}
	writeJSON(w, http.StatusOK, postMutationResponse{Post: mapped, Warnings: warnings})
}

func (a *App) APIDeletePost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid post id")
		return
	}

	err = a.Store.DeletePost(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to delete post")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) APISendNowPost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid post id")
		return
	}

	if err := a.Scheduler.SendNow(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	post, err := a.Store.GetPost(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "post sent but failed to reload record")
		return
	}
	mapped, mapErr := a.mapPostResponse(r.Context(), post)
	if mapErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load post channels")
		return
	}
	writeJSON(w, http.StatusOK, mapped)
}

func (a *App) APISendAndDeletePost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid post id")
		return
	}

	if err := a.Scheduler.SendNow(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := a.Store.DeletePost(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "post sent but delete failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"status": "sent_and_deleted",
	})
}

func (a *App) APIReschedulePost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid post id")
		return
	}

	var payload postReschedulePayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	scheduledAt, err := parseRFC3339(payload.ScheduledAt)
	if err != nil || scheduledAt == nil {
		writeAPIError(w, http.StatusBadRequest, "scheduled_at must be RFC3339")
		return
	}

	post, err := a.Store.GetPost(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load post")
		return
	}
	if post.Status == model.StatusSent {
		writeAPIError(w, http.StatusBadRequest, "sent posts cannot be rescheduled")
		return
	}

	status := post.Status
	if status == model.StatusDraft || status == model.StatusFailed {
		status = model.StatusScheduled
	}

	channelIDs, channelErr := a.Store.ListPostChannelIDs(r.Context(), post.ID)
	if channelErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load post channels")
		return
	}
	mediaType, mediaErr := a.Store.GetPostMediaType(r.Context(), post.ID)
	if mediaErr != nil {
		if errors.Is(mediaErr, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load post media_type")
		return
	}
	if validationErr := model.ValidateEditableInput(post.Text, status, scheduledAt, post.MediaURL, mediaType); validationErr != nil {
		writeAPIError(w, http.StatusBadRequest, validationErr.Error())
		return
	}
	if validationErr := a.validatePostAgainstChannelRules(r.Context(), post.Text, channelIDs); validationErr != nil {
		writeAPIError(w, http.StatusBadRequest, validationErr.Error())
		return
	}
	if validationErr := a.validatePostAgainstChannelCapabilities(r.Context(), mediaType, post.MediaURL, channelIDs); validationErr != nil {
		writeAPIError(w, http.StatusBadRequest, validationErr.Error())
		return
	}

	updated, err := a.Store.UpdatePost(r.Context(), id, db.PostInput{
		ScheduledAt:  scheduledAt,
		Text:         post.Text,
		Status:       status,
		MediaType:    mediaType,
		MediaTypeSet: true,
		MediaURL:     post.MediaURL,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to reschedule post")
		return
	}

	mapped, mapErr := a.mapPostResponse(r.Context(), updated)
	if mapErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load post channels")
		return
	}

	warnings, warnErr := a.computeSchedulingWarnings(r.Context(), *scheduledAt, channelIDs, updated.ID)
	if warnErr != nil {
		warnings = nil
	}
	writeJSON(w, http.StatusOK, postMutationResponse{Post: mapped, Warnings: warnings})
}

func (a *App) APICreateBotHandoff(w http.ResponseWriter, r *http.Request) {
	var payload botHandoffPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		name = "api-bot-" + time.Now().In(a.Config.Location).Format("20060102-150405")
	}

	created, rawToken, err := a.Store.CreateAPIKey(r.Context(), name)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to create api key")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"name":         created.Name,
		"api_key":      rawToken,
		"instructions": a.buildBotHandoff(rawToken),
	})
}

func parsePostPayload(payload postPayload) (db.PostInput, []int64, error) {
	status := model.PostStatus(strings.TrimSpace(payload.Status))
	text := strings.TrimSpace(payload.Text)

	var scheduledAt *time.Time
	if payload.ScheduledAt != nil {
		value, err := parseRFC3339(*payload.ScheduledAt)
		if err != nil {
			return db.PostInput{}, nil, errors.New("scheduled_at must be RFC3339")
		}
		scheduledAt = value
	}

	var mediaURL *string
	if payload.MediaURL != nil {
		trimmed := strings.TrimSpace(*payload.MediaURL)
		if trimmed != "" {
			mediaURL = &trimmed
		}
	}
	mediaType := model.NormalizeMediaType(payload.MediaType)
	if mediaURL != nil && mediaType == nil {
		mediaType = model.InferMediaTypeFromURL(*mediaURL)
	}

	if err := model.ValidateEditableInput(text, status, scheduledAt, mediaURL, mediaType); err != nil {
		return db.PostInput{}, nil, err
	}

	channelIDs := dedupeChannelIDs(payload.ChannelIDs)
	if status == model.StatusScheduled && len(channelIDs) == 0 {
		return db.PostInput{}, nil, errors.New("at least one channel is required when status is scheduled")
	}

	return db.PostInput{ScheduledAt: scheduledAt, Text: text, Status: status, MediaType: mediaType, MediaTypeSet: true, MediaURL: mediaURL}, channelIDs, nil
}

func (a *App) mapPostResponse(ctx context.Context, post model.Post) (postResponse, error) {
	channelIDs, err := a.Store.ListPostChannelIDs(ctx, post.ID)
	if err != nil {
		return postResponse{}, err
	}
	mediaType, err := a.Store.GetPostMediaType(ctx, post.ID)
	if err != nil {
		return postResponse{}, err
	}

	response := postResponse{
		ID:         post.ID,
		Text:       post.Text,
		Status:     string(post.Status),
		CreatedAt:  post.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  post.UpdatedAt.UTC().Format(time.RFC3339),
		FailCount:  post.FailCount,
		LastError:  post.LastError,
		MediaType:  mediaType,
		MediaURL:   post.MediaURL,
		ChannelIDs: channelIDs,
	}
	if post.ScheduledAt != nil {
		value := post.ScheduledAt.UTC().Format(time.RFC3339)
		response.ScheduledAt = &value
	}
	if post.SentAt != nil {
		value := post.SentAt.UTC().Format(time.RFC3339)
		response.SentAt = &value
	}
	if post.NextRetryAt != nil {
		value := post.NextRetryAt.UTC().Format(time.RFC3339)
		response.NextRetryAt = &value
	}
	return response, nil
}

func dedupeChannelIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	values := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		values = append(values, id)
	}
	return values
}
