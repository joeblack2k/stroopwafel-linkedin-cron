package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"linkedin-cron/internal/db"
	"linkedin-cron/internal/model"
)

type CalendarDay struct {
	Date    time.Time
	InMonth bool
	Posts   []CalendarPostCard
}

type CalendarPostCard struct {
	ID          int64
	Status      string
	ScheduledAt *time.Time
	Label       string
	Tone        string
	Preview     string
	TimeLocal   string
}

type CalendarWeekRow struct {
	HourLabel string
	Hour      int
	Days      []CalendarDay
}

type CalendarPageData struct {
	Title       string
	View        string
	CurrentDate time.Time
	PrevDate    string
	NextDate    string
	Days        []CalendarDay
	WeekDays    []CalendarDay
	WeekRows    []CalendarWeekRow
	ListPosts   []CalendarPostCard
	ReadyDates  []time.Time
	Location    *time.Location
	Message     string
	Error       string
}

type PostFormData struct {
	Title       string
	IsEdit      bool
	PostID      int64
	Form        PostFormInput
	Channels    []model.Channel
	Selected    map[int64]bool
	Message     string
	Error       string
	Location    *time.Location
	ReturnTo    string
	AllowDelete bool
}

type PostFormInput struct {
	ScheduledAt string
	Text        string
	Status      string
	MediaURL    string
	ChannelIDs  []int64
}

type SettingsPageData struct {
	Title         string
	Settings      SettingsStatus
	APIKeys       []model.APIKey
	Message       string
	Error         string
	CreatedAPIKey string
	BotHandoff    string
}

type PostViewData struct {
	Title         string
	Post          model.Post
	Channels      []model.Channel
	Message       string
	Error         string
	Location      *time.Location
	ReturnTo      string
	CreatedAtText string
	UpdatedAtText string
	MediaURLText  string
}

func (a *App) Healthz(w http.ResponseWriter, r *http.Request) {
	a.handleHealth(w, r, "server")
}

func (a *App) Calendar(w http.ResponseWriter, r *http.Request) {
	view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
	if view != "week" && view != "list" {
		view = "month"
	}

	baseDate := a.parseCalendarDate(r.URL.Query().Get("date"))
	location := a.Config.Location
	baseDate = time.Date(baseDate.Year(), baseDate.Month(), baseDate.Day(), 0, 0, 0, 0, location)

	data := CalendarPageData{
		Title:       "Calendar",
		View:        view,
		CurrentDate: baseDate,
		Location:    location,
		Message:     strings.TrimSpace(r.URL.Query().Get("msg")),
		Error:       strings.TrimSpace(r.URL.Query().Get("err")),
	}

	if view == "list" {
		allPosts, err := a.Store.ListPosts(r.Context())
		if err != nil {
			http.Error(w, "failed to load posts", http.StatusInternalServerError)
			return
		}
		cards, err := a.enrichCalendarPostCards(r.Context(), allPosts)
		if err != nil {
			http.Error(w, "failed to load post channels", http.StatusInternalServerError)
			return
		}
		sort.Slice(cards, func(i, j int) bool {
			left := cards[i].ScheduledAt
			right := cards[j].ScheduledAt
			if left == nil && right == nil {
				return cards[i].ID > cards[j].ID
			}
			if left == nil {
				return false
			}
			if right == nil {
				return true
			}
			if left.Equal(*right) {
				return cards[i].ID < cards[j].ID
			}
			return left.Before(*right)
		})
		data.ListPosts = cards
		data.ReadyDates = collectReadyDates(cards, location)
		data.PrevDate = baseDate.AddDate(0, -1, 0).Format("2006-01-02")
		data.NextDate = baseDate.AddDate(0, 1, 0).Format("2006-01-02")

		if err := a.Renderer.Render(w, "calendar.html", data); err != nil {
			http.Error(w, "failed to render calendar", http.StatusInternalServerError)
		}
		return
	}

	var start time.Time
	var end time.Time
	if view == "week" {
		start = beginningOfWeek(baseDate)
		end = start.AddDate(0, 0, 7)
		data.PrevDate = start.AddDate(0, 0, -7).Format("2006-01-02")
		data.NextDate = start.AddDate(0, 0, 7).Format("2006-01-02")
	} else {
		firstOfMonth := time.Date(baseDate.Year(), baseDate.Month(), 1, 0, 0, 0, 0, location)
		start = beginningOfWeek(firstOfMonth)
		end = start.AddDate(0, 0, 42)
		data.PrevDate = firstOfMonth.AddDate(0, -1, 0).Format("2006-01-02")
		data.NextDate = firstOfMonth.AddDate(0, 1, 0).Format("2006-01-02")
	}

	posts, err := a.Store.ListPostsByScheduledRange(r.Context(), start.UTC(), end.UTC())
	if err != nil {
		http.Error(w, "failed to load calendar data", http.StatusInternalServerError)
		return
	}
	cards, err := a.enrichCalendarPostCards(r.Context(), posts)
	if err != nil {
		http.Error(w, "failed to load post channels", http.StatusInternalServerError)
		return
	}

	postsByDate := make(map[string][]CalendarPostCard)
	for _, card := range cards {
		if card.ScheduledAt == nil {
			continue
		}
		key := card.ScheduledAt.In(location).Format("2006-01-02")
		postsByDate[key] = append(postsByDate[key], card)
	}
	for key := range postsByDate {
		sort.Slice(postsByDate[key], func(i, j int) bool {
			left := postsByDate[key][i].ScheduledAt
			right := postsByDate[key][j].ScheduledAt
			if left == nil || right == nil {
				return false
			}
			if left.Equal(*right) {
				return postsByDate[key][i].ID < postsByDate[key][j].ID
			}
			return left.Before(*right)
		})
	}

	if view == "week" {
		weekDays := make([]CalendarDay, 0, 7)
		for i := 0; i < 7; i++ {
			day := start.AddDate(0, 0, i)
			key := day.Format("2006-01-02")
			weekDays = append(weekDays, CalendarDay{Date: day, InMonth: true, Posts: postsByDate[key]})
		}
		data.WeekDays = weekDays
		data.WeekRows = buildWeekRows(weekDays, location)
	} else {
		days := make([]CalendarDay, 0, 42)
		for i := 0; i < 42; i++ {
			day := start.AddDate(0, 0, i)
			key := day.Format("2006-01-02")
			days = append(days, CalendarDay{Date: day, InMonth: day.Month() == baseDate.Month(), Posts: postsByDate[key]})
		}
		data.Days = days
	}

	if err := a.Renderer.Render(w, "calendar.html", data); err != nil {
		http.Error(w, "failed to render calendar", http.StatusInternalServerError)
	}
}

func (a *App) NewPost(w http.ResponseWriter, r *http.Request) {
	channels, err := a.Store.ListChannels(r.Context())
	if err != nil {
		http.Error(w, "failed to load channels", http.StatusInternalServerError)
		return
	}

	data := PostFormData{
		Title:    "New Post",
		IsEdit:   false,
		Location: a.Config.Location,
		Channels: channels,
		Selected: make(map[int64]bool),
		Form: PostFormInput{
			Status: "draft",
		},
		ReturnTo: safeReturnPath(r.URL.Query().Get("return_to")),
	}
	if data.ReturnTo == "" {
		data.ReturnTo = "/calendar"
	}
	if err := a.Renderer.Render(w, "post_form.html", data); err != nil {
		http.Error(w, "failed to render new post page", http.StatusInternalServerError)
	}
}

func (a *App) CreatePost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}

	channels, channelErr := a.Store.ListChannels(r.Context())
	if channelErr != nil {
		http.Error(w, "failed to load channels", http.StatusInternalServerError)
		return
	}

	input, form, err := a.parsePostForm(r)
	if err != nil {
		data := PostFormData{
			Title:    "New Post",
			IsEdit:   false,
			Form:     form,
			Channels: channels,
			Selected: selectedChannelIDMap(form.ChannelIDs),
			Error:    err.Error(),
			Location: a.Config.Location,
			ReturnTo: safeReturnPath(r.FormValue("return_to")),
		}
		if data.ReturnTo == "" {
			data.ReturnTo = "/calendar"
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = a.Renderer.Render(w, "post_form.html", data)
		return
	}

	created, createErr := a.Store.CreatePost(r.Context(), input)
	if createErr != nil {
		http.Error(w, "failed to create post", http.StatusInternalServerError)
		return
	}

	if err := a.Store.ReplacePostChannels(r.Context(), created.ID, form.ChannelIDs); err != nil {
		_ = a.Store.DeletePost(r.Context(), created.ID)
		http.Error(w, "failed to save post channels", http.StatusInternalServerError)
		return
	}

	redirectTo := safeReturnPath(r.FormValue("return_to"))
	if redirectTo == "" {
		redirectTo = "/calendar"
	}
	message := url.QueryEscape(fmt.Sprintf("Post %d created", created.ID))
	http.Redirect(w, r, redirectTo+"?msg="+message, http.StatusSeeOther)
}

func (a *App) ViewPost(w http.ResponseWriter, r *http.Request) {
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

	channels, err := a.Store.ListChannelsForPost(r.Context(), post.ID)
	if err != nil {
		http.Error(w, "failed to load post channels", http.StatusInternalServerError)
		return
	}

	returnTo := safeReturnPath(r.URL.Query().Get("return_to"))
	if returnTo == "" {
		returnTo = "/calendar"
	}

	data := PostViewData{
		Title:         fmt.Sprintf("Post #%d", post.ID),
		Post:          post,
		Channels:      channels,
		Message:       strings.TrimSpace(r.URL.Query().Get("msg")),
		Error:         strings.TrimSpace(r.URL.Query().Get("err")),
		Location:      a.Config.Location,
		ReturnTo:      returnTo,
		CreatedAtText: post.CreatedAt.In(a.Config.Location).Format("2006-01-02 15:04"),
		UpdatedAtText: post.UpdatedAt.In(a.Config.Location).Format("2006-01-02 15:04"),
		MediaURLText:  derefString(post.MediaURL),
	}
	if err := a.Renderer.Render(w, "post_view.html", data); err != nil {
		http.Error(w, "failed to render post view", http.StatusInternalServerError)
	}
}

func (a *App) EditPost(w http.ResponseWriter, r *http.Request) {
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

	channels, err := a.Store.ListChannels(r.Context())
	if err != nil {
		http.Error(w, "failed to load channels", http.StatusInternalServerError)
		return
	}
	selectedIDs, err := a.Store.ListPostChannelIDs(r.Context(), post.ID)
	if err != nil {
		http.Error(w, "failed to load post channels", http.StatusInternalServerError)
		return
	}

	form := PostFormInput{
		Text:     post.Text,
		Status:   string(post.Status),
		MediaURL: derefString(post.MediaURL),
	}
	if post.ScheduledAt != nil {
		form.ScheduledAt = post.ScheduledAt.In(a.Config.Location).Format("2006-01-02T15:04")
	}

	data := PostFormData{
		Title:       fmt.Sprintf("Edit Post #%d", post.ID),
		IsEdit:      true,
		PostID:      post.ID,
		Form:        form,
		Channels:    channels,
		Selected:    selectedChannelIDMap(selectedIDs),
		Message:     strings.TrimSpace(r.URL.Query().Get("msg")),
		Error:       strings.TrimSpace(r.URL.Query().Get("err")),
		Location:    a.Config.Location,
		ReturnTo:    safeReturnPath(r.URL.Query().Get("return_to")),
		AllowDelete: true,
	}
	if data.ReturnTo == "" {
		data.ReturnTo = "/calendar"
	}

	if err := a.Renderer.Render(w, "post_form.html", data); err != nil {
		http.Error(w, "failed to render edit page", http.StatusInternalServerError)
	}
}

func (a *App) UpdatePost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}

	channels, channelErr := a.Store.ListChannels(r.Context())
	if channelErr != nil {
		http.Error(w, "failed to load channels", http.StatusInternalServerError)
		return
	}

	input, form, parseErr := a.parsePostForm(r)
	if parseErr != nil {
		data := PostFormData{
			Title:       fmt.Sprintf("Edit Post #%d", id),
			IsEdit:      true,
			PostID:      id,
			Form:        form,
			Channels:    channels,
			Selected:    selectedChannelIDMap(form.ChannelIDs),
			Error:       parseErr.Error(),
			Location:    a.Config.Location,
			ReturnTo:    safeReturnPath(r.FormValue("return_to")),
			AllowDelete: true,
		}
		if data.ReturnTo == "" {
			data.ReturnTo = "/calendar"
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = a.Renderer.Render(w, "post_form.html", data)
		return
	}

	updated, updateErr := a.Store.UpdatePost(r.Context(), id, input)
	if updateErr != nil {
		if errors.Is(updateErr, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to update post", http.StatusInternalServerError)
		return
	}

	if err := a.Store.ReplacePostChannels(r.Context(), updated.ID, form.ChannelIDs); err != nil {
		http.Error(w, "failed to save post channels", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/posts/%d/edit?msg=%s", updated.ID, url.QueryEscape("Post updated")), http.StatusSeeOther)
}

func (a *App) DeletePost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	err = a.Store.DeletePost(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to delete post", http.StatusInternalServerError)
		return
	}

	redirectTo := safeReturnPath(r.FormValue("return_to"))
	if redirectTo == "" {
		redirectTo = "/calendar"
	}
	http.Redirect(w, r, redirectTo+"?msg="+url.QueryEscape("Post deleted"), http.StatusSeeOther)
}

func (a *App) SendNowPost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	err = a.Scheduler.SendNow(r.Context(), id)
	if err != nil {
		a.Logger.LogAttrs(
			r.Context(),
			slog.LevelWarn,
			"manual send failed",
			slog.String("component", "http"),
			slog.Int64("post_id", id),
			slog.String("error", err.Error()),
		)
		redirect := fmt.Sprintf("/posts/%d/edit?err=%s", id, url.QueryEscape(err.Error()))
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}

	redirectTo := safeReturnPath(r.FormValue("return_to"))
	if redirectTo == "" {
		redirectTo = fmt.Sprintf("/posts/%d/edit", id)
	}
	separator := "?"
	if strings.Contains(redirectTo, "?") {
		separator = "&"
	}
	http.Redirect(w, r, redirectTo+separator+"msg="+url.QueryEscape("Post sent"), http.StatusSeeOther)
}

func (a *App) SendAndDeletePost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	returnTo := safeReturnPath(r.FormValue("return_to"))
	if returnTo == "" {
		returnTo = "/calendar"
	}

	if err := a.Scheduler.SendNow(r.Context(), id); err != nil {
		a.Logger.LogAttrs(
			r.Context(),
			slog.LevelWarn,
			"send-and-delete failed during send",
			slog.String("component", "http"),
			slog.Int64("post_id", id),
			slog.String("error", err.Error()),
		)
		http.Redirect(w, r, withFlash(returnTo, "", "send failed: "+err.Error()), http.StatusSeeOther)
		return
	}

	if err := a.Store.DeletePost(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, withFlash(returnTo, "", "post sent but delete failed"), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, withFlash(returnTo, "Post sent and deleted", ""), http.StatusSeeOther)
}

func (a *App) ReschedulePost(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form body", http.StatusBadRequest)
		return
	}

	returnTo := safeReturnPath(r.FormValue("return_to"))
	if returnTo == "" {
		returnTo = "/calendar"
	}

	scheduledRaw := strings.TrimSpace(r.FormValue("scheduled_at"))
	if scheduledRaw == "" {
		scheduledDate := strings.TrimSpace(r.FormValue("scheduled_date"))
		hour := normalizeHour(r.FormValue("scheduled_hour"))
		minute := strings.TrimSpace(r.FormValue("scheduled_minute"))
		if minute == "" {
			minute = "00"
		}
		if scheduledDate != "" {
			scheduledRaw = fmt.Sprintf("%sT%s:%s", scheduledDate, hour, minute)
		}
	}

	scheduledAt, parseErr := parseDateTimeLocal(scheduledRaw, a.Config.Location)
	if parseErr != nil || scheduledAt == nil {
		http.Redirect(w, r, withFlash(returnTo, "", "invalid scheduled time"), http.StatusSeeOther)
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

	if post.Status == model.StatusSent {
		http.Redirect(w, r, withFlash(returnTo, "", "sent posts cannot be rescheduled"), http.StatusSeeOther)
		return
	}

	status := post.Status
	if status == model.StatusDraft || status == model.StatusFailed {
		status = model.StatusScheduled
	}

	if _, err := a.Store.UpdatePost(r.Context(), post.ID, db.PostInput{
		ScheduledAt: scheduledAt,
		Text:        post.Text,
		Status:      status,
		MediaURL:    post.MediaURL,
	}); err != nil {
		http.Redirect(w, r, withFlash(returnTo, "", "failed to reschedule post"), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, withFlash(returnTo, "Post moved on calendar", ""), http.StatusSeeOther)
}

func (a *App) Settings(w http.ResponseWriter, r *http.Request) {
	a.renderSettingsPage(
		w,
		r,
		http.StatusOK,
		strings.TrimSpace(r.URL.Query().Get("msg")),
		strings.TrimSpace(r.URL.Query().Get("err")),
		"",
		"",
	)
}

func (a *App) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderSettingsPage(w, r, http.StatusBadRequest, "", "invalid form body", "", "")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		a.renderSettingsPage(w, r, http.StatusBadRequest, "", "name is required", "", "")
		return
	}

	created, rawToken, err := a.Store.CreateAPIKey(r.Context(), name)
	if err != nil {
		a.renderSettingsPage(w, r, http.StatusInternalServerError, "", "failed to create api key", "", "")
		return
	}

	a.Logger.LogAttrs(
		r.Context(),
		slog.LevelInfo,
		"api key created",
		slog.String("component", "settings"),
		slog.Int64("api_key_id", created.ID),
		slog.String("api_key_name", created.Name),
	)

	a.renderSettingsPage(
		w,
		r,
		http.StatusOK,
		fmt.Sprintf("API key %q created", created.Name),
		"",
		rawToken,
		"",
	)
}

func (a *App) CreateBotHandoff(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderSettingsPage(w, r, http.StatusBadRequest, "", "invalid form body", "", "")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "bot-" + time.Now().In(a.Config.Location).Format("20060102-150405")
	}

	created, rawToken, err := a.Store.CreateAPIKey(r.Context(), name)
	if err != nil {
		a.renderSettingsPage(w, r, http.StatusInternalServerError, "", "failed to create bot api key", "", "")
		return
	}

	a.Logger.LogAttrs(
		r.Context(),
		slog.LevelInfo,
		"bot handoff created",
		slog.String("component", "settings"),
		slog.Int64("api_key_id", created.ID),
		slog.String("api_key_name", created.Name),
	)

	handoff := a.buildBotHandoff(rawToken)
	a.renderSettingsPage(
		w,
		r,
		http.StatusOK,
		"Bot handoff package generated",
		"",
		rawToken,
		handoff,
	)
}

func (a *App) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := a.Store.RevokeAPIKey(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.renderSettingsPage(w, r, http.StatusInternalServerError, "", "failed to revoke api key", "", "")
		return
	}

	a.Logger.LogAttrs(
		r.Context(),
		slog.LevelInfo,
		"api key revoked",
		slog.String("component", "settings"),
		slog.Int64("api_key_id", id),
	)

	http.Redirect(w, r, "/settings?msg="+url.QueryEscape("API key revoked"), http.StatusSeeOther)
}

func (a *App) renderSettingsPage(w http.ResponseWriter, r *http.Request, status int, message, renderErr, createdAPIKey, botHandoff string) {
	apiKeys, err := a.Store.ListAPIKeys(r.Context())
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}

	data := SettingsPageData{
		Title:         "Settings",
		Settings:      a.settingsStatus(),
		APIKeys:       sanitizeAPIKeys(apiKeys),
		Message:       message,
		Error:         renderErr,
		CreatedAPIKey: createdAPIKey,
		BotHandoff:    botHandoff,
	}
	w.WriteHeader(status)
	if err := a.Renderer.Render(w, "settings.html", data); err != nil {
		http.Error(w, "failed to render settings", http.StatusInternalServerError)
	}
}

func (a *App) parsePostForm(r *http.Request) (db.PostInput, PostFormInput, error) {
	status := model.PostStatus(strings.TrimSpace(r.FormValue("status")))
	scheduledAtRaw := strings.TrimSpace(r.FormValue("scheduled_at"))
	text := strings.TrimSpace(r.FormValue("text"))
	mediaURLRaw := strings.TrimSpace(r.FormValue("media_url"))
	channelIDs := parseChannelIDs(r.Form["channel_ids"])

	parsedScheduledAt, err := parseDateTimeLocal(scheduledAtRaw, a.Config.Location)
	if err != nil {
		return db.PostInput{}, PostFormInput{ScheduledAt: scheduledAtRaw, Text: text, Status: string(status), MediaURL: mediaURLRaw, ChannelIDs: channelIDs}, errors.New("scheduled_at must be a valid datetime")
	}

	var mediaURL *string
	if mediaURLRaw != "" {
		mediaURL = &mediaURLRaw
	}

	if err := model.ValidateEditableInput(text, status, parsedScheduledAt, mediaURL); err != nil {
		return db.PostInput{}, PostFormInput{ScheduledAt: scheduledAtRaw, Text: text, Status: string(status), MediaURL: mediaURLRaw, ChannelIDs: channelIDs}, err
	}

	if status == model.StatusScheduled && len(channelIDs) == 0 {
		return db.PostInput{}, PostFormInput{ScheduledAt: scheduledAtRaw, Text: text, Status: string(status), MediaURL: mediaURLRaw, ChannelIDs: channelIDs}, errors.New("at least one channel is required when status is scheduled")
	}

	return db.PostInput{ScheduledAt: parsedScheduledAt, Text: text, Status: status, MediaURL: mediaURL}, PostFormInput{ScheduledAt: scheduledAtRaw, Text: text, Status: string(status), MediaURL: mediaURLRaw, ChannelIDs: channelIDs}, nil
}

func beginningOfWeek(value time.Time) time.Time {
	return value.AddDate(0, 0, -int(value.Weekday()))
}

func (a *App) parseCalendarDate(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Now().In(a.Config.Location)
	}
	parsed, err := time.ParseInLocation("2006-01-02", trimmed, a.Config.Location)
	if err != nil {
		return time.Now().In(a.Config.Location)
	}
	return parsed
}

func safeReturnPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return ""
	}
	return trimmed
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func parseChannelIDs(values []string) []int64 {
	ids := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		id, err := parseID(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func selectedChannelIDMap(ids []int64) map[int64]bool {
	selected := make(map[int64]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	return selected
}

func (a *App) enrichCalendarPostCards(ctx context.Context, posts []model.Post) ([]CalendarPostCard, error) {
	cards := make([]CalendarPostCard, 0, len(posts))
	for _, post := range posts {
		channels, err := a.Store.ListChannelsForPost(ctx, post.ID)
		if err != nil {
			return nil, fmt.Errorf("list channels for post %d: %w", post.ID, err)
		}

		var scheduledAt *time.Time
		timeLocal := "No time"
		if post.ScheduledAt != nil {
			value := post.ScheduledAt.UTC()
			scheduledAt = &value
			timeLocal = value.In(a.Config.Location).Format("15:04")
		}

		cards = append(cards, CalendarPostCard{
			ID:          post.ID,
			Status:      string(post.Status),
			ScheduledAt: scheduledAt,
			Label:       summarizeCardLabel(channels),
			Tone:        toneForCard(post, channels),
			Preview:     clipText(post.Text, 120),
			TimeLocal:   timeLocal,
		})
	}
	return cards, nil
}

func summarizeCardLabel(channels []model.Channel) string {
	if len(channels) == 0 {
		return "UNASSIGNED POST"
	}

	hasLinkedIn := false
	hasFacebook := false
	hasDryRun := false

	for _, channel := range channels {
		switch channel.Type {
		case model.ChannelTypeLinkedIn:
			hasLinkedIn = true
		case model.ChannelTypeFacebook:
			hasFacebook = true
		case model.ChannelTypeDryRun:
			hasDryRun = true
		}
	}

	switch {
	case hasLinkedIn && hasFacebook:
		return "MULTI CHANNEL POST"
	case hasLinkedIn:
		return "LINKEDIN POST"
	case hasFacebook:
		return "FACEBOOK POST"
	case hasDryRun:
		return "DRY RUN POST"
	default:
		return "SOCIAL POST"
	}
}

func toneForCard(post model.Post, channels []model.Channel) string {
	switch post.Status {
	case model.StatusSent:
		return "tone-sent"
	case model.StatusFailed:
		return "tone-failed"
	case model.StatusDraft:
		return "tone-draft"
	}

	hasLinkedIn := false
	hasFacebook := false

	for _, channel := range channels {
		switch channel.Type {
		case model.ChannelTypeLinkedIn:
			hasLinkedIn = true
		case model.ChannelTypeFacebook:
			hasFacebook = true
		}
	}

	switch {
	case hasLinkedIn && hasFacebook:
		return "tone-multi"
	case hasLinkedIn:
		return "tone-linkedin"
	case hasFacebook:
		return "tone-facebook"
	default:
		return "tone-neutral"
	}
}

func collectReadyDates(cards []CalendarPostCard, location *time.Location) []time.Time {
	if location == nil {
		location = time.UTC
	}
	unique := make(map[string]time.Time)
	for _, card := range cards {
		if card.Status != string(model.StatusScheduled) || card.ScheduledAt == nil {
			continue
		}
		day := card.ScheduledAt.In(location)
		day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location)
		unique[day.Format("2006-01-02")] = day
	}
	readyDates := make([]time.Time, 0, len(unique))
	for _, value := range unique {
		readyDates = append(readyDates, value)
	}
	sort.Slice(readyDates, func(i, j int) bool {
		return readyDates[i].Before(readyDates[j])
	})
	return readyDates
}

func buildWeekRows(weekDays []CalendarDay, location *time.Location) []CalendarWeekRow {
	if location == nil {
		location = time.UTC
	}
	rows := make([]CalendarWeekRow, 0, 24)
	for hour := 0; hour < 24; hour++ {
		row := CalendarWeekRow{
			HourLabel: fmt.Sprintf("%02d:00", hour),
			Hour:      hour,
			Days:      make([]CalendarDay, 0, len(weekDays)),
		}
		for _, day := range weekDays {
			cell := CalendarDay{
				Date:    day.Date,
				InMonth: true,
				Posts:   make([]CalendarPostCard, 0),
			}
			for _, post := range day.Posts {
				if post.ScheduledAt == nil {
					continue
				}
				if post.ScheduledAt.In(location).Hour() != hour {
					continue
				}
				cell.Posts = append(cell.Posts, post)
			}
			sort.Slice(cell.Posts, func(i, j int) bool {
				left := cell.Posts[i].ScheduledAt
				right := cell.Posts[j].ScheduledAt
				if left == nil || right == nil {
					return false
				}
				if left.Equal(*right) {
					return cell.Posts[i].ID < cell.Posts[j].ID
				}
				return left.Before(*right)
			})
			row.Days = append(row.Days, cell)
		}
		rows = append(rows, row)
	}
	return rows
}

func clipText(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	if limit <= 3 {
		return trimmed[:limit]
	}
	return strings.TrimSpace(trimmed[:limit-3]) + "..."
}

func normalizeHour(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "09"
	}
	if len(trimmed) == 1 {
		trimmed = "0" + trimmed
	}
	if len(trimmed) != 2 {
		return "09"
	}
	if trimmed < "00" || trimmed > "23" {
		return "09"
	}
	return trimmed
}

func withFlash(target, message, renderErr string) string {
	target = safeReturnPath(target)
	if target == "" {
		target = "/calendar"
	}

	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	query := parsed.Query()
	if strings.TrimSpace(message) != "" {
		query.Set("msg", strings.TrimSpace(message))
	}
	if strings.TrimSpace(renderErr) != "" {
		query.Set("err", strings.TrimSpace(renderErr))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (a *App) buildBotHandoff(apiKey string) string {
	baseURL := strings.TrimSpace(a.Config.BaseURL)
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	var builder strings.Builder
	builder.WriteString("You are connected to linkedin-cron. Use this API key exactly as shown.\\n\\n")
	builder.WriteString("Base URL: ")
	builder.WriteString(baseURL)
	builder.WriteString("\\n")
	builder.WriteString("API Key: ")
	builder.WriteString(apiKey)
	builder.WriteString("\\n\\n")
	builder.WriteString("Authentication:\\n")
	builder.WriteString("- Preferred header: X-API-Key: ")
	builder.WriteString(apiKey)
	builder.WriteString("\\n")
	builder.WriteString("- Alternative header: Authorization: Bearer ")
	builder.WriteString(apiKey)
	builder.WriteString("\\n\\n")
	builder.WriteString("Suggested workflow:\\n")
	builder.WriteString("1) GET /api/v1/channels\\n")
	builder.WriteString("2) GET /api/v1/posts\\n")
	builder.WriteString("3) POST /api/v1/posts for new scheduled posts\\n")
	builder.WriteString("4) POST /api/v1/posts/{id}/send-now for immediate send\\n\\n")
	builder.WriteString("Example curl:\\n")
	builder.WriteString("curl -H \"X-API-Key: ")
	builder.WriteString(apiKey)
	builder.WriteString("\" ")
	builder.WriteString(baseURL)
	builder.WriteString("/api/v1/posts\\n")

	return builder.String()
}
