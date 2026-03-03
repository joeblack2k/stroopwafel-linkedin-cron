package model

import (
	"errors"
	"strings"
	"time"
)

type ChannelType string

const (
	ChannelTypeLinkedIn   ChannelType = "linkedin"
	ChannelTypeFacebook   ChannelType = "facebook-page"
	ChannelTypeDryRun     ChannelType = "dry-run"
	ChannelStatusActive   string      = "active"
	ChannelStatusError    string      = "error"
	ChannelStatusDisabled string      = "disabled"
)

type Channel struct {
	ID                      int64       `json:"id"`
	Type                    ChannelType `json:"type"`
	DisplayName             string      `json:"display_name"`
	Status                  string      `json:"status"`
	CreatedAt               time.Time   `json:"created_at"`
	UpdatedAt               time.Time   `json:"updated_at"`
	LastTestAt              *time.Time  `json:"last_test_at,omitempty"`
	LastError               *string     `json:"last_error,omitempty"`
	LinkedInAccessToken     *string     `json:"-"`
	LinkedInAuthorURN       *string     `json:"-"`
	LinkedInAPIBaseURL      *string     `json:"-"`
	FacebookPageAccessToken *string     `json:"-"`
	FacebookPageID          *string     `json:"-"`
	FacebookAPIBaseURL      *string     `json:"-"`
}

func (t ChannelType) Valid() bool {
	switch t {
	case ChannelTypeLinkedIn, ChannelTypeFacebook, ChannelTypeDryRun:
		return true
	default:
		return false
	}
}

func ValidateChannelInput(channelType ChannelType, displayName string) error {
	if !channelType.Valid() {
		return errors.New("invalid channel type")
	}
	if strings.TrimSpace(displayName) == "" {
		return errors.New("display_name is required")
	}
	return nil
}
