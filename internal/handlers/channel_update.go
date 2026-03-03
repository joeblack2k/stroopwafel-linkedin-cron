package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

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

type ChannelEditPageData struct {
	Title   string
	Channel ChannelView
	Form    ChannelUpdateFormInput
	Message string
	Error   string
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

	a.renderChannelEditPage(w, http.StatusOK, channel, channelToUpdateForm(channel), strings.TrimSpace(r.URL.Query().Get("msg")), strings.TrimSpace(r.URL.Query().Get("err")))
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
		a.renderChannelEditPage(w, http.StatusBadRequest, existing, channelToUpdateForm(existing), "", "invalid form body")
		return
	}

	input, form, err := parseChannelUpdateForm(r)
	if err != nil {
		a.renderChannelEditPage(w, http.StatusBadRequest, existing, form, "", err.Error())
		return
	}

	if _, err := a.Store.UpdateChannel(r.Context(), id, input); err != nil {
		a.renderChannelEditPage(w, http.StatusBadRequest, existing, form, "", err.Error())
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

func (a *App) renderChannelEditPage(w http.ResponseWriter, status int, channel model.Channel, form ChannelUpdateFormInput, message, renderErr string) {
	data := ChannelEditPageData{
		Title:   "Edit Channel",
		Channel: toChannelView(channel),
		Form:    form,
		Message: message,
		Error:   renderErr,
	}
	w.WriteHeader(status)
	if err := a.Renderer.Render(w, "channel_edit.html", data); err != nil {
		http.Error(w, "failed to render channel edit", http.StatusInternalServerError)
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
