package db

import (
	"context"
	"path/filepath"
	"testing"

	"linkedin-cron/internal/model"
)

func TestListChannelsFiltered(t *testing.T) {
	t.Parallel()

	store := newChannelListTestStore(t)
	ctx := context.Background()

	linkedIn, err := store.CreateChannel(ctx, ChannelInput{Type: model.ChannelTypeLinkedIn, DisplayName: "LinkedIn Main", LinkedInAccessToken: ptrChannelListString("token"), LinkedInAuthorURN: ptrChannelListString("urn:li:organization:1")})
	if err != nil {
		t.Fatalf("create linkedin channel: %v", err)
	}
	_, err = store.CreateChannel(ctx, ChannelInput{Type: model.ChannelTypeFacebook, DisplayName: "Facebook Main", FacebookPageAccessToken: ptrChannelListString("token"), FacebookPageID: ptrChannelListString("123")})
	if err != nil {
		t.Fatalf("create facebook channel: %v", err)
	}
	if _, err := store.SetChannelStatus(ctx, linkedIn.ID, model.ChannelStatusDisabled, nil); err != nil {
		t.Fatalf("disable linkedin channel: %v", err)
	}

	filter := ChannelListFilter{Type: model.ChannelTypeLinkedIn, Status: model.ChannelStatusDisabled, SearchQ: "Main"}
	total, err := store.CountChannelsFiltered(ctx, filter)
	if err != nil {
		t.Fatalf("count channels filtered: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total=1, got %d", total)
	}

	items, err := store.ListChannelsFiltered(ctx, filter, 10, 0)
	if err != nil {
		t.Fatalf("list channels filtered: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one filtered channel, got %d", len(items))
	}
	if items[0].ID != linkedIn.ID {
		t.Fatalf("expected linkedIn id=%d, got %d", linkedIn.ID, items[0].ID)
	}
}

func newChannelListTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "channels_list.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if _, err := Migrate(context.Background(), database); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return NewStore(database)
}

func ptrChannelListString(value string) *string {
	trimmed := value
	return &trimmed
}
