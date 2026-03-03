package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"linkedin-cron/internal/model"
)

type SecretAction string

const (
	SecretActionKeep    SecretAction = "keep"
	SecretActionReplace SecretAction = "replace"
	SecretActionClear   SecretAction = "clear"
)

type ChannelUpdateInput struct {
	DisplayName *string

	LinkedInAuthorURN  *string
	LinkedInAPIBaseURL *string

	FacebookPageID     *string
	FacebookAPIBaseURL *string

	LinkedInAccessTokenAction SecretAction
	LinkedInAccessToken       *string

	FacebookPageTokenAction SecretAction
	FacebookPageToken       *string
}

func ParseSecretAction(value string) (SecretAction, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(SecretActionKeep):
		return SecretActionKeep, nil
	case string(SecretActionReplace):
		return SecretActionReplace, nil
	case string(SecretActionClear):
		return SecretActionClear, nil
	default:
		return "", fmt.Errorf("invalid secret action: %s", value)
	}
}

func (s *Store) UpdateChannel(ctx context.Context, id int64, input ChannelUpdateInput) (model.Channel, error) {
	existing, err := s.GetChannel(ctx, id)
	if err != nil {
		return model.Channel{}, err
	}

	displayName := strings.TrimSpace(existing.DisplayName)
	if input.DisplayName != nil {
		displayName = strings.TrimSpace(*input.DisplayName)
	}
	if displayName == "" {
		return model.Channel{}, fmt.Errorf("display_name is required")
	}

	linkedInToken, err := applySecretAction(existing.LinkedInAccessToken, input.LinkedInAccessTokenAction, input.LinkedInAccessToken, "linkedin_access_token")
	if err != nil {
		return model.Channel{}, err
	}
	facebookToken, err := applySecretAction(existing.FacebookPageAccessToken, input.FacebookPageTokenAction, input.FacebookPageToken, "facebook_page_access_token")
	if err != nil {
		return model.Channel{}, err
	}

	linkedInAuthorURN := applyNullableStringPatch(existing.LinkedInAuthorURN, input.LinkedInAuthorURN)
	linkedInAPIBaseURL := applyNullableStringPatch(existing.LinkedInAPIBaseURL, input.LinkedInAPIBaseURL)
	facebookPageID := applyNullableStringPatch(existing.FacebookPageID, input.FacebookPageID)
	facebookAPIBaseURL := applyNullableStringPatch(existing.FacebookAPIBaseURL, input.FacebookAPIBaseURL)

	candidate := ChannelInput{
		Type:                    existing.Type,
		DisplayName:             displayName,
		LinkedInAccessToken:     linkedInToken,
		LinkedInAuthorURN:       linkedInAuthorURN,
		LinkedInAPIBaseURL:      linkedInAPIBaseURL,
		FacebookPageAccessToken: facebookToken,
		FacebookPageID:          facebookPageID,
		FacebookAPIBaseURL:      facebookAPIBaseURL,
	}
	status, validationErr := validateChannelConfig(candidate)
	lastError := nullableValidationError(validationErr)

	now := time.Now().UTC()
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE channels
		 SET display_name = ?,
		     status = ?,
		     updated_at = ?,
		     last_error = ?,
		     linkedin_access_token = ?,
		     linkedin_author_urn = ?,
		     linkedin_api_base_url = ?,
		     facebook_page_access_token = ?,
		     facebook_page_id = ?,
		     facebook_api_base_url = ?
		 WHERE id = ?`,
		displayName,
		status,
		formatDBTime(now),
		lastError,
		nullableString(linkedInToken),
		nullableString(linkedInAuthorURN),
		nullableString(linkedInAPIBaseURL),
		nullableString(facebookToken),
		nullableString(facebookPageID),
		nullableString(facebookAPIBaseURL),
		id,
	)
	if err != nil {
		return model.Channel{}, fmt.Errorf("update channel %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.Channel{}, fmt.Errorf("read rows affected for update channel %d: %w", id, err)
	}
	if affected == 0 {
		return model.Channel{}, ErrNotFound
	}

	return s.GetChannel(ctx, id)
}

func applySecretAction(current *string, action SecretAction, replacement *string, fieldName string) (*string, error) {
	normalizedAction, err := ParseSecretAction(string(action))
	if err != nil {
		return nil, err
	}

	switch normalizedAction {
	case SecretActionKeep:
		return copyNullableString(current), nil
	case SecretActionClear:
		return nil, nil
	case SecretActionReplace:
		if replacement == nil {
			return nil, fmt.Errorf("%s is required when action=replace", fieldName)
		}
		trimmed := strings.TrimSpace(*replacement)
		if trimmed == "" {
			return nil, fmt.Errorf("%s is required when action=replace", fieldName)
		}
		return &trimmed, nil
	default:
		return nil, fmt.Errorf("invalid secret action: %s", action)
	}
}

func applyNullableStringPatch(current *string, incoming *string) *string {
	if incoming == nil {
		return copyNullableString(current)
	}
	trimmed := strings.TrimSpace(*incoming)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func copyNullableString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
