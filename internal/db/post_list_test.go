package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stroopwafel/internal/model"
)

func TestListPostsFilteredByStatusAndQuery(t *testing.T) {
	t.Parallel()

	store := newPostListTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	postA, err := store.CreatePost(ctx, PostInput{Text: "alpha scheduled", Status: model.StatusScheduled, ScheduledAt: ptrPostListTime(now.Add(5 * time.Minute))})
	if err != nil {
		t.Fatalf("create post A: %v", err)
	}
	postB, err := store.CreatePost(ctx, PostInput{Text: "alpha draft", Status: model.StatusDraft})
	if err != nil {
		t.Fatalf("create post B: %v", err)
	}
	postC, err := store.CreatePost(ctx, PostInput{Text: "beta scheduled", Status: model.StatusScheduled, ScheduledAt: ptrPostListTime(now.Add(10 * time.Minute))})
	if err != nil {
		t.Fatalf("create post C: %v", err)
	}
	_ = postB

	channel, err := store.CreateChannel(ctx, ChannelInput{Type: model.ChannelTypeDryRun, DisplayName: "Dry"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := store.ReplacePostChannels(ctx, postA.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("assign channel A: %v", err)
	}
	if err := store.ReplacePostChannels(ctx, postC.ID, []int64{channel.ID}); err != nil {
		t.Fatalf("assign channel C: %v", err)
	}

	filter := PostListFilter{Status: model.StatusScheduled, SearchQuery: "alpha", ChannelID: &channel.ID}
	total, err := store.CountPostsFiltered(ctx, filter)
	if err != nil {
		t.Fatalf("count filtered posts: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected filtered total=1, got %d", total)
	}

	items, err := store.ListPostsFiltered(ctx, filter, 10, 0)
	if err != nil {
		t.Fatalf("list filtered posts: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 filtered post, got %d", len(items))
	}
	if items[0].ID != postA.ID {
		t.Fatalf("expected postA id=%d, got %d", postA.ID, items[0].ID)
	}
}

func newPostListTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "post_list.db")
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

func ptrPostListTime(value time.Time) *time.Time {
	v := value.UTC()
	return &v
}
