package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
)

func TestBulkSetPostChannelsRequiresConfirmation(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	channel := mustCreateDryRunChannel(t, app.Store)
	post, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:   "bulk channels",
		Status: model.StatusDraft,
	})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}

	form := url.Values{}
	form.Add("post_ids", strconv.FormatInt(post.ID, 10))
	form.Add("channel_ids", strconv.FormatInt(channel.ID, 10))
	form.Set("status", "scheduled")
	form.Set("q", "bulk")

	request := httptest.NewRequest(http.MethodPost, "/posts/bulk/channels", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	app.BulkSetPostChannels(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("expected %d, got %d", http.StatusSeeOther, recorder.Code)
	}
	location := parseRedirectLocation(t, recorder)
	if got := location.Query().Get("err"); !strings.Contains(got, "confirm the bulk action first") {
		t.Fatalf("expected confirmation error, got %q", got)
	}
	if got := location.Query().Get("last_action"); got != "channels" {
		t.Fatalf("expected last_action=channels, got %q", got)
	}
	if got := location.Query().Get("selected_post_ids"); got != strconv.FormatInt(post.ID, 10) {
		t.Fatalf("expected selected_post_ids to keep selection, got %q", got)
	}
	if got := location.Query().Get("selected_channel_ids"); got != strconv.FormatInt(channel.ID, 10) {
		t.Fatalf("expected selected_channel_ids to keep selection, got %q", got)
	}
	if got := location.Query().Get("status"); got != "scheduled" {
		t.Fatalf("expected status filter to be preserved, got %q", got)
	}
	if got := location.Query().Get("q"); got != "bulk" {
		t.Fatalf("expected query filter to be preserved, got %q", got)
	}
}

func TestBulkSendNowPostsPreselectsFailuresForRetry(t *testing.T) {
	t.Parallel()

	app := newAPIApp(t)
	successPost, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:   "send me",
		Status: model.StatusDraft,
	})
	if err != nil {
		t.Fatalf("create success post: %v", err)
	}
	failedPost, err := app.Store.CreatePost(context.Background(), db.PostInput{
		Text:   "already sent",
		Status: model.StatusSent,
	})
	if err != nil {
		t.Fatalf("create failed post seed: %v", err)
	}

	form := url.Values{}
	form.Add("post_ids", strconv.FormatInt(successPost.ID, 10))
	form.Add("post_ids", strconv.FormatInt(failedPost.ID, 10))
	form.Set("confirm_bulk", "yes")

	request := httptest.NewRequest(http.MethodPost, "/posts/bulk/send-now", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	app.BulkSendNowPosts(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("expected %d, got %d", http.StatusSeeOther, recorder.Code)
	}
	location := parseRedirectLocation(t, recorder)
	if got := location.Query().Get("last_action"); got != "send-now" {
		t.Fatalf("expected last_action=send-now, got %q", got)
	}
	if got := location.Query().Get("failed_post_ids"); got != strconv.FormatInt(failedPost.ID, 10) {
		t.Fatalf("expected failed post prefill, got %q", got)
	}
	if got := location.Query().Get("selected_post_ids"); got != strconv.FormatInt(failedPost.ID, 10) {
		t.Fatalf("expected selected_post_ids to preselect failed post, got %q", got)
	}
	if got := location.Query().Get("err"); !strings.Contains(got, "failed posts are preselected for retry") {
		t.Fatalf("expected retry helper message, got %q", got)
	}

	updatedSuccess, err := app.Store.GetPost(context.Background(), successPost.ID)
	if err != nil {
		t.Fatalf("reload success post: %v", err)
	}
	if updatedSuccess.Status != model.StatusSent {
		t.Fatalf("expected success post status sent, got %q", updatedSuccess.Status)
	}
}

func parseRedirectLocation(t *testing.T, recorder *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	location := strings.TrimSpace(recorder.Header().Get("Location"))
	if location == "" {
		t.Fatalf("missing redirect location header")
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location %q: %v", location, err)
	}
	return parsed
}
