package model

import "time"

type MediaAsset struct {
	ID         int64             `json:"id"`
	MediaURL   string            `json:"media_url"`
	MediaType  string            `json:"media_type"`
	Filename   *string           `json:"filename,omitempty"`
	SizeBytes  int64             `json:"size_bytes"`
	StoredPath *string           `json:"stored_path,omitempty"`
	Source     string            `json:"source"`
	Tags       []string          `json:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}
