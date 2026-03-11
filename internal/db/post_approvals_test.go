package db

import (
	"context"
	"testing"
	"time"

	"stroopwafel/internal/model"
)

func TestListPendingApprovalPosts(t *testing.T) {
	t.Parallel()

	store := newPostListTestStore(t)
	ctx := context.Background()

	dueAt := time.Now().UTC().Add(30 * time.Minute)
	pendingPost, err := store.CreatePost(ctx, PostInput{
		Text:               "agent planned post",
		Status:             model.StatusDraft,
		ScheduledAt:        &dueAt,
		ApprovalPending:    true,
		ApprovalPendingSet: true,
	})
	if err != nil {
		t.Fatalf("create pending post: %v", err)
	}

	if _, err := store.CreatePost(ctx, PostInput{Text: "normal post", Status: model.StatusScheduled, ScheduledAt: &dueAt}); err != nil {
		t.Fatalf("create regular post: %v", err)
	}

	pending, err := store.ListPendingApprovalPosts(ctx, 50)
	if err != nil {
		t.Fatalf("list pending approval posts: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending post, got %d", len(pending))
	}
	if pending[0].ID != pendingPost.ID {
		t.Fatalf("expected pending post id %d, got %d", pendingPost.ID, pending[0].ID)
	}
	if !pending[0].ApprovalPending {
		t.Fatal("expected approval_pending=true")
	}
	if pending[0].PlanningApproved {
		t.Fatal("expected planning_approved=false while waiting for approval")
	}
}

func TestAcceptPostPlanning(t *testing.T) {
	t.Parallel()

	store := newPostListTestStore(t)
	ctx := context.Background()

	dueAt := time.Now().UTC().Add(45 * time.Minute)
	created, err := store.CreatePost(ctx, PostInput{
		Text:               "needs approval",
		Status:             model.StatusDraft,
		ScheduledAt:        &dueAt,
		ApprovalPending:    true,
		ApprovalPendingSet: true,
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	approved, err := store.AcceptPostPlanning(ctx, created.ID)
	if err != nil {
		t.Fatalf("accept post planning: %v", err)
	}
	if approved.Status != model.StatusScheduled {
		t.Fatalf("expected status scheduled, got %s", approved.Status)
	}
	if approved.ApprovalPending {
		t.Fatal("expected approval_pending=false after approval")
	}
	if !approved.PlanningApproved {
		t.Fatal("expected planning_approved=true after approval")
	}
}
