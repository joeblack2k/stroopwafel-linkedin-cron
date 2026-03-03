package model

import "time"

const (
	PublishAttemptStatusSent   = "sent"
	PublishAttemptStatusRetry  = "retry"
	PublishAttemptStatusFailed = "failed"
)

type PublishAttempt struct {
	ID            int64      `json:"id"`
	PostID        int64      `json:"post_id"`
	ChannelID     int64      `json:"channel_id"`
	AttemptNo     int        `json:"attempt_no"`
	AttemptedAt   time.Time  `json:"attempted_at"`
	Status        string     `json:"status"`
	Error         *string    `json:"error,omitempty"`
	ErrorCategory *string    `json:"error_category,omitempty"`
	RetryAt       *time.Time `json:"retry_at,omitempty"`
	ExternalID    *string    `json:"external_id,omitempty"`
	Permalink     *string    `json:"permalink,omitempty"`
	ScreenshotURL *string    `json:"screenshot_url,omitempty"`
}
