package model

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type PostStatus string

type MediaType string

const (
	StatusDraft     PostStatus = "draft"
	StatusScheduled PostStatus = "scheduled"
	StatusSent      PostStatus = "sent"
	StatusFailed    PostStatus = "failed"

	MediaTypeLink  MediaType = "link"
	MediaTypeImage MediaType = "image"
	MediaTypeVideo MediaType = "video"
)

type Post struct {
	ID              int64      `json:"id"`
	ScheduledAt     *time.Time `json:"scheduled_at,omitempty"`
	Text            string     `json:"text"`
	Status          PostStatus `json:"status"`
	ApprovalPending bool       `json:"approval_pending"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	SentAt          *time.Time `json:"sent_at,omitempty"`
	FailCount       int        `json:"fail_count"`
	LastError       *string    `json:"last_error,omitempty"`
	MediaType       *string    `json:"media_type,omitempty"`
	MediaURL        *string    `json:"media_url,omitempty"`
	NextRetryAt     *time.Time `json:"next_retry_at,omitempty"`
}

func (s PostStatus) Valid() bool {
	switch s {
	case StatusDraft, StatusScheduled, StatusSent, StatusFailed:
		return true
	default:
		return false
	}
}

func (s PostStatus) Editable() bool {
	switch s {
	case StatusDraft, StatusScheduled:
		return true
	default:
		return false
	}
}

func ValidateEditableInput(text string, status PostStatus, scheduledAt *time.Time, mediaURL *string, mediaType ...*string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("text is required")
	}
	if !status.Valid() {
		return fmt.Errorf("invalid status: %s", status)
	}
	if !status.Editable() {
		return errors.New("status must be draft or scheduled")
	}
	if status == StatusScheduled && scheduledAt == nil {
		return errors.New("scheduled_at is required when status is scheduled")
	}
	if mediaURL != nil {
		trimmed := strings.TrimSpace(*mediaURL)
		if trimmed != "" {
			parsed, err := url.ParseRequestURI(trimmed)
			if err != nil || parsed.Scheme == "" || parsed.Host == "" {
				return errors.New("media_url must be empty or a valid absolute URL")
			}
		}
	}

	if len(mediaType) > 0 {
		normalizedMediaType := NormalizeMediaType(mediaType[0])
		if normalizedMediaType != nil && !IsSupportedMediaType(*normalizedMediaType) {
			return errors.New("media_type must be one of: link, image, video")
		}
		if normalizedMediaType != nil && mediaURL == nil {
			return errors.New("media_url is required when media_type is set")
		}
	}

	return nil
}

func NormalizeMediaType(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.ToLower(strings.TrimSpace(*value))
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func IsSupportedMediaType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(MediaTypeLink), string(MediaTypeImage), string(MediaTypeVideo):
		return true
	default:
		return false
	}
}

func InferMediaTypeFromURL(mediaURL string) *string {
	trimmed := strings.ToLower(strings.TrimSpace(mediaURL))
	if trimmed == "" {
		return nil
	}
	if queryIndex := strings.IndexAny(trimmed, "?#"); queryIndex >= 0 {
		trimmed = trimmed[:queryIndex]
	}

	var detected string
	switch {
	case strings.HasSuffix(trimmed, ".png"), strings.HasSuffix(trimmed, ".jpg"), strings.HasSuffix(trimmed, ".jpeg"), strings.HasSuffix(trimmed, ".gif"), strings.HasSuffix(trimmed, ".webp"):
		detected = string(MediaTypeImage)
	case strings.HasSuffix(trimmed, ".mp4"), strings.HasSuffix(trimmed, ".mov"), strings.HasSuffix(trimmed, ".webm"):
		detected = string(MediaTypeVideo)
	default:
		detected = string(MediaTypeLink)
	}
	return &detected
}
