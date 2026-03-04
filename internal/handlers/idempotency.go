package handlers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"stroopwafel/internal/db"
)

const (
	idempotencyKeyHeader       = "Idempotency-Key"
	idempotentReplayHeader     = "X-Idempotent-Replay"
	maxIdempotencyKeyLength    = 128
	maxIdempotentResponseBytes = 512 * 1024
)

func (a *App) WithAPIIdempotency(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
			next(w, r)
			return
		}

		idempotencyKey := strings.TrimSpace(r.Header.Get(idempotencyKeyHeader))
		if idempotencyKey == "" {
			next(w, r)
			return
		}
		if len(idempotencyKey) > maxIdempotencyKeyLength {
			writeAPIError(w, http.StatusBadRequest, "idempotency key is too long")
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "failed to read request body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		hashSum := sha256.Sum256(bodyBytes)
		requestHash := hex.EncodeToString(hashSum[:])
		authScope := apiIdempotencyScope(r)

		record, created, err := a.Store.ReserveAPIIdempotency(r.Context(), db.APIIdempotencyInput{
			AuthScope:      authScope,
			IdempotencyKey: idempotencyKey,
			Method:         r.Method,
			Path:           r.URL.Path,
			RequestHash:    requestHash,
		})
		if err != nil {
			a.Logger.Error("failed to reserve idempotency key", "component", "api", "path", r.URL.Path, "error", err.Error())
			writeAPIError(w, http.StatusInternalServerError, "failed to process idempotency key")
			return
		}

		if !created {
			if record.Method != r.Method || record.Path != r.URL.Path || record.RequestHash != requestHash {
				writeAPIError(w, http.StatusConflict, "idempotency key was already used with a different request")
				return
			}
			if record.StatusCode == 0 {
				writeAPIError(w, http.StatusConflict, "idempotent request is already in progress")
				return
			}

			w.Header().Set(idempotentReplayHeader, "true")
			if strings.TrimSpace(record.ResponseBody) == "" {
				w.WriteHeader(record.StatusCode)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(record.StatusCode)
			_, _ = w.Write([]byte(record.ResponseBody))
			return
		}

		capture := &idempotencyCaptureWriter{ResponseWriter: w, status: http.StatusOK}
		next(capture, r)

		responseBody := capture.body.String()
		if len(responseBody) > maxIdempotentResponseBytes {
			responseBody = responseBody[:maxIdempotentResponseBytes]
		}

		if err := a.Store.CompleteAPIIdempotency(r.Context(), authScope, idempotencyKey, capture.status, responseBody); err != nil {
			if !errors.Is(err, db.ErrNotFound) {
				a.Logger.Warn("failed to complete idempotency key", "component", "api", "path", r.URL.Path, "error", err.Error())
			}
		}
	}
}

func apiIdempotencyScope(r *http.Request) string {
	if apiKeyID := apiKeyIDFromContext(r.Context()); apiKeyID > 0 {
		return "api-key:" + strconv.FormatInt(apiKeyID, 10)
	}

	if apiKeyName := strings.TrimSpace(apiKeyNameFromContext(r.Context())); apiKeyName != "" {
		return "api-key-name:" + apiKeyName
	}

	if authUser := strings.TrimSpace(authUserFromContext(r.Context())); authUser != "" {
		return "basic:" + authUser
	}

	return "anonymous"
}

type idempotencyCaptureWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *idempotencyCaptureWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *idempotencyCaptureWriter) Write(payload []byte) (int, error) {
	_, _ = w.body.Write(payload)
	return w.ResponseWriter.Write(payload)
}
