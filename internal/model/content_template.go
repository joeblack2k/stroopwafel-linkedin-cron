package model

import "time"

type ContentTemplate struct {
	ID           int64        `json:"id"`
	Name         string       `json:"name"`
	Description  *string      `json:"description,omitempty"`
	Body         string       `json:"body"`
	ChannelType  *ChannelType `json:"channel_type,omitempty"`
	MediaAssetID *int64       `json:"media_asset_id,omitempty"`
	Tags         []string     `json:"tags,omitempty"`
	IsActive     bool         `json:"is_active"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}
