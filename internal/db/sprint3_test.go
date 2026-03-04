package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"stroopwafel/internal/model"
)

func TestMediaAssetCRUDAndFilters(t *testing.T) {
	t.Parallel()

	store := newSprint3Store(t)

	asset, err := store.CreateMediaAsset(context.Background(), MediaAssetInput{
		MediaURL:   "https://cdn.example.com/hero.png",
		MediaType:  "image",
		Filename:   ptrStringSprint3("hero.png"),
		SizeBytes:  1024,
		StoredPath: ptrStringSprint3("/media/hero.png"),
		Tags:       []string{"Launch", "Hero"},
		Metadata: map[string]string{
			"alt": "Hero image",
		},
	})
	if err != nil {
		t.Fatalf("create media asset: %v", err)
	}
	if asset.ID <= 0 {
		t.Fatalf("expected media asset id > 0")
	}
	if len(asset.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(asset.Tags))
	}

	found, err := store.GetMediaAsset(context.Background(), asset.ID)
	if err != nil {
		t.Fatalf("get media asset: %v", err)
	}
	if found.MediaURL != asset.MediaURL {
		t.Fatalf("expected media_url=%q, got %q", asset.MediaURL, found.MediaURL)
	}

	updated, err := store.UpdateMediaAsset(context.Background(), asset.ID, MediaAssetInput{
		Tags: []string{"featured"},
		Metadata: map[string]string{
			"alt": "Featured image",
		},
	})
	if err != nil {
		t.Fatalf("update media asset: %v", err)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "featured" {
		t.Fatalf("expected updated tags to contain featured, got %#v", updated.Tags)
	}

	upserted, err := store.UpsertMediaAssetByURL(context.Background(), MediaAssetInput{
		MediaURL:  asset.MediaURL,
		MediaType: "image",
		Source:    "upload",
		Tags:      []string{"featured", "hero"},
	})
	if err != nil {
		t.Fatalf("upsert media asset: %v", err)
	}
	if upserted.ID != asset.ID {
		t.Fatalf("expected upsert id=%d, got %d", asset.ID, upserted.ID)
	}

	items, err := store.ListMediaAssets(context.Background(), MediaAssetFilter{Tag: "featured"}, 10, 0)
	if err != nil {
		t.Fatalf("list media assets: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 listed asset, got %d", len(items))
	}

	total, err := store.CountMediaAssets(context.Background(), MediaAssetFilter{MediaType: "image"})
	if err != nil {
		t.Fatalf("count media assets: %v", err)
	}
	if total < 1 {
		t.Fatalf("expected at least 1 media asset, got %d", total)
	}

	if err := store.DeleteMediaAsset(context.Background(), asset.ID); err != nil {
		t.Fatalf("delete media asset: %v", err)
	}
	if _, err := store.GetMediaAsset(context.Background(), asset.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestContentTemplateCRUDAndFilters(t *testing.T) {
	t.Parallel()

	store := newSprint3Store(t)

	media, err := store.CreateMediaAsset(context.Background(), MediaAssetInput{
		MediaURL:  "https://cdn.example.com/template.jpg",
		MediaType: "image",
		Tags:      []string{"template"},
	})
	if err != nil {
		t.Fatalf("create media asset: %v", err)
	}

	template, err := store.CreateContentTemplate(context.Background(), ContentTemplateInput{
		Name:         "Product Highlight",
		Description:  ptrStringSprint3("Reusable launch format"),
		Body:         "{{headline}}\n\n{{cta}}",
		ChannelType:  ptrStringSprint3("linkedin"),
		MediaAssetID: ptrInt64Sprint3(media.ID),
		Tags:         []string{"launch", "sales"},
	})
	if err != nil {
		t.Fatalf("create content template: %v", err)
	}
	if template.ID <= 0 {
		t.Fatalf("expected template id > 0")
	}

	active := false
	updated, err := store.UpdateContentTemplate(context.Background(), template.ID, ContentTemplateInput{
		Body:     "{{headline}}\n\n{{value}}\n\n{{cta}}",
		IsActive: &active,
		Tags:     []string{"launch"},
	})
	if err != nil {
		t.Fatalf("update content template: %v", err)
	}
	if updated.IsActive {
		t.Fatalf("expected updated template to be inactive")
	}

	list, err := store.ListContentTemplates(context.Background(), ContentTemplateFilter{
		ChannelType: "linkedin",
		IsActive:    &active,
		Tag:         "launch",
	}, 20, 0)
	if err != nil {
		t.Fatalf("list content templates: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 listed template, got %d", len(list))
	}

	total, err := store.CountContentTemplates(context.Background(), ContentTemplateFilter{IsActive: &active})
	if err != nil {
		t.Fatalf("count content templates: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected count=1, got %d", total)
	}

	if err := store.DeleteContentTemplate(context.Background(), template.ID); err != nil {
		t.Fatalf("delete content template: %v", err)
	}
	if _, err := store.GetContentTemplate(context.Background(), template.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestChannelDeliveryStatsByDateRange(t *testing.T) {
	t.Parallel()

	store := newSprint3Store(t)

	channel, err := store.CreateChannel(context.Background(), ChannelInput{Type: model.ChannelTypeLinkedIn, DisplayName: "LinkedIn"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	otherChannel, err := store.CreateChannel(context.Background(), ChannelInput{Type: model.ChannelTypeFacebook, DisplayName: "Facebook"})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	post, err := store.CreatePost(context.Background(), PostInput{Text: "delivery stat post", Status: model.StatusScheduled, ScheduledAt: ptrTimeSprint3(time.Now().UTC().Add(1 * time.Hour))})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	rangeStart := time.Now().UTC().Add(-1 * time.Hour)
	rangeEnd := time.Now().UTC().Add(1 * time.Hour)

	if _, err := store.InsertPublishAttempt(context.Background(), PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   1,
		AttemptedAt: time.Now().UTC(),
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert sent attempt: %v", err)
	}
	if _, err := store.InsertPublishAttempt(context.Background(), PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   channel.ID,
		AttemptNo:   2,
		AttemptedAt: time.Now().UTC(),
		Status:      model.PublishAttemptStatusFailed,
	}); err != nil {
		t.Fatalf("insert failed attempt: %v", err)
	}
	if _, err := store.InsertPublishAttempt(context.Background(), PublishAttemptInput{
		PostID:      post.ID,
		ChannelID:   otherChannel.ID,
		AttemptNo:   1,
		AttemptedAt: time.Now().UTC().Add(-48 * time.Hour),
		Status:      model.PublishAttemptStatusSent,
	}); err != nil {
		t.Fatalf("insert old attempt: %v", err)
	}

	stats, err := store.ListChannelDeliveryStats(context.Background(), ChannelDeliveryFilter{From: &rangeStart, To: &rangeEnd}, 20, 0)
	if err != nil {
		t.Fatalf("list channel delivery stats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 channel stat rows, got %d", len(stats))
	}

	var linkedInStat ChannelDeliveryStat
	for _, item := range stats {
		if item.ChannelID == channel.ID {
			linkedInStat = item
			break
		}
	}
	if linkedInStat.TotalCount != 2 || linkedInStat.SentCount != 1 || linkedInStat.FailedCount != 1 {
		t.Fatalf("unexpected linkedin stats: %+v", linkedInStat)
	}

	count, err := store.CountChannelDeliveryStats(context.Background())
	if err != nil {
		t.Fatalf("count channel delivery stats: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected channel count=2, got %d", count)
	}
}

func newSprint3Store(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sprint3.db")
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

func ptrStringSprint3(value string) *string {
	copyValue := value
	return &copyValue
}

func ptrInt64Sprint3(value int64) *int64 {
	copyValue := value
	return &copyValue
}

func ptrTimeSprint3(value time.Time) *time.Time {
	copyValue := value.UTC()
	return &copyValue
}
