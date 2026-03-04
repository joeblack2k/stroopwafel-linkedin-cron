package model

import "time"

const (
	DefaultChannelMaxRetries              = 3
	DefaultChannelBackoffFirstSeconds     = 60
	DefaultChannelBackoffSecondSeconds    = 300
	DefaultChannelBackoffThirdSeconds     = 900
	DefaultChannelRateLimitBackoffSeconds = 1800
)

type ChannelRetryPolicy struct {
	ChannelID               int64     `json:"channel_id"`
	MaxRetries              int       `json:"max_retries"`
	BackoffFirstSeconds     int       `json:"backoff_first_seconds"`
	BackoffSecondSeconds    int       `json:"backoff_second_seconds"`
	BackoffThirdSeconds     int       `json:"backoff_third_seconds"`
	RateLimitBackoffSeconds int       `json:"rate_limit_backoff_seconds"`
	MaxPostsPerDay          *int      `json:"max_posts_per_day,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func DefaultChannelRetryPolicy(channelID int64) ChannelRetryPolicy {
	return ChannelRetryPolicy{
		ChannelID:               channelID,
		MaxRetries:              DefaultChannelMaxRetries,
		BackoffFirstSeconds:     DefaultChannelBackoffFirstSeconds,
		BackoffSecondSeconds:    DefaultChannelBackoffSecondSeconds,
		BackoffThirdSeconds:     DefaultChannelBackoffThirdSeconds,
		RateLimitBackoffSeconds: DefaultChannelRateLimitBackoffSeconds,
	}
}

func (p ChannelRetryPolicy) HasDailyLimit() bool {
	return p.MaxPostsPerDay != nil && *p.MaxPostsPerDay > 0
}
