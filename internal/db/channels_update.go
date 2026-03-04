package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"stroopwafel/internal/model"
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

	InstagramBusinessAccountID *string
	InstagramAPIBaseURL        *string

	LinkedInAccessTokenAction SecretAction
	LinkedInAccessToken       *string

	FacebookPageTokenAction SecretAction
	FacebookPageToken       *string

	InstagramAccessTokenAction SecretAction
	InstagramAccessToken       *string

	AuditActor  string
	AuditSource string
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

	linkedInToken, linkedInAction, err := applySecretAction(existing.LinkedInAccessToken, input.LinkedInAccessTokenAction, input.LinkedInAccessToken, "linkedin_access_token")
	if err != nil {
		return model.Channel{}, err
	}
	facebookToken, facebookAction, err := applySecretAction(existing.FacebookPageAccessToken, input.FacebookPageTokenAction, input.FacebookPageToken, "facebook_page_access_token")
	if err != nil {
		return model.Channel{}, err
	}
	instagramToken, instagramAction, err := applySecretAction(existing.InstagramAccessToken, input.InstagramAccessTokenAction, input.InstagramAccessToken, "instagram_access_token")
	if err != nil {
		return model.Channel{}, err
	}

	linkedInAuthorURN := applyNullableStringPatch(existing.LinkedInAuthorURN, input.LinkedInAuthorURN)
	linkedInAPIBaseURL := applyNullableStringPatch(existing.LinkedInAPIBaseURL, input.LinkedInAPIBaseURL)
	facebookPageID := applyNullableStringPatch(existing.FacebookPageID, input.FacebookPageID)
	facebookAPIBaseURL := applyNullableStringPatch(existing.FacebookAPIBaseURL, input.FacebookAPIBaseURL)
	instagramBusinessAccountID := applyNullableStringPatch(existing.InstagramBusinessAccountID, input.InstagramBusinessAccountID)
	instagramAPIBaseURL := applyNullableStringPatch(existing.InstagramAPIBaseURL, input.InstagramAPIBaseURL)

	candidate := ChannelInput{
		Type:                       existing.Type,
		DisplayName:                displayName,
		LinkedInAccessToken:        linkedInToken,
		LinkedInAuthorURN:          linkedInAuthorURN,
		LinkedInAPIBaseURL:         linkedInAPIBaseURL,
		FacebookPageAccessToken:    facebookToken,
		FacebookPageID:             facebookPageID,
		FacebookAPIBaseURL:         facebookAPIBaseURL,
		InstagramAccessToken:       instagramToken,
		InstagramBusinessAccountID: instagramBusinessAccountID,
		InstagramAPIBaseURL:        instagramAPIBaseURL,
	}
	status, validationErr := validateChannelConfig(candidate)
	lastError := nullableValidationError(validationErr)
	newLastError := validationErrorPointer(validationErr)

	changedFields := collectChangedChannelFields(
		existing,
		displayName,
		status,
		linkedInAuthorURN,
		linkedInAPIBaseURL,
		facebookPageID,
		facebookAPIBaseURL,
		instagramBusinessAccountID,
		instagramAPIBaseURL,
		newLastError,
		linkedInAction,
		facebookAction,
		instagramAction,
	)
	summary := buildChannelAuditSummary(changedFields, linkedInAction, facebookAction, instagramAction)

	metadataPayload := map[string]any{
		"source":         normalizeAuditSource(input.AuditSource),
		"changed_fields": changedFields,
		"status_before":  existing.Status,
		"status_after":   status,
		"secret_actions": map[string]string{
			"linkedin_access_token":      string(linkedInAction),
			"facebook_page_access_token": string(facebookAction),
			"instagram_access_token":     string(instagramAction),
		},
		"validation_error": validationErrorString(validationErr),
	}
	metadataBytes, err := json.Marshal(metadataPayload)
	if err != nil {
		return model.Channel{}, fmt.Errorf("marshal channel audit metadata: %w", err)
	}
	metadataValue := string(metadataBytes)

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Channel{}, fmt.Errorf("begin update channel tx: %w", err)
	}

	result, err := tx.ExecContext(
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
		     facebook_api_base_url = ?,
		     instagram_access_token = ?,
		     instagram_business_account_id = ?,
		     instagram_api_base_url = ?
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
		nullableString(instagramToken),
		nullableString(instagramBusinessAccountID),
		nullableString(instagramAPIBaseURL),
		id,
	)
	if err != nil {
		_ = tx.Rollback()
		return model.Channel{}, fmt.Errorf("update channel %d: %w", id, err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return model.Channel{}, fmt.Errorf("read rows affected for update channel %d: %w", id, err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return model.Channel{}, ErrNotFound
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO channel_audit_events(channel_id, event_type, actor, summary, metadata, created_at)
		 VALUES(?, ?, ?, ?, ?, ?)`,
		id,
		"channel.updated",
		normalizeAuditActor(input.AuditActor),
		summary,
		metadataValue,
		formatDBTime(now),
	); err != nil {
		_ = tx.Rollback()
		return model.Channel{}, fmt.Errorf("record channel audit event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.Channel{}, fmt.Errorf("commit update channel tx: %w", err)
	}

	return s.GetChannel(ctx, id)
}

func applySecretAction(current *string, action SecretAction, replacement *string, fieldName string) (*string, SecretAction, error) {
	normalizedAction, err := ParseSecretAction(string(action))
	if err != nil {
		return nil, "", err
	}

	switch normalizedAction {
	case SecretActionKeep:
		return copyNullableString(current), normalizedAction, nil
	case SecretActionClear:
		return nil, normalizedAction, nil
	case SecretActionReplace:
		if replacement == nil {
			return nil, "", fmt.Errorf("%s is required when action=replace", fieldName)
		}
		trimmed := strings.TrimSpace(*replacement)
		if trimmed == "" {
			return nil, "", fmt.Errorf("%s is required when action=replace", fieldName)
		}
		return &trimmed, normalizedAction, nil
	default:
		return nil, "", fmt.Errorf("invalid secret action: %s", action)
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

func collectChangedChannelFields(
	existing model.Channel,
	displayName string,
	status string,
	linkedInAuthorURN *string,
	linkedInAPIBaseURL *string,
	facebookPageID *string,
	facebookAPIBaseURL *string,
	instagramBusinessAccountID *string,
	instagramAPIBaseURL *string,
	lastError *string,
	linkedInAction SecretAction,
	facebookAction SecretAction,
	instagramAction SecretAction,
) []string {
	changed := make([]string, 0, 13)
	if existing.DisplayName != displayName {
		changed = append(changed, "display_name")
	}
	if existing.Status != status {
		changed = append(changed, "status")
	}
	if nullableStringsDiffer(existing.LinkedInAuthorURN, linkedInAuthorURN) {
		changed = append(changed, "linkedin_author_urn")
	}
	if nullableStringsDiffer(existing.LinkedInAPIBaseURL, linkedInAPIBaseURL) {
		changed = append(changed, "linkedin_api_base_url")
	}
	if nullableStringsDiffer(existing.FacebookPageID, facebookPageID) {
		changed = append(changed, "facebook_page_id")
	}
	if nullableStringsDiffer(existing.FacebookAPIBaseURL, facebookAPIBaseURL) {
		changed = append(changed, "facebook_api_base_url")
	}
	if nullableStringsDiffer(existing.InstagramBusinessAccountID, instagramBusinessAccountID) {
		changed = append(changed, "instagram_business_account_id")
	}
	if nullableStringsDiffer(existing.InstagramAPIBaseURL, instagramAPIBaseURL) {
		changed = append(changed, "instagram_api_base_url")
	}
	if nullableStringsDiffer(existing.LastError, lastError) {
		changed = append(changed, "last_error")
	}
	if linkedInAction != SecretActionKeep {
		changed = append(changed, "linkedin_access_token")
	}
	if facebookAction != SecretActionKeep {
		changed = append(changed, "facebook_page_access_token")
	}
	if instagramAction != SecretActionKeep {
		changed = append(changed, "instagram_access_token")
	}
	return changed
}

func buildChannelAuditSummary(changedFields []string, linkedInAction, facebookAction, instagramAction SecretAction) string {
	segments := make([]string, 0, 2)
	if len(changedFields) > 0 {
		segments = append(segments, "updated "+strings.Join(changedFields, ", "))
	}

	secretActions := make([]string, 0, 2)
	if linkedInAction != SecretActionKeep {
		secretActions = append(secretActions, "linkedin_access_token="+string(linkedInAction))
	}
	if facebookAction != SecretActionKeep {
		secretActions = append(secretActions, "facebook_page_access_token="+string(facebookAction))
	}
	if instagramAction != SecretActionKeep {
		secretActions = append(secretActions, "instagram_access_token="+string(instagramAction))
	}
	if len(secretActions) > 0 {
		segments = append(segments, "secrets "+strings.Join(secretActions, ", "))
	}

	if len(segments) == 0 {
		return "channel update with no persisted changes"
	}
	return strings.Join(segments, "; ")
}

func normalizeAuditActor(actor string) string {
	trimmed := strings.TrimSpace(actor)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func normalizeAuditSource(source string) string {
	trimmed := strings.ToLower(strings.TrimSpace(source))
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func validationErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func validationErrorPointer(err error) *string {
	if err == nil {
		return nil
	}
	value := err.Error()
	return &value
}

func nullableStringsDiffer(left, right *string) bool {
	if left == nil && right == nil {
		return false
	}
	if left == nil || right == nil {
		return true
	}
	return *left != *right
}

func copyNullableString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
