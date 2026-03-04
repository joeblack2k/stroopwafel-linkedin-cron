package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stroopwafel/internal/db"
	"stroopwafel/internal/model"
)

type mediaAssetPayload struct {
	MediaURL   string            `json:"media_url"`
	MediaType  string            `json:"media_type"`
	Filename   *string           `json:"filename,omitempty"`
	SizeBytes  int64             `json:"size_bytes,omitempty"`
	StoredPath *string           `json:"stored_path,omitempty"`
	Source     string            `json:"source,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type mediaAssetResponse struct {
	ID         int64             `json:"id"`
	MediaURL   string            `json:"media_url"`
	MediaType  string            `json:"media_type"`
	Filename   *string           `json:"filename,omitempty"`
	SizeBytes  int64             `json:"size_bytes"`
	StoredPath *string           `json:"stored_path,omitempty"`
	Source     string            `json:"source"`
	Tags       []string          `json:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
}

type mediaAssetsListResponse struct {
	Items      []mediaAssetResponse `json:"items"`
	Pagination paginationResponse   `json:"pagination"`
}

type contentTemplatePayload struct {
	Name         string   `json:"name"`
	Description  *string  `json:"description,omitempty"`
	Body         string   `json:"body"`
	ChannelType  *string  `json:"channel_type,omitempty"`
	MediaAssetID *int64   `json:"media_asset_id,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	IsActive     *bool    `json:"is_active,omitempty"`
}

type contentTemplateResponse struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Description  *string  `json:"description,omitempty"`
	Body         string   `json:"body"`
	ChannelType  *string  `json:"channel_type,omitempty"`
	MediaAssetID *int64   `json:"media_asset_id,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	IsActive     bool     `json:"is_active"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type contentTemplatesListResponse struct {
	Items      []contentTemplateResponse `json:"items"`
	Pagination paginationResponse        `json:"pagination"`
}

type channelDeliveryStatResponse struct {
	ChannelID   int64   `json:"channel_id"`
	ChannelType string  `json:"channel_type"`
	DisplayName string  `json:"display_name"`
	SentCount   int     `json:"sent_count"`
	FailedCount int     `json:"failed_count"`
	RetryCount  int     `json:"retry_count"`
	TotalCount  int     `json:"total_count"`
	SuccessRate float64 `json:"success_rate"`
}

type channelDeliveryStatsResponse struct {
	RangeFrom  *string                       `json:"range_from,omitempty"`
	RangeTo    *string                       `json:"range_to,omitempty"`
	Items      []channelDeliveryStatResponse `json:"items"`
	Pagination paginationResponse            `json:"pagination"`
}

func (a *App) APIListMediaAssets(w http.ResponseWriter, r *http.Request) {
	filter := db.MediaAssetFilter{
		Query:     strings.TrimSpace(r.URL.Query().Get("q")),
		Tag:       strings.TrimSpace(r.URL.Query().Get("tag")),
		MediaType: strings.TrimSpace(r.URL.Query().Get("media_type")),
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	total, err := a.Store.CountMediaAssets(r.Context(), filter)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to count media assets")
		return
	}

	items, err := a.Store.ListMediaAssets(r.Context(), filter, limit, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list media assets")
		return
	}

	responses := make([]mediaAssetResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, mapMediaAssetResponse(item))
	}

	writeJSON(w, http.StatusOK, mediaAssetsListResponse{Items: responses, Pagination: buildPagination(limit, offset, total)})
}

func (a *App) APIGetMediaAsset(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid media asset id")
		return
	}

	asset, err := a.Store.GetMediaAsset(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "media asset not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load media asset")
		return
	}

	writeJSON(w, http.StatusOK, mapMediaAssetResponse(asset))
}

func (a *App) APICreateMediaAsset(w http.ResponseWriter, r *http.Request) {
	var payload mediaAssetPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	asset, err := a.Store.CreateMediaAsset(r.Context(), db.MediaAssetInput{
		MediaURL:   payload.MediaURL,
		MediaType:  payload.MediaType,
		Filename:   payload.Filename,
		SizeBytes:  payload.SizeBytes,
		StoredPath: payload.StoredPath,
		Source:     payload.Source,
		Tags:       payload.Tags,
		Metadata:   payload.Metadata,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, mapMediaAssetResponse(asset))
}

func (a *App) APIUpdateMediaAsset(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid media asset id")
		return
	}

	var payload mediaAssetPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	asset, err := a.Store.UpdateMediaAsset(r.Context(), id, db.MediaAssetInput{
		MediaURL:   payload.MediaURL,
		MediaType:  payload.MediaType,
		Filename:   payload.Filename,
		SizeBytes:  payload.SizeBytes,
		StoredPath: payload.StoredPath,
		Source:     payload.Source,
		Tags:       payload.Tags,
		Metadata:   payload.Metadata,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "media asset not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapMediaAssetResponse(asset))
}

func (a *App) APIDeleteMediaAsset(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid media asset id")
		return
	}

	if err := a.Store.DeleteMediaAsset(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "media asset not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to delete media asset")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (a *App) APIListContentTemplates(w http.ResponseWriter, r *http.Request) {
	var activeFilter *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("is_active")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid is_active filter")
			return
		}
		activeFilter = &value
	}

	filter := db.ContentTemplateFilter{
		Query:       strings.TrimSpace(r.URL.Query().Get("q")),
		Tag:         strings.TrimSpace(r.URL.Query().Get("tag")),
		ChannelType: strings.TrimSpace(r.URL.Query().Get("channel_type")),
		IsActive:    activeFilter,
	}
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	total, err := a.Store.CountContentTemplates(r.Context(), filter)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to count content templates")
		return
	}

	items, err := a.Store.ListContentTemplates(r.Context(), filter, limit, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list content templates")
		return
	}

	responses := make([]contentTemplateResponse, 0, len(items))
	for _, item := range items {
		responses = append(responses, mapContentTemplateResponse(item))
	}

	writeJSON(w, http.StatusOK, contentTemplatesListResponse{Items: responses, Pagination: buildPagination(limit, offset, total)})
}

func (a *App) APIGetContentTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid template id")
		return
	}

	item, err := a.Store.GetContentTemplate(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "template not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load template")
		return
	}

	writeJSON(w, http.StatusOK, mapContentTemplateResponse(item))
}

func (a *App) APICreateContentTemplate(w http.ResponseWriter, r *http.Request) {
	var payload contentTemplatePayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	item, err := a.Store.CreateContentTemplate(r.Context(), db.ContentTemplateInput{
		Name:         payload.Name,
		Description:  payload.Description,
		Body:         payload.Body,
		ChannelType:  payload.ChannelType,
		MediaAssetID: payload.MediaAssetID,
		Tags:         payload.Tags,
		IsActive:     payload.IsActive,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, mapContentTemplateResponse(item))
}

func (a *App) APIUpdateContentTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid template id")
		return
	}

	var payload contentTemplatePayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	item, err := a.Store.UpdateContentTemplate(r.Context(), id, db.ContentTemplateInput{
		Name:         payload.Name,
		Description:  payload.Description,
		Body:         payload.Body,
		ChannelType:  payload.ChannelType,
		MediaAssetID: payload.MediaAssetID,
		Tags:         payload.Tags,
		IsActive:     payload.IsActive,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "template not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapContentTemplateResponse(item))
}

func (a *App) APIDeleteContentTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid template id")
		return
	}

	if err := a.Store.DeleteContentTemplate(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "template not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to delete template")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}

func (a *App) APIAnalyticsChannelDelivery(w http.ResponseWriter, r *http.Request) {
	from, err := parseRFC3339(strings.TrimSpace(r.URL.Query().Get("from")))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid from timestamp")
		return
	}
	to, err := parseRFC3339(strings.TrimSpace(r.URL.Query().Get("to")))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid to timestamp")
		return
	}

	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	offset := parseOffset(r.URL.Query().Get("offset"), 0)

	total, err := a.Store.CountChannelDeliveryStats(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to count channel delivery stats")
		return
	}

	stats, err := a.Store.ListChannelDeliveryStats(r.Context(), db.ChannelDeliveryFilter{From: from, To: to}, limit, offset)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to list channel delivery stats")
		return
	}

	items := make([]channelDeliveryStatResponse, 0, len(stats))
	for _, item := range stats {
		items = append(items, channelDeliveryStatResponse{
			ChannelID:   item.ChannelID,
			ChannelType: string(item.ChannelType),
			DisplayName: item.DisplayName,
			SentCount:   item.SentCount,
			FailedCount: item.FailedCount,
			RetryCount:  item.RetryCount,
			TotalCount:  item.TotalCount,
			SuccessRate: item.SuccessRate,
		})
	}

	response := channelDeliveryStatsResponse{Items: items, Pagination: buildPagination(limit, offset, total)}
	if from != nil {
		value := from.UTC().Format(time.RFC3339)
		response.RangeFrom = &value
	}
	if to != nil {
		value := to.UTC().Format(time.RFC3339)
		response.RangeTo = &value
	}

	writeJSON(w, http.StatusOK, response)
}

func mapMediaAssetResponse(item model.MediaAsset) mediaAssetResponse {
	return mediaAssetResponse{
		ID:         item.ID,
		MediaURL:   item.MediaURL,
		MediaType:  item.MediaType,
		Filename:   item.Filename,
		SizeBytes:  item.SizeBytes,
		StoredPath: item.StoredPath,
		Source:     item.Source,
		Tags:       item.Tags,
		Metadata:   item.Metadata,
		CreatedAt:  item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  item.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func mapContentTemplateResponse(item model.ContentTemplate) contentTemplateResponse {
	response := contentTemplateResponse{
		ID:           item.ID,
		Name:         item.Name,
		Description:  item.Description,
		Body:         item.Body,
		MediaAssetID: item.MediaAssetID,
		Tags:         item.Tags,
		IsActive:     item.IsActive,
		CreatedAt:    item.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    item.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if item.ChannelType != nil {
		value := string(*item.ChannelType)
		response.ChannelType = &value
	}
	return response
}
