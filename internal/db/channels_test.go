package db

import (
	"context"
	"path/filepath"
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
