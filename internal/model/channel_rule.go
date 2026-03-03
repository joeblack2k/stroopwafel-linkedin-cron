package model

import "time"

type ChannelRule struct {
	ChannelID      int64     `json:"channel_id"`
	MaxTextLength  *int      `json:"max_text_length,omitempty"`
	MaxHashtags    *int      `json:"max_hashtags,omitempty"`
	RequiredPhrase *string   `json:"required_phrase,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (r ChannelRule) Enabled() bool {
	return r.MaxTextLength != nil || r.MaxHashtags != nil || (r.RequiredPhrase != nil && *r.RequiredPhrase != "")
}
