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
	ChannelTypeInstagram  ChannelType = "instagram"
	ChannelTypeDryRun     ChannelType = "dry-run"
	ChannelStatusActive   string      = "active"
	ChannelStatusError    string      = "error"
	ChannelStatusDisabled string      = "disabled"
)

type ChannelCapabilities struct {
	SupportsMedia bool     `json:"supports_media"`
	MediaTypes    []string `json:"media_types,omitempty"`
	RequiresMedia bool     `json:"requires_media"`
}

type Channel struct {
	ID                         int64       `json:"id"`
	Type                       ChannelType `json:"type"`
	DisplayName                string      `json:"display_name"`
	Status                     string      `json:"status"`
	CreatedAt                  time.Time   `json:"created_at"`
	UpdatedAt                  time.Time   `json:"updated_at"`
	LastTestAt                 *time.Time  `json:"last_test_at,omitempty"`
	LastError                  *string     `json:"last_error,omitempty"`
	LinkedInAccessToken        *string     `json:"-"`
	LinkedInAuthorURN          *string     `json:"-"`
	LinkedInAPIBaseURL         *string     `json:"-"`
	FacebookPageAccessToken    *string     `json:"-"`
	FacebookPageID             *string     `json:"-"`
	FacebookAPIBaseURL         *string     `json:"-"`
	InstagramAccessToken       *string     `json:"-"`
	InstagramBusinessAccountID *string     `json:"-"`
	InstagramAPIBaseURL        *string     `json:"-"`
}

func (t ChannelType) Valid() bool {
	switch t {
	case ChannelTypeLinkedIn, ChannelTypeFacebook, ChannelTypeInstagram, ChannelTypeDryRun:
		return true
	default:
		return false
	}
}

func ChannelCapabilitiesForType(channelType ChannelType) ChannelCapabilities {
	switch channelType {
	case ChannelTypeLinkedIn:
		return ChannelCapabilities{SupportsMedia: true, MediaTypes: []string{string(MediaTypeLink)}}
	case ChannelTypeFacebook:
		return ChannelCapabilities{SupportsMedia: true, MediaTypes: []string{string(MediaTypeLink)}}
	case ChannelTypeInstagram:
		return ChannelCapabilities{SupportsMedia: true, MediaTypes: []string{string(MediaTypeImage), string(MediaTypeVideo)}, RequiresMedia: true}
	case ChannelTypeDryRun:
		return ChannelCapabilities{SupportsMedia: true, MediaTypes: []string{string(MediaTypeLink), string(MediaTypeImage), string(MediaTypeVideo)}}
	default:
		return ChannelCapabilities{}
	}
}

func (c Channel) Capabilities() ChannelCapabilities {
	return ChannelCapabilitiesForType(c.Type)
}

func (c ChannelCapabilities) SupportsMediaType(mediaType string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(mediaType))
	if trimmed == "" {
		return true
	}
	if len(c.MediaTypes) == 0 {
		return true
	}
	for _, value := range c.MediaTypes {
		if strings.EqualFold(strings.TrimSpace(value), trimmed) {
			return true
		}
	}
	return false
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
