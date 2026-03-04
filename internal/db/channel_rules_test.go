package db

import (
	"context"
	"path/filepath"
	"testing"

	"stroopwafel/internal/model"
)

func TestChannelRulesUpsertAndClear(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "channel-rules.db")
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
		LinkedInAccessToken: ptrStringRule("token"),
		LinkedInAuthorURN:   ptrStringRule("urn:li:person:1"),
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	maxTextLength := 2000
	maxHashtags := 5
	requiredPhrase := "boek nu"
	updated, err := store.UpsertChannelRule(context.Background(), channel.ID, ChannelRuleInput{
		MaxTextLength:  &maxTextLength,
		MaxHashtags:    &maxHashtags,
		RequiredPhrase: &requiredPhrase,
	})
	if err != nil {
		t.Fatalf("upsert channel rule: %v", err)
	}
	if updated.MaxTextLength == nil || *updated.MaxTextLength != 2000 {
		t.Fatalf("expected max_text_length=2000, got %#v", updated.MaxTextLength)
	}
	if updated.MaxHashtags == nil || *updated.MaxHashtags != 5 {
		t.Fatalf("expected max_hashtags=5, got %#v", updated.MaxHashtags)
	}
	if updated.RequiredPhrase == nil || *updated.RequiredPhrase != "boek nu" {
		t.Fatalf("expected required_phrase=boek nu, got %#v", updated.RequiredPhrase)
	}

	loaded, found, err := store.GetChannelRule(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get channel rule: %v", err)
	}
	if !found {
		t.Fatal("expected channel rule to be found")
	}
	if loaded.MaxTextLength == nil || *loaded.MaxTextLength != 2000 {
		t.Fatalf("expected loaded max_text_length=2000, got %#v", loaded.MaxTextLength)
	}

	cleared, err := store.UpsertChannelRule(context.Background(), channel.ID, ChannelRuleInput{})
	if err != nil {
		t.Fatalf("clear channel rule: %v", err)
	}
	if cleared.ChannelID != channel.ID {
		t.Fatalf("expected cleared channel id %d, got %d", channel.ID, cleared.ChannelID)
	}

	_, found, err = store.GetChannelRule(context.Background(), channel.ID)
	if err != nil {
		t.Fatalf("get channel rule after clear: %v", err)
	}
	if found {
		t.Fatal("expected no channel rule after clear")
	}
}

func TestChannelRulesRejectInvalidValues(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "channel-rules-invalid.db")
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
		Type:        model.ChannelTypeDryRun,
		DisplayName: "Dry",
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	invalid := 0
	if _, err := store.UpsertChannelRule(context.Background(), channel.ID, ChannelRuleInput{MaxTextLength: &invalid}); err == nil {
		t.Fatal("expected error for invalid max_text_length")
	}
}

func ptrStringRule(value string) *string {
	copyValue := value
	return &copyValue
}
