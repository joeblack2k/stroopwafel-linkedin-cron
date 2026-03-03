package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
)

type attemptResponse struct {
	ID          int64   `json:"id"`
	PostID      int64   `json:"post_id"`
	ChannelID   int64   `json:"channel_id"`
	ChannelName string  `json:"channel_name"`
	ChannelType string  `json:"channel_type"`
	AttemptNo   int     `json:"attempt_no"`
	AttemptedAt string  `json:"attempted_at"`
	Status      string  `json:"status"`
	Error       *string `json:"error,omitempty"`
	RetryAt     *string `json:"retry_at,omitempty"`
	ExternalID  *string `json:"external_id,omitempty"`
}

type attemptsListResponse struct {
	Items      []attemptResponse  `json:"items"`
	Pagination paginationResponse `json:"pagination"`
}

type bulkSendNowPayload struct {
	PostIDs []int64 `json:"post_ids"`
}

type bulkChannelsPayload struct {
	PostIDs    []int64 `json:"post_ids"`
	ChannelIDs []int64 `json:"channel_ids"`
}

type bulkOperationResult struct {
	Requested int      `json:"requested"`
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
}

func (a *App) APIListPostAttempts(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid post id")
		return
	}

	if _, err := a.Store.GetPost(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "post not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load post")
		return
	}

	statusFilter := normalizeAttemptStatus(r.URL.Query().Get("status"))
	var channelFilter *int64
	if channelRaw := strings.TrimSpace(r.URL.Query().Get("channel_id")); channelRaw != "" {
		parsed, parseErr := parseID(channelRaw)
		if parseErr != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid channel_id")
			return
		}
		channelFilter = &parsed
	}

	attemptedFrom, attemptedTo, err := parseAttemptedRangeRFC3339(r.URL.Query().Get("attempted_from"), r.URL.Query().Get("attempted_to"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"), 200)
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	total, err := a.Store.CountPublishAttemptsForPost(r.Context(), id, channelFilter, statusFilter, attemptedFrom, attemptedTo)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to count post attempts")
		return
	}

	attempts, err := a.Store.ListPublishAttemptsForPost(r.Context(), id, channelFilter, statusFilter, attemptedFrom, attemptedTo, limit, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list post attempts")
		return
	}

	channels, err := a.Store.ListChannelsForPost(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load channels")
		return
	}
	channelMap := make(map[int64]model.Channel, len(channels))
	for _, channel := range channels {
		channelMap[channel.ID] = channel
	}

	response := make([]attemptResponse, 0, len(attempts))
	for _, attempt := range attempts {
		channel := channelMap[attempt.ChannelID]
		item := attemptResponse{
			ID:          attempt.ID,
			PostID:      attempt.PostID,
			ChannelID:   attempt.ChannelID,
			ChannelName: channel.DisplayName,
			ChannelType: string(channel.Type),
			AttemptNo:   attempt.AttemptNo,
			AttemptedAt: attempt.AttemptedAt.UTC().Format(time.RFC3339),
			Status:      attempt.Status,
			Error:       attempt.Error,
			ExternalID:  attempt.ExternalID,
		}
		if item.ChannelName == "" {
			item.ChannelName = fmt.Sprintf("channel-%d", attempt.ChannelID)
		}
		if attempt.RetryAt != nil {
			value := attempt.RetryAt.UTC().Format(time.RFC3339)
			item.RetryAt = &value
		}
		response = append(response, item)
	}

	writeJSON(w, http.StatusOK, attemptsListResponse{
		Items:      response,
		Pagination: buildPagination(limit, offset, total),
	})
}

func (a *App) APIBulkSendNowPosts(w http.ResponseWriter, r *http.Request) {
	var payload bulkSendNowPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	postIDs := dedupeChannelIDs(payload.PostIDs)
	if len(postIDs) == 0 {
		writeAPIError(w, http.StatusBadRequest, "post_ids must include at least one id")
		return
	}

	result := bulkOperationResult{Requested: len(postIDs), Errors: make([]string, 0)}
	for _, postID := range postIDs {
		if err := a.Scheduler.SendNow(r.Context(), postID); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("post %d: %v", postID, err))
			continue
		}
		result.Succeeded++
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *App) APIBulkSetPostChannels(w http.ResponseWriter, r *http.Request) {
	var payload bulkChannelsPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	postIDs := dedupeChannelIDs(payload.PostIDs)
	channelIDs := dedupeChannelIDs(payload.ChannelIDs)
	if len(postIDs) == 0 {
		writeAPIError(w, http.StatusBadRequest, "post_ids must include at least one id")
		return
	}

	posts, err := a.Store.ListPostsByIDs(r.Context(), postIDs)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load selected posts")
		return
	}
	if len(posts) != len(postIDs) {
		writeAPIError(w, http.StatusBadRequest, "one or more post_ids were not found")
		return
	}

	if len(channelIDs) == 0 {
		for _, post := range posts {
			if post.Status == model.StatusScheduled {
				writeAPIError(w, http.StatusBadRequest, "scheduled posts must keep at least one channel")
				return
			}
		}
	}

	result := bulkOperationResult{Requested: len(postIDs), Errors: make([]string, 0)}
	for _, postID := range postIDs {
		if replaceErr := a.Store.ReplacePostChannels(r.Context(), postID, channelIDs); replaceErr != nil {
			result.Failed++
			result.Errors = append(result.Errors, fmt.Sprintf("post %d: %v", postID, replaceErr))
			continue
		}
		result.Succeeded++
	}

	writeJSON(w, http.StatusOK, result)
}
