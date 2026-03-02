package handlers

import (
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
	MediaURL    *string `json:"media_url"`
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
	MediaURL    *string `json:"media_url,omitempty"`
	NextRetryAt *string `json:"next_retry_at,omitempty"`
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
		response = append(response, mapPostResponse(post))
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

	writeJSON(w, http.StatusOK, mapPostResponse(post))
}

func (a *App) APICreatePost(w http.ResponseWriter, r *http.Request) {
	var payload postPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	input, err := parsePostPayload(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, err := a.Store.CreatePost(r.Context(), input)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to create post")
		return
	}
	writeJSON(w, http.StatusCreated, mapPostResponse(created))
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

	input, err := parsePostPayload(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
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
	writeJSON(w, http.StatusOK, mapPostResponse(updated))
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
	writeJSON(w, http.StatusOK, mapPostResponse(post))
}

func parsePostPayload(payload postPayload) (db.PostInput, error) {
	status := model.PostStatus(strings.TrimSpace(payload.Status))
	text := strings.TrimSpace(payload.Text)

	var scheduledAt *time.Time
	if payload.ScheduledAt != nil {
		value, err := parseRFC3339(*payload.ScheduledAt)
		if err != nil {
			return db.PostInput{}, errors.New("scheduled_at must be RFC3339")
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

	if err := model.ValidateEditableInput(text, status, scheduledAt, mediaURL); err != nil {
		return db.PostInput{}, err
	}

	return db.PostInput{ScheduledAt: scheduledAt, Text: text, Status: status, MediaURL: mediaURL}, nil
}

func mapPostResponse(post model.Post) postResponse {
	response := postResponse{
		ID:        post.ID,
		Text:      post.Text,
		Status:    string(post.Status),
		CreatedAt: post.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: post.UpdatedAt.UTC().Format(time.RFC3339),
		FailCount: post.FailCount,
		LastError: post.LastError,
		MediaURL:  post.MediaURL,
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
	return response
}
