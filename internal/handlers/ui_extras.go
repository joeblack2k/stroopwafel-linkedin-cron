package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"stroopwafel/internal/db"
	"stroopwafel/internal/model"
)

type PostAttemptView struct {
	AttemptedAt   string
	ChannelName   string
	ChannelType   string
	Status        string
	AttemptNo     int
	Error         string
	ErrorCategory string
	RetryAt       string
	ExternalID    string
	Permalink     string
	ScreenshotURL string
}

type PostHistoryPageData struct {
	Title             string
	Post              model.Post
	Attempts          []PostAttemptView
	Channels          []model.Channel
	SelectedStatus    string
	SelectedChannelID int64
	AttemptedFrom     string
	AttemptedTo       string
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

	SelectedPostIDs     map[int64]bool
	SelectedChannelIDs  map[int64]bool
	HasServerSelections bool
	HasFailurePrefill   bool
	FailedPostCount     int
	LastAction          string
	LastActionLabel     string
	FilterStatus        string
	FilterQuery         string
	TotalPostsCount     int
	VisiblePostsCount   int
}

type bulkRedirectState struct {
	Message            string
	Error              string
	SelectedPostIDs    []int64
	SelectedChannelIDs []int64
	FailedPostIDs      []int64
	LastAction         string
	FilterStatus       string
	FilterQuery        string
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

	attemptedFromInput := strings.TrimSpace(r.URL.Query().Get("attempted_from"))
	attemptedToInput := strings.TrimSpace(r.URL.Query().Get("attempted_to"))
	attemptedFromFilter, attemptedToFilter, rangeErr := parseAttemptedRangeLocal(attemptedFromInput, attemptedToInput, a.Config.Location)
	if attemptedFromFilter != nil {
		attemptedFromInput = attemptedFromFilter.In(a.Config.Location).Format("2006-01-02T15:04")
	}
	if attemptedToFilter != nil {
		attemptedToInput = attemptedToFilter.In(a.Config.Location).Format("2006-01-02T15:04")
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

	if rangeErr != nil {
		attemptedFromFilter = nil
		attemptedToFilter = nil
	}

	totalAttempts, err := a.Store.CountPublishAttemptsForPost(r.Context(), id, channelFilter, selectedStatus, attemptedFromFilter, attemptedToFilter)
	if err != nil {
		http.Error(w, "failed to count attempts", http.StatusInternalServerError)
		return
	}

	attempts, err := a.Store.ListPublishAttemptsForPost(r.Context(), id, channelFilter, selectedStatus, attemptedFromFilter, attemptedToFilter, pageSize, offset)
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
			AttemptedAt:   attempt.AttemptedAt.In(a.Config.Location).Format("2006-01-02 15:04"),
			ChannelName:   channel.DisplayName,
			ChannelType:   string(channel.Type),
			Status:        attempt.Status,
			AttemptNo:     attempt.AttemptNo,
			Error:         derefString(attempt.Error),
			ErrorCategory: derefString(attempt.ErrorCategory),
			RetryAt:       "",
			ExternalID:    derefString(attempt.ExternalID),
			Permalink:     derefString(attempt.Permalink),
			ScreenshotURL: derefString(attempt.ScreenshotURL),
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

	errorMessage := strings.TrimSpace(r.URL.Query().Get("err"))
	if rangeErr != nil {
		if errorMessage == "" {
			errorMessage = rangeErr.Error()
		} else {
			errorMessage = errorMessage + "; " + rangeErr.Error()
		}
	}

	data := PostHistoryPageData{
		Title:             "Post History",
		Post:              post,
		Attempts:          views,
		Channels:          channels,
		SelectedStatus:    selectedStatus,
		SelectedChannelID: selectedChannelID,
		AttemptedFrom:     attemptedFromInput,
		AttemptedTo:       attemptedToInput,
		Message:           strings.TrimSpace(r.URL.Query().Get("msg")),
		Error:             errorMessage,
		ReturnTo:          returnTo,
		Page:              page,
		PageSize:          pageSize,
		TotalAttempts:     totalAttempts,
		HasPrevPage:       hasPrevPage,
		HasNextPage:       hasNextPage,
	}

	attemptedFromForLinks := attemptedFromInput
	attemptedToForLinks := attemptedToInput
	if rangeErr != nil {
		attemptedFromForLinks = ""
		attemptedToForLinks = ""
	}
	if hasPrevPage {
		data.PrevPageURL = buildPostHistoryPageURL(id, returnTo, selectedStatus, selectedChannelID, attemptedFromForLinks, attemptedToForLinks, prevPage, pageSize)
	}
	if hasNextPage {
		data.NextPageURL = buildPostHistoryPageURL(id, returnTo, selectedStatus, selectedChannelID, attemptedFromForLinks, attemptedToForLinks, nextPage, pageSize)
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
	totalPostsCount := len(posts)
	filterStatus := normalizeBulkPostStatus(r.URL.Query().Get("status"))
	filterQuery := strings.TrimSpace(r.URL.Query().Get("q"))
	if filterStatus != "" || filterQuery != "" {
		posts = filterBulkPosts(posts, filterStatus, filterQuery)
	}
	if len(posts) > 200 {
		posts = posts[:200]
	}

	channels, err := a.Store.ListChannels(r.Context())
	if err != nil {
		http.Error(w, "failed to load channels", http.StatusInternalServerError)
		return
	}

	selectedPostIDs := parseCSVIDs(r.URL.Query().Get("selected_post_ids"))
	selectedChannelIDs := parseCSVIDs(r.URL.Query().Get("selected_channel_ids"))
	failedPostIDs := parseCSVIDs(r.URL.Query().Get("failed_post_ids"))
	lastAction := normalizeBulkAction(r.URL.Query().Get("last_action"))
	if lastAction == "" {
		lastAction = "channels"
	}
	if len(selectedPostIDs) == 0 && len(failedPostIDs) > 0 {
		selectedPostIDs = append([]int64(nil), failedPostIDs...)
	}

	data := BulkPostsPageData{
		Title:               "Bulk Actions",
		Posts:               posts,
		Channels:            channels,
		Location:            a.Config.Location,
		Message:             strings.TrimSpace(r.URL.Query().Get("msg")),
		Error:               strings.TrimSpace(r.URL.Query().Get("err")),
		SelectedPostIDs:     selectedChannelIDMap(selectedPostIDs),
		SelectedChannelIDs:  selectedChannelIDMap(selectedChannelIDs),
		HasServerSelections: len(selectedPostIDs) > 0 || len(selectedChannelIDs) > 0,
		HasFailurePrefill:   len(failedPostIDs) > 0,
		FailedPostCount:     len(failedPostIDs),
		LastAction:          lastAction,
		LastActionLabel:     bulkActionLabel(lastAction),
		FilterStatus:        filterStatus,
		FilterQuery:         filterQuery,
		TotalPostsCount:     totalPostsCount,
		VisiblePostsCount:   len(posts),
	}
	if err := a.Renderer.Render(w, "posts_bulk.html", data); err != nil {
		http.Error(w, "failed to render bulk page", http.StatusInternalServerError)
	}
}

func (a *App) BulkSetPostChannels(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, buildBulkRedirectURL(bulkRedirectState{LastAction: "channels", Error: "invalid form body"}), http.StatusSeeOther)
		return
	}

	postIDs := parseChannelIDs(r.Form["post_ids"])
	channelIDs := parseChannelIDs(r.Form["channel_ids"])
	state := bulkRedirectState{
		SelectedPostIDs:    postIDs,
		SelectedChannelIDs: channelIDs,
		LastAction:         "channels",
		FilterStatus:       normalizeBulkPostStatus(r.FormValue("status")),
		FilterQuery:        strings.TrimSpace(r.FormValue("q")),
	}

	if !isBulkActionConfirmed(r) {
		state.Error = "confirm the bulk action first"
		http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
		return
	}
	if len(postIDs) == 0 {
		state.Error = "select at least one post"
		http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
		return
	}

	posts, err := a.Store.ListPostsByIDs(r.Context(), postIDs)
	if err != nil {
		state.Error = "failed to load selected posts"
		http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
		return
	}
	if len(posts) != len(postIDs) {
		state.Error = "one or more posts were not found"
		http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
		return
	}

	if len(channelIDs) == 0 {
		for _, post := range posts {
			if post.Status == model.StatusScheduled {
				state.Error = "scheduled posts must keep at least one channel"
				http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
				return
			}
		}
	}

	updated := 0
	failedPostIDs := make([]int64, 0)
	failures := make([]string, 0)
	for _, postID := range postIDs {
		if updateErr := a.Store.ReplacePostChannels(r.Context(), postID, channelIDs); updateErr != nil {
			failedPostIDs = append(failedPostIDs, postID)
			failures = append(failures, fmt.Sprintf("post %d: %v", postID, updateErr))
			continue
		}
		updated++
	}

	if len(failedPostIDs) > 0 {
		state.Error = fmt.Sprintf("updated %d/%d posts; %d failed; failed posts are preselected for retry (%s)", updated, len(postIDs), len(failedPostIDs), summarizeBulkFailures(failures, 3))
		state.FailedPostIDs = failedPostIDs
		state.SelectedPostIDs = failedPostIDs
		http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
		return
	}

	state.Message = fmt.Sprintf("Updated channels for %d posts", updated)
	http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
}

func (a *App) BulkSendNowPosts(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, buildBulkRedirectURL(bulkRedirectState{LastAction: "send-now", Error: "invalid form body"}), http.StatusSeeOther)
		return
	}

	postIDs := parseChannelIDs(r.Form["post_ids"])
	state := bulkRedirectState{
		SelectedPostIDs: postIDs,
		LastAction:      "send-now",
		FilterStatus:    normalizeBulkPostStatus(r.FormValue("status")),
		FilterQuery:     strings.TrimSpace(r.FormValue("q")),
	}

	if !isBulkActionConfirmed(r) {
		state.Error = "confirm the bulk action first"
		http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
		return
	}
	if len(postIDs) == 0 {
		state.Error = "select at least one post"
		http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
		return
	}

	sent := 0
	failedPostIDs := make([]int64, 0)
	failures := make([]string, 0)
	for _, postID := range postIDs {
		if sendErr := a.Scheduler.SendNow(r.Context(), postID); sendErr != nil {
			failedPostIDs = append(failedPostIDs, postID)
			failures = append(failures, fmt.Sprintf("post %d: %v", postID, sendErr))
			continue
		}
		sent++
	}

	if len(failedPostIDs) > 0 {
		state.Error = fmt.Sprintf("sent %d/%d posts; %d failed; failed posts are preselected for retry (%s)", sent, len(postIDs), len(failedPostIDs), summarizeBulkFailures(failures, 3))
		state.FailedPostIDs = failedPostIDs
		state.SelectedPostIDs = failedPostIDs
		http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
		return
	}

	state.Message = fmt.Sprintf("Sent %d posts", sent)
	http.Redirect(w, r, buildBulkRedirectURL(state), http.StatusSeeOther)
}

func isBulkActionConfirmed(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.FormValue("confirm_bulk")), "yes")
}

func buildBulkRedirectURL(state bulkRedirectState) string {
	values := url.Values{}
	if message := strings.TrimSpace(state.Message); message != "" {
		values.Set("msg", message)
	}
	if errText := strings.TrimSpace(state.Error); errText != "" {
		values.Set("err", errText)
	}
	if encoded := encodeCSVIDs(state.SelectedPostIDs); encoded != "" {
		values.Set("selected_post_ids", encoded)
	}
	if encoded := encodeCSVIDs(state.SelectedChannelIDs); encoded != "" {
		values.Set("selected_channel_ids", encoded)
	}
	if encoded := encodeCSVIDs(state.FailedPostIDs); encoded != "" {
		values.Set("failed_post_ids", encoded)
	}
	if action := normalizeBulkAction(state.LastAction); action != "" {
		values.Set("last_action", action)
	}
	if status := normalizeBulkPostStatus(state.FilterStatus); status != "" {
		values.Set("status", status)
	}
	if query := strings.TrimSpace(state.FilterQuery); query != "" {
		values.Set("q", query)
	}

	path := "/posts/bulk"
	if query := values.Encode(); query != "" {
		return path + "?" + query
	}
	return path
}

func parseCSVIDs(value string) []int64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	return parseChannelIDs(parts)
}

func encodeCSVIDs(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	values := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		values = append(values, id)
	}
	if len(values) == 0 {
		return ""
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	parts := make([]string, len(values))
	for idx, id := range values {
		parts[idx] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func normalizeBulkAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "channels":
		return "channels"
	case "send-now":
		return "send-now"
	default:
		return ""
	}
}

func normalizeBulkPostStatus(value string) string {
	switch model.PostStatus(strings.ToLower(strings.TrimSpace(value))) {
	case model.StatusDraft, model.StatusScheduled, model.StatusFailed, model.StatusSent:
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

func filterBulkPosts(posts []model.Post, statusFilter, query string) []model.Post {
	normalizedStatus := normalizeBulkPostStatus(statusFilter)
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	if normalizedStatus == "" && normalizedQuery == "" {
		return posts
	}

	filtered := make([]model.Post, 0, len(posts))
	for _, post := range posts {
		if normalizedStatus != "" && post.Status != model.PostStatus(normalizedStatus) {
			continue
		}
		if normalizedQuery != "" {
			textMatch := strings.Contains(strings.ToLower(post.Text), normalizedQuery)
			idMatch := strings.Contains(strconv.FormatInt(post.ID, 10), normalizedQuery)
			if !textMatch && !idMatch {
				continue
			}
		}
		filtered = append(filtered, post)
	}
	return filtered
}

func bulkActionLabel(value string) string {
	switch normalizeBulkAction(value) {
	case "send-now":
		return "Send now"
	default:
		return "Apply channels"
	}
}

func summarizeBulkFailures(failures []string, limit int) string {
	if len(failures) == 0 {
		return "no error details"
	}
	if limit <= 0 {
		limit = 1
	}
	if len(failures) <= limit {
		return strings.Join(failures, "; ")
	}
	remaining := len(failures) - limit
	return fmt.Sprintf("%s; +%d more", strings.Join(failures[:limit], "; "), remaining)
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

func buildPostHistoryPageURL(postID int64, returnTo, status string, channelID int64, attemptedFrom, attemptedTo string, page, pageSize int) string {
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
	if strings.TrimSpace(attemptedFrom) != "" {
		values.Set("attempted_from", strings.TrimSpace(attemptedFrom))
	}
	if strings.TrimSpace(attemptedTo) != "" {
		values.Set("attempted_to", strings.TrimSpace(attemptedTo))
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
