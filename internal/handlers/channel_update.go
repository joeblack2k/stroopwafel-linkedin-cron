package handlers

import (
	"context"
	"encoding/json"
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

type ChannelUpdateFormInput struct {
	DisplayName string

	LinkedInAuthorURN  string
	LinkedInAPIBaseURL string

	FacebookPageID     string
	FacebookAPIBaseURL string

	InstagramBusinessID string
	InstagramAPIBaseURL string

	LinkedInAccessTokenAction string
	LinkedInAccessToken       string

	FacebookPageTokenAction string
	FacebookPageToken       string

	InstagramAccessTokenAction string
	InstagramAccessToken       string
}

type ChannelRuleFormInput struct {
	MaxTextLength  string
	MaxHashtags    string
	RequiredPhrase string
}

type ChannelAuditSecretActionView struct {
	Field  string
	Action string
}

type ChannelAuditMetadataView struct {
	Raw             string
	Source          string
	ChangedFields   []string
	SecretActions   []ChannelAuditSecretActionView
	StatusBefore    string
	StatusAfter     string
	ValidationError string
	ParseError      string
	HasDetails      bool
}

type ChannelAuditEventView struct {
	CreatedAt string
	EventType string
	Actor     string
	Summary   string
	Metadata  ChannelAuditMetadataView
}

type ChannelEditPageData struct {
	Title    string
	Channel  ChannelView
	Form     ChannelUpdateFormInput
	RuleForm ChannelRuleFormInput
	Message  string
	Error    string

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

	InstagramBusinessID *string `json:"instagram_business_account_id,omitempty"`
	InstagramAPIBaseURL *string `json:"instagram_api_base_url,omitempty"`

	LinkedInAccessTokenAction string  `json:"linkedin_access_token_action,omitempty"`
	LinkedInAccessToken       *string `json:"linkedin_access_token,omitempty"`

	FacebookPageTokenAction string  `json:"facebook_page_access_token_action,omitempty"`
	FacebookPageToken       *string `json:"facebook_page_access_token,omitempty"`

	InstagramAccessTokenAction string  `json:"instagram_access_token_action,omitempty"`
	InstagramAccessToken       *string `json:"instagram_access_token,omitempty"`
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

func (a *App) UpdateChannelRules(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/channels/"+strconv.FormatInt(id, 10)+"/edit?err="+url.QueryEscape("invalid form body"), http.StatusSeeOther)
		return
	}

	maxTextLength, err := parseOptionalPositiveInt(r.FormValue("max_text_length"))
	if err != nil {
		http.Redirect(w, r, "/settings/channels/"+strconv.FormatInt(id, 10)+"/edit?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	maxHashtags, err := parseOptionalPositiveInt(r.FormValue("max_hashtags"))
	if err != nil {
		http.Redirect(w, r, "/settings/channels/"+strconv.FormatInt(id, 10)+"/edit?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	requiredPhrase := strings.TrimSpace(r.FormValue("required_phrase"))

	_, upsertErr := a.Store.UpsertChannelRule(r.Context(), id, db.ChannelRuleInput{
		MaxTextLength:  maxTextLength,
		MaxHashtags:    maxHashtags,
		RequiredPhrase: stringPointer(requiredPhrase),
	})
	if upsertErr != nil {
		http.Redirect(w, r, "/settings/channels/"+strconv.FormatInt(id, 10)+"/edit?err="+url.QueryEscape(upsertErr.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings/channels/"+strconv.FormatInt(id, 10)+"/edit?msg="+url.QueryEscape("Channel rules saved"), http.StatusSeeOther)
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

		InstagramBusinessID: strings.TrimSpace(r.FormValue("instagram_business_account_id")),
		InstagramAPIBaseURL: strings.TrimSpace(r.FormValue("instagram_api_base_url")),

		LinkedInAccessTokenAction: strings.TrimSpace(r.FormValue("linkedin_access_token_action")),
		LinkedInAccessToken:       strings.TrimSpace(r.FormValue("linkedin_access_token")),

		FacebookPageTokenAction: strings.TrimSpace(r.FormValue("facebook_page_access_token_action")),
		FacebookPageToken:       strings.TrimSpace(r.FormValue("facebook_page_access_token")),

		InstagramAccessTokenAction: strings.TrimSpace(r.FormValue("instagram_access_token_action")),
		InstagramAccessToken:       strings.TrimSpace(r.FormValue("instagram_access_token")),
	}

	linkedInAction, err := db.ParseSecretAction(form.LinkedInAccessTokenAction)
	if err != nil {
		return db.ChannelUpdateInput{}, form, err
	}
	facebookAction, err := db.ParseSecretAction(form.FacebookPageTokenAction)
	if err != nil {
		return db.ChannelUpdateInput{}, form, err
	}
	instagramAction, err := db.ParseSecretAction(form.InstagramAccessTokenAction)
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

		InstagramBusinessAccountID: stringPointer(form.InstagramBusinessID),
		InstagramAPIBaseURL:        stringPointer(form.InstagramAPIBaseURL),

		LinkedInAccessTokenAction: linkedInAction,
		LinkedInAccessToken:       stringPointer(form.LinkedInAccessToken),

		FacebookPageTokenAction: facebookAction,
		FacebookPageToken:       stringPointer(form.FacebookPageToken),

		InstagramAccessTokenAction: instagramAction,
		InstagramAccessToken:       stringPointer(form.InstagramAccessToken),
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
	instagramAction, err := db.ParseSecretAction(payload.InstagramAccessTokenAction)
	if err != nil {
		return db.ChannelUpdateInput{}, err
	}

	return db.ChannelUpdateInput{
		DisplayName: normalizePayloadStringPointer(payload.DisplayName),

		LinkedInAuthorURN:  normalizePayloadStringPointer(payload.LinkedInAuthorURN),
		LinkedInAPIBaseURL: normalizePayloadStringPointer(payload.LinkedInAPIBaseURL),

		FacebookPageID:     normalizePayloadStringPointer(payload.FacebookPageID),
		FacebookAPIBaseURL: normalizePayloadStringPointer(payload.FacebookAPIBaseURL),

		InstagramBusinessAccountID: normalizePayloadStringPointer(payload.InstagramBusinessID),
		InstagramAPIBaseURL:        normalizePayloadStringPointer(payload.InstagramAPIBaseURL),

		LinkedInAccessTokenAction: linkedInAction,
		LinkedInAccessToken:       normalizePayloadStringPointer(payload.LinkedInAccessToken),

		FacebookPageTokenAction: facebookAction,
		FacebookPageToken:       normalizePayloadStringPointer(payload.FacebookPageToken),

		InstagramAccessTokenAction: instagramAction,
		InstagramAccessToken:       normalizePayloadStringPointer(payload.InstagramAccessToken),
	}, nil
}

func channelToUpdateForm(channel model.Channel) ChannelUpdateFormInput {
	return ChannelUpdateFormInput{
		DisplayName: channel.DisplayName,

		LinkedInAuthorURN:  derefString(channel.LinkedInAuthorURN),
		LinkedInAPIBaseURL: derefString(channel.LinkedInAPIBaseURL),

		FacebookPageID:     derefString(channel.FacebookPageID),
		FacebookAPIBaseURL: derefString(channel.FacebookAPIBaseURL),

		InstagramBusinessID: derefString(channel.InstagramBusinessAccountID),
		InstagramAPIBaseURL: derefString(channel.InstagramAPIBaseURL),

		LinkedInAccessTokenAction:  string(db.SecretActionKeep),
		FacebookPageTokenAction:    string(db.SecretActionKeep),
		InstagramAccessTokenAction: string(db.SecretActionKeep),
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

	ruleForm := a.loadChannelRuleForm(r.Context(), channel.ID)

	data := ChannelEditPageData{
		Title:        "Edit Channel",
		Channel:      toChannelView(channel),
		Form:         form,
		RuleForm:     ruleForm,
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
			Metadata:  parseChannelAuditMetadataView(event.Metadata),
		})
	}

	return views, total, nil
}

type channelAuditMetadataPayload struct {
	Source          string            `json:"source"`
	ChangedFields   []string          `json:"changed_fields"`
	StatusBefore    string            `json:"status_before"`
	StatusAfter     string            `json:"status_after"`
	SecretActions   map[string]string `json:"secret_actions"`
	ValidationError string            `json:"validation_error"`
}

func parseChannelAuditMetadataView(metadata *string) ChannelAuditMetadataView {
	raw := strings.TrimSpace(derefString(metadata))
	view := ChannelAuditMetadataView{Raw: raw}
	if raw == "" {
		return view
	}

	var payload channelAuditMetadataPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		view.ParseError = "metadata is not valid JSON"
		view.HasDetails = true
		return view
	}

	view.Source = strings.TrimSpace(payload.Source)
	view.ChangedFields = normalizeAuditChangedFields(payload.ChangedFields)
	view.StatusBefore = strings.TrimSpace(payload.StatusBefore)
	view.StatusAfter = strings.TrimSpace(payload.StatusAfter)
	view.ValidationError = strings.TrimSpace(payload.ValidationError)

	secretActionKeys := make([]string, 0, len(payload.SecretActions))
	for key := range payload.SecretActions {
		secretActionKeys = append(secretActionKeys, key)
	}
	sort.Strings(secretActionKeys)

	view.SecretActions = make([]ChannelAuditSecretActionView, 0, len(secretActionKeys))
	for _, key := range secretActionKeys {
		action := strings.TrimSpace(payload.SecretActions[key])
		if action == "" {
			continue
		}
		view.SecretActions = append(view.SecretActions, ChannelAuditSecretActionView{
			Field:  key,
			Action: action,
		})
	}

	view.HasDetails = view.Source != "" || len(view.ChangedFields) > 0 || len(view.SecretActions) > 0 || view.ValidationError != "" || (view.StatusBefore != "" && view.StatusAfter != "") || view.ParseError != ""
	if !view.HasDetails && view.Raw != "" {
		view.HasDetails = true
	}
	return view
}

func normalizeAuditChangedFields(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	sort.Strings(normalized)
	return normalized
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
	case "basic", "session":
		prefix := authMethodFromContext(ctx)
		username := authUserFromContext(ctx)
		if username == "" {
			username = strings.TrimSpace(a.Config.BasicAuthUser)
		}
		if username == "" {
			return prefix
		}
		return prefix + ":" + username
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

func (a *App) loadChannelRuleForm(ctx context.Context, channelID int64) ChannelRuleFormInput {
	rule, found, err := a.Store.GetChannelRule(ctx, channelID)
	if err != nil || !found {
		return ChannelRuleFormInput{}
	}
	form := ChannelRuleFormInput{RequiredPhrase: derefString(rule.RequiredPhrase)}
	if rule.MaxTextLength != nil {
		form.MaxTextLength = strconv.Itoa(*rule.MaxTextLength)
	}
	if rule.MaxHashtags != nil {
		form.MaxHashtags = strconv.Itoa(*rule.MaxHashtags)
	}
	return form
}

func parseOptionalPositiveInt(raw string) (*int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return nil, fmt.Errorf("value must be a positive integer")
	}
	return &value, nil
}
