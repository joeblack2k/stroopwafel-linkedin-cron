package handlers

import (
	"context"
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

type ChannelUpdateFormInput struct {
	DisplayName string

	LinkedInAuthorURN  string
	LinkedInAPIBaseURL string

	FacebookPageID     string
	FacebookAPIBaseURL string

	LinkedInAccessTokenAction string
	LinkedInAccessToken       string

	FacebookPageTokenAction string
	FacebookPageToken       string
}

type ChannelAuditEventView struct {
	CreatedAt string
	EventType string
	Actor     string
	Summary   string
	Metadata  string
}

type ChannelEditPageData struct {
	Title   string
	Channel ChannelView
	Form    ChannelUpdateFormInput
	Message string
	Error   string

	AuditEvents  []ChannelAuditEventView
	AuditLimit   int
	AuditOffset  int
	AuditTotal   int
	AuditHasPrev bool
	AuditHasNext bool
	AuditPrevURL string
	AuditNextURL string
}

type channelUpdatePayload struct {
	DisplayName *string `json:"display_name,omitempty"`

	LinkedInAuthorURN  *string `json:"linkedin_author_urn,omitempty"`
	LinkedInAPIBaseURL *string `json:"linkedin_api_base_url,omitempty"`

	FacebookPageID     *string `json:"facebook_page_id,omitempty"`
	FacebookAPIBaseURL *string `json:"facebook_api_base_url,omitempty"`

	LinkedInAccessTokenAction string  `json:"linkedin_access_token_action,omitempty"`
	LinkedInAccessToken       *string `json:"linkedin_access_token,omitempty"`

	FacebookPageTokenAction string  `json:"facebook_page_access_token_action,omitempty"`
	FacebookPageToken       *string `json:"facebook_page_access_token,omitempty"`
}

type channelAuditEventResponse struct {
	ID        int64   `json:"id"`
	ChannelID int64   `json:"channel_id"`
	EventType string  `json:"event_type"`
	Actor     string  `json:"actor"`
	Summary   string  `json:"summary"`
	Metadata  *string `json:"metadata,omitempty"`
	CreatedAt string  `json:"created_at"`
}

type channelAuditListResponse struct {
	Items      []channelAuditEventResponse `json:"items"`
	Pagination paginationResponse          `json:"pagination"`
}

func (a *App) EditChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	channel, err := a.Store.GetChannel(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load channel", http.StatusInternalServerError)
		return
	}

	a.renderChannelEditPage(w, r, http.StatusOK, channel, channelToUpdateForm(channel), strings.TrimSpace(r.URL.Query().Get("msg")), strings.TrimSpace(r.URL.Query().Get("err")))
}

func (a *App) UpdateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	existing, err := a.Store.GetChannel(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load channel", http.StatusInternalServerError)
		return
	}

	if err := r.ParseForm(); err != nil {
		a.renderChannelEditPage(w, r, http.StatusBadRequest, existing, channelToUpdateForm(existing), "", "invalid form body")
		return
	}

	input, form, err := parseChannelUpdateForm(r)
	if err != nil {
		a.renderChannelEditPage(w, r, http.StatusBadRequest, existing, form, "", err.Error())
		return
	}
	input.AuditActor = a.channelAuditActor(r.Context())
	input.AuditSource = "ui"

	if _, err := a.Store.UpdateChannel(r.Context(), id, input); err != nil {
		a.renderChannelEditPage(w, r, http.StatusBadRequest, existing, form, "", err.Error())
		return
	}

	http.Redirect(w, r, "/settings/channels?msg="+url.QueryEscape("Channel updated"), http.StatusSeeOther)
}

func (a *App) APIUpdateChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	var payload channelUpdatePayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	input, err := parseChannelUpdatePayload(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.AuditActor = a.channelAuditActor(r.Context())
	input.AuditSource = "api"

	updated, err := a.Store.UpdateChannel(r.Context(), id, input)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapChannelResponse(updated))
}

func (a *App) APIListChannelAuditEvents(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if _, err := a.Store.GetChannel(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load channel")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	if limit > 200 {
		limit = 200
	}
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	events, err := a.Store.ListChannelAuditEvents(r.Context(), id, limit, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list channel audit events")
		return
	}
	total, err := a.Store.CountChannelAuditEvents(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to count channel audit events")
		return
	}

	items := make([]channelAuditEventResponse, 0, len(events))
	for _, event := range events {
		items = append(items, mapChannelAuditEventResponse(event))
	}

	writeJSON(w, http.StatusOK, channelAuditListResponse{
		Items:      items,
		Pagination: buildPagination(limit, offset, total),
	})
}

func mapChannelAuditEventResponse(event model.ChannelAuditEvent) channelAuditEventResponse {
	return channelAuditEventResponse{
		ID:        event.ID,
		ChannelID: event.ChannelID,
		EventType: event.EventType,
		Actor:     event.Actor,
		Summary:   event.Summary,
		Metadata:  event.Metadata,
		CreatedAt: event.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func parseChannelUpdateForm(r *http.Request) (db.ChannelUpdateInput, ChannelUpdateFormInput, error) {
	form := ChannelUpdateFormInput{
		DisplayName: strings.TrimSpace(r.FormValue("display_name")),

		LinkedInAuthorURN:  strings.TrimSpace(r.FormValue("linkedin_author_urn")),
		LinkedInAPIBaseURL: strings.TrimSpace(r.FormValue("linkedin_api_base_url")),

		FacebookPageID:     strings.TrimSpace(r.FormValue("facebook_page_id")),
		FacebookAPIBaseURL: strings.TrimSpace(r.FormValue("facebook_api_base_url")),

		LinkedInAccessTokenAction: strings.TrimSpace(r.FormValue("linkedin_access_token_action")),
		LinkedInAccessToken:       strings.TrimSpace(r.FormValue("linkedin_access_token")),

		FacebookPageTokenAction: strings.TrimSpace(r.FormValue("facebook_page_access_token_action")),
		FacebookPageToken:       strings.TrimSpace(r.FormValue("facebook_page_access_token")),
	}

	linkedInAction, err := db.ParseSecretAction(form.LinkedInAccessTokenAction)
	if err != nil {
		return db.ChannelUpdateInput{}, form, err
	}
	facebookAction, err := db.ParseSecretAction(form.FacebookPageTokenAction)
	if err != nil {
		return db.ChannelUpdateInput{}, form, err
	}

	if form.DisplayName == "" {
		return db.ChannelUpdateInput{}, form, errors.New("display_name is required")
	}

	input := db.ChannelUpdateInput{
		DisplayName: stringPointer(form.DisplayName),

		LinkedInAuthorURN:  stringPointer(form.LinkedInAuthorURN),
		LinkedInAPIBaseURL: stringPointer(form.LinkedInAPIBaseURL),

		FacebookPageID:     stringPointer(form.FacebookPageID),
		FacebookAPIBaseURL: stringPointer(form.FacebookAPIBaseURL),

		LinkedInAccessTokenAction: linkedInAction,
		LinkedInAccessToken:       stringPointer(form.LinkedInAccessToken),

		FacebookPageTokenAction: facebookAction,
		FacebookPageToken:       stringPointer(form.FacebookPageToken),
	}

	return input, form, nil
}

func parseChannelUpdatePayload(payload channelUpdatePayload) (db.ChannelUpdateInput, error) {
	linkedInAction, err := db.ParseSecretAction(payload.LinkedInAccessTokenAction)
	if err != nil {
		return db.ChannelUpdateInput{}, err
	}
	facebookAction, err := db.ParseSecretAction(payload.FacebookPageTokenAction)
	if err != nil {
		return db.ChannelUpdateInput{}, err
	}

	return db.ChannelUpdateInput{
		DisplayName: normalizePayloadStringPointer(payload.DisplayName),

		LinkedInAuthorURN:  normalizePayloadStringPointer(payload.LinkedInAuthorURN),
		LinkedInAPIBaseURL: normalizePayloadStringPointer(payload.LinkedInAPIBaseURL),

		FacebookPageID:     normalizePayloadStringPointer(payload.FacebookPageID),
		FacebookAPIBaseURL: normalizePayloadStringPointer(payload.FacebookAPIBaseURL),

		LinkedInAccessTokenAction: linkedInAction,
		LinkedInAccessToken:       normalizePayloadStringPointer(payload.LinkedInAccessToken),

		FacebookPageTokenAction: facebookAction,
		FacebookPageToken:       normalizePayloadStringPointer(payload.FacebookPageToken),
	}, nil
}

func channelToUpdateForm(channel model.Channel) ChannelUpdateFormInput {
	return ChannelUpdateFormInput{
		DisplayName: channel.DisplayName,

		LinkedInAuthorURN:  derefString(channel.LinkedInAuthorURN),
		LinkedInAPIBaseURL: derefString(channel.LinkedInAPIBaseURL),

		FacebookPageID:     derefString(channel.FacebookPageID),
		FacebookAPIBaseURL: derefString(channel.FacebookAPIBaseURL),

		LinkedInAccessTokenAction: string(db.SecretActionKeep),
		FacebookPageTokenAction:   string(db.SecretActionKeep),
	}
}

func (a *App) renderChannelEditPage(w http.ResponseWriter, r *http.Request, status int, channel model.Channel, form ChannelUpdateFormInput, message, renderErr string) {
	auditLimit := parseLimit(r.URL.Query().Get("audit_limit"), 10)
	if auditLimit > 100 {
		auditLimit = 100
	}
	auditOffset := parseOffset(r.URL.Query().Get("audit_offset"), 0)

	auditEvents, auditTotal, auditErr := a.loadChannelAuditHistory(r.Context(), channel.ID, auditLimit, auditOffset)
	if auditErr != nil {
		if renderErr == "" {
			renderErr = "failed to load channel audit history"
		} else {
			renderErr = renderErr + "; failed to load channel audit history"
		}
	}

	auditHasPrev := auditOffset > 0
	auditHasNext := auditOffset+len(auditEvents) < auditTotal
	prevOffset := auditOffset - auditLimit
	if prevOffset < 0 {
		prevOffset = 0
	}
	nextOffset := auditOffset + auditLimit

	data := ChannelEditPageData{
		Title:        "Edit Channel",
		Channel:      toChannelView(channel),
		Form:         form,
		Message:      message,
		Error:        renderErr,
		AuditEvents:  auditEvents,
		AuditLimit:   auditLimit,
		AuditOffset:  auditOffset,
		AuditTotal:   auditTotal,
		AuditHasPrev: auditHasPrev,
		AuditHasNext: auditHasNext,
	}
	if auditHasPrev {
		data.AuditPrevURL = buildChannelAuditPageURL(channel.ID, auditLimit, prevOffset)
	}
	if auditHasNext {
		data.AuditNextURL = buildChannelAuditPageURL(channel.ID, auditLimit, nextOffset)
	}

	w.WriteHeader(status)
	if err := a.Renderer.Render(w, "channel_edit.html", data); err != nil {
		http.Error(w, "failed to render channel edit", http.StatusInternalServerError)
	}
}

func (a *App) loadChannelAuditHistory(ctx context.Context, channelID int64, limit, offset int) ([]ChannelAuditEventView, int, error) {
	events, err := a.Store.ListChannelAuditEvents(ctx, channelID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := a.Store.CountChannelAuditEvents(ctx, channelID)
	if err != nil {
		return nil, 0, err
	}

	views := make([]ChannelAuditEventView, 0, len(events))
	for _, event := range events {
		views = append(views, ChannelAuditEventView{
			CreatedAt: event.CreatedAt.In(a.Config.Location).Format("2006-01-02 15:04"),
			EventType: event.EventType,
			Actor:     event.Actor,
			Summary:   event.Summary,
			Metadata:  strings.TrimSpace(derefString(event.Metadata)),
		})
	}

	return views, total, nil
}

func buildChannelAuditPageURL(channelID int64, limit, offset int) string {
	values := url.Values{}
	if limit > 0 {
		values.Set("audit_limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		values.Set("audit_offset", strconv.Itoa(offset))
	}

	base := "/settings/channels/" + strconv.FormatInt(channelID, 10) + "/edit"
	query := values.Encode()
	if query == "" {
		return base
	}
	return base + "?" + query
}

func (a *App) channelAuditActor(ctx context.Context) string {
	switch authMethodFromContext(ctx) {
	case "api-key":
		apiKeyName := strings.TrimSpace(apiKeyNameFromContext(ctx))
		apiKeyID := apiKeyIDFromContext(ctx)
		if apiKeyName != "" && apiKeyID > 0 {
			return fmt.Sprintf("api-key:%s#%d", apiKeyName, apiKeyID)
		}
		if apiKeyName != "" {
			return "api-key:" + apiKeyName
		}
		if apiKeyID > 0 {
			return fmt.Sprintf("api-key:%d", apiKeyID)
		}
		return "api-key"
	case "basic":
		username := strings.TrimSpace(a.Config.BasicAuthUser)
		if username == "" {
			return "basic"
		}
		return "basic:" + username
	default:
		return "unknown"
	}
}

func stringPointer(value string) *string {
	copyValue := value
	return &copyValue
}

func normalizePayloadStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}
