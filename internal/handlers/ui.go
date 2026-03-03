package handlers

import (
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
	Posts   []model.Post
}

type CalendarPageData struct {
	Title       string
	View        string
	CurrentDate time.Time
	PrevDate    string
	NextDate    string
	Days        []CalendarDay
	WeekDays    []CalendarDay
	Location    *time.Location
	Message     string
	Error       string
}

type PostFormData struct {
	Title       string
	IsEdit      bool
	PostID      int64
	Form        PostFormInput
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
}

type SettingsPageData struct {
	Title         string
	Settings      SettingsStatus
	APIKeys       []model.APIKey
	Message       string
	Error         string
	CreatedAPIKey string
}

func (a *App) Healthz(w http.ResponseWriter, r *http.Request) {
	a.handleHealth(w, r, "server")
}

func (a *App) Calendar(w http.ResponseWriter, r *http.Request) {
	view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
	if view != "week" {
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

	postsByDate := make(map[string][]model.Post)
	for _, post := range posts {
		if post.ScheduledAt == nil {
			continue
		}
		key := post.ScheduledAt.In(location).Format("2006-01-02")
		postsByDate[key] = append(postsByDate[key], post)
	}
	for key := range postsByDate {
		sort.Slice(postsByDate[key], func(i, j int) bool {
			left := postsByDate[key][i].ScheduledAt
			right := postsByDate[key][j].ScheduledAt
			if left == nil || right == nil {
				return false
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
	data := PostFormData{
		Title:    "New Post",
		IsEdit:   false,
		Location: a.Config.Location,
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

	input, form, err := a.parsePostForm(r)
	if err != nil {
		data := PostFormData{
			Title:    "New Post",
			IsEdit:   false,
			Form:     form,
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

	redirectTo := safeReturnPath(r.FormValue("return_to"))
	if redirectTo == "" {
		redirectTo = "/calendar"
	}
	message := url.QueryEscape(fmt.Sprintf("Post %d created", created.ID))
	http.Redirect(w, r, redirectTo+"?msg="+message, http.StatusSeeOther)
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

	input, form, parseErr := a.parsePostForm(r)
	if parseErr != nil {
		data := PostFormData{
			Title:       fmt.Sprintf("Edit Post #%d", id),
			IsEdit:      true,
			PostID:      id,
			Form:        form,
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

func (a *App) Settings(w http.ResponseWriter, r *http.Request) {
	a.renderSettingsPage(
		w,
		r,
		http.StatusOK,
		strings.TrimSpace(r.URL.Query().Get("msg")),
		strings.TrimSpace(r.URL.Query().Get("err")),
		"",
	)
}

func (a *App) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderSettingsPage(w, r, http.StatusBadRequest, "", "invalid form body", "")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		a.renderSettingsPage(w, r, http.StatusBadRequest, "", "name is required", "")
		return
	}

	created, rawToken, err := a.Store.CreateAPIKey(r.Context(), name)
	if err != nil {
		a.renderSettingsPage(w, r, http.StatusInternalServerError, "", "failed to create api key", "")
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
		a.renderSettingsPage(w, r, http.StatusInternalServerError, "", "failed to revoke api key", "")
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

func (a *App) renderSettingsPage(w http.ResponseWriter, r *http.Request, status int, message, renderErr, createdAPIKey string) {
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

	parsedScheduledAt, err := parseDateTimeLocal(scheduledAtRaw, a.Config.Location)
	if err != nil {
		return db.PostInput{}, PostFormInput{ScheduledAt: scheduledAtRaw, Text: text, Status: string(status), MediaURL: mediaURLRaw}, errors.New("scheduled_at must be a valid datetime")
	}

	var mediaURL *string
	if mediaURLRaw != "" {
		mediaURL = &mediaURLRaw
	}

	if err := model.ValidateEditableInput(text, status, parsedScheduledAt, mediaURL); err != nil {
		return db.PostInput{}, PostFormInput{ScheduledAt: scheduledAtRaw, Text: text, Status: string(status), MediaURL: mediaURLRaw}, err
	}

	return db.PostInput{ScheduledAt: parsedScheduledAt, Text: text, Status: status, MediaURL: mediaURL}, PostFormInput{ScheduledAt: scheduledAtRaw, Text: text, Status: string(status), MediaURL: mediaURLRaw}, nil
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
