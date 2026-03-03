package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"linkedin-cron/internal/model"
)

func TestChannelCRUDAndPostLinks(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "channels.db")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := NewStore(database)
	post, err := store.CreatePost(context.Background(), PostInput{Text: "channel post", Status: model.StatusDraft})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	channel, err := store.CreateChannel(context.Background(), ChannelInput{
		Type:                model.ChannelTypeLinkedIn,
		DisplayName:         "LinkedIn Main",
		LinkedInAccessToken: ptrString("token"),
		LinkedInAuthorURN:   ptrString("urn:li:person:abc"),
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if channel.ID == 0 {
		t.Fatal("expected channel id")
	}

	channels, err := store.ListChannels(context.Background())
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != 1 {
		t.Fatalf("expected one channel, got %d", len(channels))
	}

	if err := store.ReplacePostChannels(context.Background(), post.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("replace post channels: %v", err)
	}

	linked, err := store.ListPostChannelIDs(context.Background(), post.ID)
	if err != nil {
		t.Fatalf("list post channel ids: %v", err)
	}
	if len(linked) != 1 || linked[0] != channel.ID {
		t.Fatalf("unexpected post channel ids: %+v", linked)
	}

	tested, err := store.TestChannel(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("test channel: %v", err)
	}
	if tested.Status != model.ChannelStatusActive {
		t.Fatalf("expected active status, got %s", tested.Status)
	}

	if err := store.DeleteChannel(context.Background(), channel.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}

	remaining, err := store.ListChannels(context.Background())
	if err != nil {
		t.Fatalf("list channels after delete: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected zero channels after delete, got %d", len(remaining))
	}
}

func ptrString(value string) *string {
	return &value
}

func TestUpdateChannelSecretActions(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "channels-update.db")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if _, err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := NewStore(database)
	channel, err := store.CreateChannel(context.Background(), ChannelInput{
		Type:                model.ChannelTypeLinkedIn,
		DisplayName:         "LinkedIn Main",
		LinkedInAccessToken: ptrString("token-old"),
		LinkedInAuthorURN:   ptrString("urn:li:organization:123"),
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	updated, err := store.UpdateChannel(context.Background(), channel.ID, ChannelUpdateInput{
		DisplayName:               ptrString("LinkedIn Renamed"),
		LinkedInAuthorURN:         ptrString("urn:li:organization:456"),
		LinkedInAccessTokenAction: SecretActionKeep,
		FacebookPageTokenAction:   SecretActionKeep,
		LinkedInAccessToken:       ptrString(""),
		FacebookPageToken:         ptrString(""),
		LinkedInAPIBaseURL:        ptrString(""),
		FacebookAPIBaseURL:        ptrString(""),
		FacebookPageID:            ptrString(""),
		AuditActor:                "unit-test",
		AuditSource:               "test",
	})
	if err != nil {
		t.Fatalf("update keep action: %v", err)
	}
	if updated.DisplayName != "LinkedIn Renamed" {
		t.Fatalf("expected renamed display name, got %q", updated.DisplayName)
	}
	if got := derefNullableString(updated.LinkedInAccessToken); got != "token-old" {
		t.Fatalf("expected token-old to be kept, got %q", got)
	}

	updated, err = store.UpdateChannel(context.Background(), channel.ID, ChannelUpdateInput{
		LinkedInAccessTokenAction: SecretActionReplace,
		LinkedInAccessToken:       ptrString("token-new"),
		FacebookPageTokenAction:   SecretActionKeep,
		AuditActor:                "unit-test",
		AuditSource:               "test",
	})
	if err != nil {
		t.Fatalf("update replace action: %v", err)
	}
	if got := derefNullableString(updated.LinkedInAccessToken); got != "token-new" {
		t.Fatalf("expected token-new, got %q", got)
	}

	updated, err = store.UpdateChannel(context.Background(), channel.ID, ChannelUpdateInput{
		LinkedInAccessTokenAction: SecretActionClear,
		FacebookPageTokenAction:   SecretActionKeep,
		AuditActor:                "unit-test",
		AuditSource:               "test",
	})
	if err != nil {
		t.Fatalf("update clear action: %v", err)
	}
	if updated.LinkedInAccessToken != nil {
		t.Fatalf("expected linkedin token to be cleared")
	}
	if updated.Status != model.ChannelStatusError {
		t.Fatalf("expected channel status error after clearing required token, got %s", updated.Status)
	}
	if updated.LastError == nil || *updated.LastError == "" {
		t.Fatalf("expected last_error after clearing required token")
	}

	_, err = store.UpdateChannel(context.Background(), channel.ID, ChannelUpdateInput{
		LinkedInAccessTokenAction: SecretActionReplace,
		FacebookPageTokenAction:   SecretActionKeep,
	})
	if err == nil {
		t.Fatal("expected error when replacing token without a value")
	}

	auditCount, err := store.CountChannelAuditEvents(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("count channel audit events: %v", err)
	}
	if auditCount != 3 {
		t.Fatalf("expected 3 channel audit events, got %d", auditCount)
	}

	events, err := store.ListChannelAuditEvents(context.Background(), channel.ID, 10, 0)
	if err != nil {
		t.Fatalf("list channel audit events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 channel audit events, got %d", len(events))
	}
	if events[0].EventType != "channel.updated" {
		t.Fatalf("expected event_type=channel.updated, got %q", events[0].EventType)
	}
	if strings.TrimSpace(events[0].Actor) == "" {
		t.Fatalf("expected non-empty actor")
	}
	if events[0].Metadata == nil || strings.TrimSpace(*events[0].Metadata) == "" {
		t.Fatalf("expected non-empty metadata")
	}
}
