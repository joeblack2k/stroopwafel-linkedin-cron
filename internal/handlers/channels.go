package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"linkedin-cron/internal/config"
	"linkedin-cron/internal/db"
	"linkedin-cron/internal/facebook"
	"linkedin-cron/internal/instagram"
	"linkedin-cron/internal/linkedin"
	"linkedin-cron/internal/model"
)

type ChannelFormInput struct {
	Type                    string
	DisplayName             string
	LinkedInAccessToken     string
	LinkedInAuthorURN       string
	LinkedInAPIBaseURL      string
	FacebookPageAccessToken string
	FacebookPageID          string
	FacebookAPIBaseURL      string
	InstagramAccessToken    string
	InstagramBusinessID     string
	InstagramAPIBaseURL     string
}

type ChannelView struct {
	ID                   int64
	Type                 string
	DisplayName          string
	Status               string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	LastTestAt           *time.Time
	LastError            string
	LinkedInTokenMasked  string
	LinkedInAuthorMasked string
	FacebookTokenMasked  string
	FacebookPageIDMasked string
	InstagramTokenMasked string
	InstagramBizIDMasked string
	LinkedInConfigured   bool
	FacebookConfigured   bool
	InstagramConfigured  bool
	LinkedInAPIBaseURL   string
	FacebookAPIBaseURL   string
	InstagramAPIBaseURL  string
}

type ChannelStats struct {
	Total     int
	Active    int
	Error     int
	Disabled  int
	LinkedIn  int
	Facebook  int
	Instagram int
	DryRun    int
}

type ChannelsPageData struct {
	Title    string
	Stats    ChannelStats
	Channels []ChannelView
	Form     ChannelFormInput
	Message  string
	Error    string
}

type channelPayload struct {
	Type                    string  `json:"type"`
	DisplayName             string  `json:"display_name"`
	LinkedInAccessToken     *string `json:"linkedin_access_token,omitempty"`
	LinkedInAuthorURN       *string `json:"linkedin_author_urn,omitempty"`
	LinkedInAPIBaseURL      *string `json:"linkedin_api_base_url,omitempty"`
	FacebookPageAccessToken *string `json:"facebook_page_access_token,omitempty"`
	FacebookPageID          *string `json:"facebook_page_id,omitempty"`
	FacebookAPIBaseURL      *string `json:"facebook_api_base_url,omitempty"`
	InstagramAccessToken    *string `json:"instagram_access_token,omitempty"`
	InstagramBusinessID     *string `json:"instagram_business_account_id,omitempty"`
	InstagramAPIBaseURL     *string `json:"instagram_api_base_url,omitempty"`
}

type channelSecretPreview struct {
	LinkedInAccessTokenMasked string `json:"linkedin_access_token_masked"`
	LinkedInAuthorURNMasked   string `json:"linkedin_author_urn_masked"`
	FacebookTokenMasked       string `json:"facebook_page_access_token_masked"`
	FacebookPageIDMasked      string `json:"facebook_page_id_masked"`
	InstagramTokenMasked      string `json:"instagram_access_token_masked"`
	InstagramBizIDMasked      string `json:"instagram_business_account_id_masked"`
}

type channelSecretPresence struct {
	LinkedInAccessTokenPresent bool `json:"linkedin_access_token_present"`
	LinkedInAuthorURNPresent   bool `json:"linkedin_author_urn_present"`
	FacebookTokenPresent       bool `json:"facebook_page_access_token_present"`
	FacebookPageIDPresent      bool `json:"facebook_page_id_present"`
	InstagramTokenPresent      bool `json:"instagram_access_token_present"`
	InstagramBizIDPresent      bool `json:"instagram_business_account_id_present"`
}

type channelResponse struct {
	ID                  int64                 `json:"id"`
	Type                string                `json:"type"`
	DisplayName         string                `json:"display_name"`
	Status              string                `json:"status"`
	CreatedAt           string                `json:"created_at"`
	UpdatedAt           string                `json:"updated_at"`
	LastTestAt          *string               `json:"last_test_at,omitempty"`
	LastError           *string               `json:"last_error,omitempty"`
	LinkedInConfigured  bool                  `json:"linkedin_configured"`
	FacebookConfigured  bool                  `json:"facebook_configured"`
	InstagramConfigured bool                  `json:"instagram_configured"`
	SupportsMedia       bool                  `json:"supports_media"`
	MediaTypes          []string              `json:"media_types,omitempty"`
	RequiresMedia       bool                  `json:"requires_media"`
	LinkedInAPIBaseURL  string                `json:"linkedin_api_base_url,omitempty"`
	FacebookAPIBaseURL  string                `json:"facebook_api_base_url,omitempty"`
	InstagramAPIBaseURL string                `json:"instagram_api_base_url,omitempty"`
	SecretPreview       channelSecretPreview  `json:"secret_preview"`
	SecretPresence      channelSecretPresence `json:"secret_presence"`
}

func (a *App) Channels(w http.ResponseWriter, r *http.Request) {
	defaultType := normalizeChannelTypeParam(r.URL.Query().Get("type"))
	a.renderChannelsPage(
		w,
		r,
		http.StatusOK,
		ChannelFormInput{Type: defaultType},
		strings.TrimSpace(r.URL.Query().Get("msg")),
		strings.TrimSpace(r.URL.Query().Get("err")),
	)
}

func (a *App) CreateChannel(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderChannelsPage(w, r, http.StatusBadRequest, ChannelFormInput{}, "", "invalid form body")
		return
	}

	input, form, err := parseChannelForm(r)
	if err != nil {
		a.renderChannelsPage(w, r, http.StatusBadRequest, form, "", err.Error())
		return
	}

	created, err := a.Store.CreateChannel(r.Context(), input)
	if err != nil {
		a.renderChannelsPage(w, r, http.StatusBadRequest, form, "", err.Error())
		return
	}

	a.Logger.Info("channel created", "component", "channels", "channel_id", created.ID, "channel_type", created.Type)
	http.Redirect(w, r, "/settings/channels?msg="+url.QueryEscape("Channel created"), http.StatusSeeOther)
}

func (a *App) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := a.Store.DeleteChannel(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.renderChannelsPage(w, r, http.StatusInternalServerError, ChannelFormInput{}, "", "failed to delete channel")
		return
	}

	http.Redirect(w, r, "/settings/channels?msg="+url.QueryEscape("Channel deleted"), http.StatusSeeOther)
}

func (a *App) TestChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	tested, err := a.runChannelTest(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.renderChannelsPage(w, r, http.StatusInternalServerError, ChannelFormInput{}, "", "failed to test channel")
		return
	}

	msg := fmt.Sprintf("Channel %q test status: %s", tested.DisplayName, tested.Status)
	http.Redirect(w, r, "/settings/channels?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (a *App) runChannelTest(ctx context.Context, id int64) (model.Channel, error) {
	tested, err := a.Store.TestChannel(ctx, id)
	if err != nil {
		return model.Channel{}, err
	}
	if tested.Status != model.ChannelStatusActive {
		return tested, nil
	}

	probeErr := probeChannel(ctx, tested, a.Logger)
	if probeErr != nil {
		message := probeErr.Error()
		return a.Store.SetChannelTestResult(ctx, tested.ID, model.ChannelStatusError, &message)
	}

	return a.Store.SetChannelTestResult(ctx, tested.ID, model.ChannelStatusActive, nil)
}

func probeChannel(ctx context.Context, channel model.Channel, logger *slog.Logger) error {
	switch channel.Type {
	case model.ChannelTypeDryRun:
		return nil
	case model.ChannelTypeLinkedIn:
		baseURL := strings.TrimSpace(derefString(channel.LinkedInAPIBaseURL))
		if baseURL == "" {
			baseURL = "https://api.linkedin.com"
		}
		return linkedin.NewPublisher(
			baseURL,
			derefString(channel.LinkedInAccessToken),
			derefString(channel.LinkedInAuthorURN),
			logger,
		).Probe(ctx)
	case model.ChannelTypeFacebook:
		baseURL := strings.TrimSpace(derefString(channel.FacebookAPIBaseURL))
		if baseURL == "" {
			baseURL = "https://graph.facebook.com/v22.0"
		}
		return facebook.NewPublisher(
			baseURL,
			derefString(channel.FacebookPageAccessToken),
			derefString(channel.FacebookPageID),
			logger,
		).Probe(ctx)
	case model.ChannelTypeInstagram:
		baseURL := strings.TrimSpace(derefString(channel.InstagramAPIBaseURL))
		if baseURL == "" {
			baseURL = "https://graph.facebook.com/v22.0"
		}
		return instagram.NewPublisher(
			baseURL,
			derefString(channel.InstagramAccessToken),
			derefString(channel.InstagramBusinessAccountID),
			logger,
		).Probe(ctx)
	default:
		return fmt.Errorf("unsupported channel type: %s", channel.Type)
	}
}

func (a *App) renderChannelsPage(w http.ResponseWriter, r *http.Request, status int, form ChannelFormInput, message, renderErr string) {
	channels, err := a.Store.ListChannels(r.Context())
	if err != nil {
		http.Error(w, "failed to load channels", http.StatusInternalServerError)
		return
	}

	views := make([]ChannelView, 0, len(channels))
	for _, channel := range channels {
		views = append(views, toChannelView(channel))
	}

	data := ChannelsPageData{
		Title:    "Channels",
		Stats:    buildChannelStats(channels),
		Channels: views,
		Form:     form,
		Message:  message,
		Error:    renderErr,
	}
	w.WriteHeader(status)
	if err := a.Renderer.Render(w, "channels.html", data); err != nil {
		http.Error(w, "failed to render channels", http.StatusInternalServerError)
	}
}

func (a *App) APIListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := a.Store.ListChannels(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	response := make([]channelResponse, 0, len(channels))
	for _, channel := range channels {
		response = append(response, mapChannelResponse(channel))
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *App) APICreateChannel(w http.ResponseWriter, r *http.Request) {
	var payload channelPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	input, err := parseChannelPayload(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, err := a.Store.CreateChannel(r.Context(), input)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, mapChannelResponse(created))
}

func (a *App) APIDeleteChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if err := a.Store.DeleteChannel(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to delete channel")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) APITestChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	tested, err := a.runChannelTest(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to test channel")
		return
	}
	writeJSON(w, http.StatusOK, mapChannelResponse(tested))
}

func normalizeChannelTypeParam(raw string) string {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	switch trimmed {
	case string(model.ChannelTypeLinkedIn), string(model.ChannelTypeFacebook), string(model.ChannelTypeInstagram), string(model.ChannelTypeDryRun):
		return trimmed
	default:
		return string(model.ChannelTypeLinkedIn)
	}
}

func parseChannelForm(r *http.Request) (db.ChannelInput, ChannelFormInput, error) {
	form := ChannelFormInput{
		Type:                    strings.TrimSpace(r.FormValue("type")),
		DisplayName:             strings.TrimSpace(r.FormValue("display_name")),
		LinkedInAccessToken:     strings.TrimSpace(r.FormValue("linkedin_access_token")),
		LinkedInAuthorURN:       strings.TrimSpace(r.FormValue("linkedin_author_urn")),
		LinkedInAPIBaseURL:      strings.TrimSpace(r.FormValue("linkedin_api_base_url")),
		FacebookPageAccessToken: strings.TrimSpace(r.FormValue("facebook_page_access_token")),
		FacebookPageID:          strings.TrimSpace(r.FormValue("facebook_page_id")),
		FacebookAPIBaseURL:      strings.TrimSpace(r.FormValue("facebook_api_base_url")),
		InstagramAccessToken:    strings.TrimSpace(r.FormValue("instagram_access_token")),
		InstagramBusinessID:     strings.TrimSpace(r.FormValue("instagram_business_account_id")),
		InstagramAPIBaseURL:     strings.TrimSpace(r.FormValue("instagram_api_base_url")),
	}

	channelType := model.ChannelType(form.Type)
	input := db.ChannelInput{
		Type:                       channelType,
		DisplayName:                form.DisplayName,
		LinkedInAccessToken:        optionalString(form.LinkedInAccessToken),
		LinkedInAuthorURN:          optionalString(form.LinkedInAuthorURN),
		LinkedInAPIBaseURL:         optionalString(form.LinkedInAPIBaseURL),
		FacebookPageAccessToken:    optionalString(form.FacebookPageAccessToken),
		FacebookPageID:             optionalString(form.FacebookPageID),
		FacebookAPIBaseURL:         optionalString(form.FacebookAPIBaseURL),
		InstagramAccessToken:       optionalString(form.InstagramAccessToken),
		InstagramBusinessAccountID: optionalString(form.InstagramBusinessID),
		InstagramAPIBaseURL:        optionalString(form.InstagramAPIBaseURL),
	}

	if err := model.ValidateChannelInput(channelType, form.DisplayName); err != nil {
		return db.ChannelInput{}, form, err
	}
	return input, form, nil
}

func parseChannelPayload(payload channelPayload) (db.ChannelInput, error) {
	channelType := model.ChannelType(strings.TrimSpace(payload.Type))
	if err := model.ValidateChannelInput(channelType, payload.DisplayName); err != nil {
		return db.ChannelInput{}, err
	}

	return db.ChannelInput{
		Type:                       channelType,
		DisplayName:                strings.TrimSpace(payload.DisplayName),
		LinkedInAccessToken:        trimStringPointer(payload.LinkedInAccessToken),
		LinkedInAuthorURN:          trimStringPointer(payload.LinkedInAuthorURN),
		LinkedInAPIBaseURL:         trimStringPointer(payload.LinkedInAPIBaseURL),
		FacebookPageAccessToken:    trimStringPointer(payload.FacebookPageAccessToken),
		FacebookPageID:             trimStringPointer(payload.FacebookPageID),
		FacebookAPIBaseURL:         trimStringPointer(payload.FacebookAPIBaseURL),
		InstagramAccessToken:       trimStringPointer(payload.InstagramAccessToken),
		InstagramBusinessAccountID: trimStringPointer(payload.InstagramBusinessID),
		InstagramAPIBaseURL:        trimStringPointer(payload.InstagramAPIBaseURL),
	}, nil
}

func mapChannelResponse(channel model.Channel) channelResponse {
	linkedInToken := strings.TrimSpace(derefString(channel.LinkedInAccessToken))
	linkedInAuthor := strings.TrimSpace(derefString(channel.LinkedInAuthorURN))
	facebookToken := strings.TrimSpace(derefString(channel.FacebookPageAccessToken))
	facebookPageID := strings.TrimSpace(derefString(channel.FacebookPageID))
	instagramToken := strings.TrimSpace(derefString(channel.InstagramAccessToken))
	instagramBusinessID := strings.TrimSpace(derefString(channel.InstagramBusinessAccountID))
	capabilities := channel.Capabilities()

	response := channelResponse{
		ID:                  channel.ID,
		Type:                string(channel.Type),
		DisplayName:         channel.DisplayName,
		Status:              channel.Status,
		CreatedAt:           channel.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:           channel.UpdatedAt.UTC().Format(time.RFC3339),
		LastError:           channel.LastError,
		LinkedInConfigured:  linkedInToken != "" && linkedInAuthor != "",
		FacebookConfigured:  facebookToken != "" && facebookPageID != "",
		InstagramConfigured: instagramToken != "" && instagramBusinessID != "",
		SupportsMedia:       capabilities.SupportsMedia,
		MediaTypes:          capabilities.MediaTypes,
		RequiresMedia:       capabilities.RequiresMedia,
		LinkedInAPIBaseURL:  strings.TrimSpace(derefString(channel.LinkedInAPIBaseURL)),
		FacebookAPIBaseURL:  strings.TrimSpace(derefString(channel.FacebookAPIBaseURL)),
		InstagramAPIBaseURL: strings.TrimSpace(derefString(channel.InstagramAPIBaseURL)),
		SecretPreview: channelSecretPreview{
			LinkedInAccessTokenMasked: config.MaskSecret(linkedInToken),
			LinkedInAuthorURNMasked:   config.MaskSecret(linkedInAuthor),
			FacebookTokenMasked:       config.MaskSecret(facebookToken),
			FacebookPageIDMasked:      config.MaskSecret(facebookPageID),
			InstagramTokenMasked:      config.MaskSecret(instagramToken),
			InstagramBizIDMasked:      config.MaskSecret(instagramBusinessID),
		},
		SecretPresence: channelSecretPresence{
			LinkedInAccessTokenPresent: linkedInToken != "",
			LinkedInAuthorURNPresent:   linkedInAuthor != "",
			FacebookTokenPresent:       facebookToken != "",
			FacebookPageIDPresent:      facebookPageID != "",
			InstagramTokenPresent:      instagramToken != "",
			InstagramBizIDPresent:      instagramBusinessID != "",
		},
	}
	if channel.LastTestAt != nil {
		value := channel.LastTestAt.UTC().Format(time.RFC3339)
		response.LastTestAt = &value
	}
	return response
}

func toChannelView(channel model.Channel) ChannelView {
	view := ChannelView{
		ID:                   channel.ID,
		Type:                 string(channel.Type),
		DisplayName:          channel.DisplayName,
		Status:               channel.Status,
		CreatedAt:            channel.CreatedAt,
		UpdatedAt:            channel.UpdatedAt,
		LastTestAt:           channel.LastTestAt,
		LinkedInTokenMasked:  config.MaskSecret(derefString(channel.LinkedInAccessToken)),
		LinkedInAuthorMasked: config.MaskSecret(derefString(channel.LinkedInAuthorURN)),
		FacebookTokenMasked:  config.MaskSecret(derefString(channel.FacebookPageAccessToken)),
		FacebookPageIDMasked: config.MaskSecret(derefString(channel.FacebookPageID)),
		InstagramTokenMasked: config.MaskSecret(derefString(channel.InstagramAccessToken)),
		InstagramBizIDMasked: config.MaskSecret(derefString(channel.InstagramBusinessAccountID)),
		LinkedInConfigured:   strings.TrimSpace(derefString(channel.LinkedInAccessToken)) != "" && strings.TrimSpace(derefString(channel.LinkedInAuthorURN)) != "",
		FacebookConfigured:   strings.TrimSpace(derefString(channel.FacebookPageAccessToken)) != "" && strings.TrimSpace(derefString(channel.FacebookPageID)) != "",
		InstagramConfigured:  strings.TrimSpace(derefString(channel.InstagramAccessToken)) != "" && strings.TrimSpace(derefString(channel.InstagramBusinessAccountID)) != "",
		LinkedInAPIBaseURL:   derefString(channel.LinkedInAPIBaseURL),
		FacebookAPIBaseURL:   derefString(channel.FacebookAPIBaseURL),
		InstagramAPIBaseURL:  derefString(channel.InstagramAPIBaseURL),
	}
	if channel.LastError != nil {
		view.LastError = *channel.LastError
	}
	return view
}

func buildChannelStats(channels []model.Channel) ChannelStats {
	stats := ChannelStats{Total: len(channels)}
	for _, channel := range channels {
		switch channel.Status {
		case model.ChannelStatusActive:
			stats.Active++
		case model.ChannelStatusError:
			stats.Error++
		case model.ChannelStatusDisabled:
			stats.Disabled++
		}

		switch channel.Type {
		case model.ChannelTypeLinkedIn:
			stats.LinkedIn++
		case model.ChannelTypeFacebook:
			stats.Facebook++
		case model.ChannelTypeInstagram:
			stats.Instagram++
		case model.ChannelTypeDryRun:
			stats.DryRun++
		}
	}
	return stats
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func trimStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (a *App) DisableChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	updated, err := a.Store.SetChannelStatus(r.Context(), id, model.ChannelStatusDisabled, nil)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.renderChannelsPage(w, r, http.StatusInternalServerError, ChannelFormInput{}, "", "failed to disable channel")
		return
	}

	msg := fmt.Sprintf("Channel %q disabled", updated.DisplayName)
	http.Redirect(w, r, "/settings/channels?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (a *App) EnableChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if _, err := a.Store.SetChannelStatus(r.Context(), id, model.ChannelStatusActive, nil); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.renderChannelsPage(w, r, http.StatusInternalServerError, ChannelFormInput{}, "", "failed to enable channel")
		return
	}

	tested, err := a.runChannelTest(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		a.renderChannelsPage(w, r, http.StatusInternalServerError, ChannelFormInput{}, "", "failed to re-test channel")
		return
	}

	msg := fmt.Sprintf("Channel %q enabled (status: %s)", tested.DisplayName, tested.Status)
	http.Redirect(w, r, "/settings/channels?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (a *App) APIDisableChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	updated, err := a.Store.SetChannelStatus(r.Context(), id, model.ChannelStatusDisabled, nil)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to disable channel")
		return
	}
	writeJSON(w, http.StatusOK, mapChannelResponse(updated))
}

func (a *App) APIEnableChannel(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if _, err := a.Store.SetChannelStatus(r.Context(), id, model.ChannelStatusActive, nil); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to enable channel")
		return
	}

	tested, err := a.runChannelTest(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to re-test channel")
		return
	}
	writeJSON(w, http.StatusOK, mapChannelResponse(tested))
}
