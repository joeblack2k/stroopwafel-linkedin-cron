package model

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type PostStatus string

const (
	StatusDraft     PostStatus = "draft"
	StatusScheduled PostStatus = "scheduled"
	StatusSent      PostStatus = "sent"
	StatusFailed    PostStatus = "failed"
)

type Post struct {
	ID          int64      `json:"id"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	Text        string     `json:"text"`
	Status      PostStatus `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
	FailCount   int        `json:"fail_count"`
	LastError   *string    `json:"last_error,omitempty"`
	MediaURL    *string    `json:"media_url,omitempty"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
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

func ValidateEditableInput(text string, status PostStatus, scheduledAt *time.Time, mediaURL *string) error {
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
	return nil
}
