package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
)

type PostAttemptView struct {
	AttemptedAt string
	ChannelName string
	ChannelType string
	Status      string
	AttemptNo   int
	Error       string
	RetryAt     string
	ExternalID  string
}

type PostHistoryPageData struct {
	Title             string
	Post              model.Post
	Attempts          []PostAttemptView
	Channels          []model.Channel
	SelectedStatus    string
	SelectedChannelID int64
	Message           string
	Error             string
	ReturnTo          string

	Page          int
	PageSize      int
	TotalAttempts int
	HasPrevPage   bool
	HasNextPage   bool
	PrevPageURL   string
	NextPageURL   string
}

type BulkPostsPageData struct {
	Title    string
	Posts    []model.Post
	Channels []model.Channel
	Location *time.Location
	Message  string
	Error    string
}

func (a *App) PostHistory(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	post, err := a.Store.GetPost(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load post", http.StatusInternalServerError)
		return
	}

	selectedStatus := normalizeAttemptStatus(r.URL.Query().Get("status"))
	var selectedChannelID int64
	var channelFilter *int64
	channelIDRaw := strings.TrimSpace(r.URL.Query().Get("channel_id"))
	if channelIDRaw != "" {
		parsed, parseErr := parseID(channelIDRaw)
		if parseErr == nil {
			selectedChannelID = parsed
			channelFilter = &parsed
		}
	}

	channels, err := a.Store.ListChannelsForPost(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to load channels", http.StatusInternalServerError)
		return
	}

	pageSize := parseLimit(r.URL.Query().Get("page_size"), 50)
	if pageSize > 200 {
		pageSize = 200
	}
	page := parsePage(r.URL.Query().Get("page"), 1)
	offset := (page - 1) * pageSize

	totalAttempts, err := a.Store.CountPublishAttemptsForPost(r.Context(), id, channelFilter, selectedStatus)
	if err != nil {
		http.Error(w, "failed to count attempts", http.StatusInternalServerError)
		return
	}

	attempts, err := a.Store.ListPublishAttemptsForPost(r.Context(), id, channelFilter, selectedStatus, pageSize, offset)
	if err != nil {
		http.Error(w, "failed to load attempts", http.StatusInternalServerError)
		return
	}

	channelMap := make(map[int64]model.Channel, len(channels))
	for _, channel := range channels {
		channelMap[channel.ID] = channel
	}

	views := make([]PostAttemptView, 0, len(attempts))
	for _, attempt := range attempts {
		channel := channelMap[attempt.ChannelID]
		view := PostAttemptView{
			AttemptedAt: attempt.AttemptedAt.In(a.Config.Location).Format("2006-01-02 15:04"),
			ChannelName: channel.DisplayName,
			ChannelType: string(channel.Type),
			Status:      attempt.Status,
			AttemptNo:   attempt.AttemptNo,
			Error:       derefString(attempt.Error),
			RetryAt:     "",
			ExternalID:  derefString(attempt.ExternalID),
		}
		if view.ChannelName == "" {
			view.ChannelName = fmt.Sprintf("Channel #%d", attempt.ChannelID)
		}
		if attempt.RetryAt != nil {
			view.RetryAt = attempt.RetryAt.In(a.Config.Location).Format("2006-01-02 15:04")
		}
		views = append(views, view)
	}

	returnTo := safeReturnPath(r.URL.Query().Get("return_to"))
	if returnTo == "" {
		returnTo = "/calendar"
	}

	hasPrevPage := page > 1
	hasNextPage := offset+len(attempts) < totalAttempts
	prevPage := page - 1
	if prevPage < 1 {
		prevPage = 1
	}
	nextPage := page + 1

	data := PostHistoryPageData{
		Title:             "Post History",
		Post:              post,
		Attempts:          views,
		Channels:          channels,
		SelectedStatus:    selectedStatus,
		SelectedChannelID: selectedChannelID,
		Message:           strings.TrimSpace(r.URL.Query().Get("msg")),
		Error:             strings.TrimSpace(r.URL.Query().Get("err")),
		ReturnTo:          returnTo,
		Page:              page,
		PageSize:          pageSize,
		TotalAttempts:     totalAttempts,
		HasPrevPage:       hasPrevPage,
		HasNextPage:       hasNextPage,
	}
	if hasPrevPage {
		data.PrevPageURL = buildPostHistoryPageURL(id, returnTo, selectedStatus, selectedChannelID, prevPage, pageSize)
	}
	if hasNextPage {
		data.NextPageURL = buildPostHistoryPageURL(id, returnTo, selectedStatus, selectedChannelID, nextPage, pageSize)
	}

	if err := a.Renderer.Render(w, "post_history.html", data); err != nil {
		http.Error(w, "failed to render history", http.StatusInternalServerError)
	}
}

func (a *App) BulkPosts(w http.ResponseWriter, r *http.Request) {
	posts, err := a.Store.ListPosts(r.Context())
	if err != nil {
		http.Error(w, "failed to load posts", http.StatusInternalServerError)
		return
	}
	if len(posts) > 200 {
		posts = posts[:200]
	}

	channels, err := a.Store.ListChannels(r.Context())
	if err != nil {
		http.Error(w, "failed to load channels", http.StatusInternalServerError)
		return
	}

	data := BulkPostsPageData{
		Title:    "Bulk Actions",
		Posts:    posts,
		Channels: channels,
		Location: a.Config.Location,
		Message:  strings.TrimSpace(r.URL.Query().Get("msg")),
		Error:    strings.TrimSpace(r.URL.Query().Get("err")),
	}
	if err := a.Renderer.Render(w, "posts_bulk.html", data); err != nil {
		http.Error(w, "failed to render bulk page", http.StatusInternalServerError)
	}
}

func (a *App) BulkSetPostChannels(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape("invalid form body"), http.StatusSeeOther)
		return
	}

	postIDs := parseChannelIDs(r.Form["post_ids"])
	channelIDs := parseChannelIDs(r.Form["channel_ids"])
	if len(postIDs) == 0 {
		http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape("select at least one post"), http.StatusSeeOther)
		return
	}

	posts, err := a.Store.ListPostsByIDs(r.Context(), postIDs)
	if err != nil {
		http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape("failed to load selected posts"), http.StatusSeeOther)
		return
	}
	if len(posts) != len(postIDs) {
		http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape("one or more posts were not found"), http.StatusSeeOther)
		return
	}

	if len(channelIDs) == 0 {
		for _, post := range posts {
			if post.Status == model.StatusScheduled {
				http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape("scheduled posts must keep at least one channel"), http.StatusSeeOther)
				return
			}
		}
	}

	updated := 0
	failures := make([]string, 0)
	for _, postID := range postIDs {
		if updateErr := a.Store.ReplacePostChannels(r.Context(), postID, channelIDs); updateErr != nil {
			failures = append(failures, fmt.Sprintf("post %d: %v", postID, updateErr))
			continue
		}
		updated++
	}

	if len(failures) > 0 {
		errMessage := fmt.Sprintf("updated %d/%d posts; %s", updated, len(postIDs), strings.Join(failures, "; "))
		http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape(errMessage), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Updated channels for %d posts", updated)
	http.Redirect(w, r, "/posts/bulk?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (a *App) BulkSendNowPosts(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape("invalid form body"), http.StatusSeeOther)
		return
	}

	postIDs := parseChannelIDs(r.Form["post_ids"])
	if len(postIDs) == 0 {
		http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape("select at least one post"), http.StatusSeeOther)
		return
	}

	sent := 0
	failures := make([]string, 0)
	for _, postID := range postIDs {
		if sendErr := a.Scheduler.SendNow(r.Context(), postID); sendErr != nil {
			failures = append(failures, fmt.Sprintf("post %d: %v", postID, sendErr))
			continue
		}
		sent++
	}

	if len(failures) > 0 {
		errMessage := fmt.Sprintf("sent %d/%d posts; %s", sent, len(postIDs), strings.Join(failures, "; "))
		http.Redirect(w, r, "/posts/bulk?err="+url.QueryEscape(errMessage), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Sent %d posts", sent)
	http.Redirect(w, r, "/posts/bulk?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func normalizeAttemptStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case model.PublishAttemptStatusSent, model.PublishAttemptStatusRetry, model.PublishAttemptStatusFailed:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func parsePage(value string, fallback int) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func buildPostHistoryPageURL(postID int64, returnTo, status string, channelID int64, page, pageSize int) string {
	values := url.Values{}
	if returnTo != "" {
		values.Set("return_to", returnTo)
	}
	if status != "" {
		values.Set("status", status)
	}
	if channelID > 0 {
		values.Set("channel_id", strconv.FormatInt(channelID, 10))
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if pageSize > 0 {
		values.Set("page_size", strconv.Itoa(pageSize))
	}

	base := "/posts/" + strconv.FormatInt(postID, 10) + "/history"
	query := values.Encode()
	if query == "" {
		return base
	}
	return base + "?" + query
}
