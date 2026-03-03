package model

import "time"

type ChannelAuditEvent struct {
	ID        int64     `json:"id"`
	ChannelID int64     `json:"channel_id"`
	EventType string    `json:"event_type"`
	Actor     string    `json:"actor"`
	Summary   string    `json:"summary"`
	Metadata  *string   `json:"metadata,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
