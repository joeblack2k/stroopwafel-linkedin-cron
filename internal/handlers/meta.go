package handlers

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var embeddedOpenAPISpec []byte

type apiErrorCatalogEntry struct {
	Code        string `json:"code"`
	Status      int    `json:"status"`
	Description string `json:"description"`
}

var apiErrorCatalog = []apiErrorCatalogEntry{
	{Code: "auth_unauthorized", Status: http.StatusUnauthorized, Description: "Authentication failed or missing credentials."},
	{Code: "request_invalid_json", Status: http.StatusBadRequest, Description: "JSON body is invalid or malformed."},
	{Code: "request_invalid_post_id", Status: http.StatusBadRequest, Description: "Post id path/query parameter is invalid."},
	{Code: "request_invalid_channel_id", Status: http.StatusBadRequest, Description: "Channel id path/query parameter is invalid."},
	{Code: "request_invalid_status_filter", Status: http.StatusBadRequest, Description: "The status filter query value is invalid."},
	{Code: "request_invalid_type_filter", Status: http.StatusBadRequest, Description: "The type filter query value is invalid."},
	{Code: "post_not_found", Status: http.StatusNotFound, Description: "Requested post does not exist."},
	{Code: "channel_not_found", Status: http.StatusNotFound, Description: "Requested channel does not exist."},
	{Code: "conflict_*", Status: http.StatusConflict, Description: "Conflict while processing request (for example idempotency mismatch)."},
	{Code: "bad_request_*", Status: http.StatusBadRequest, Description: "Validation or request-level problem."},
	{Code: "internal_error_*", Status: http.StatusInternalServerError, Description: "Unexpected server-side failure."},
}

func (a *App) APIErrorCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version": 1,
		"errors":  apiErrorCatalog,
	})
}

func (a *App) APIOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(embeddedOpenAPISpec)
}
