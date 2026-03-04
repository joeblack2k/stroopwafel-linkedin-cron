package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stroopwafel/internal/db"
)

const (
	mediaUploadMaxBytes = 100 << 20 // 100MB
)

type mediaUploadResponse struct {
	AssetID    int64  `json:"asset_id"`
	MediaURL   string `json:"media_url"`
	MediaType  string `json:"media_type"`
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	StoredPath string `json:"stored_path"`
}

func (a *App) UploadMediaUI(w http.ResponseWriter, r *http.Request) {
	a.handleMediaUpload(w, r)
}

func (a *App) APIUploadMedia(w http.ResponseWriter, r *http.Request) {
	a.handleMediaUpload(w, r)
}

func (a *App) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, mediaUploadMaxBytes)
	if err := r.ParseMultipartForm(mediaUploadMaxBytes); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid multipart form or file too large")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	uploadDir := filepath.Join(a.Config.DataDir, "uploads")
	if mkdirErr := os.MkdirAll(uploadDir, 0o755); mkdirErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to prepare upload directory")
		return
	}

	headerBuffer := make([]byte, 512)
	readBytes, readErr := file.Read(headerBuffer)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "failed to read upload")
		return
	}
	detectedType := http.DetectContentType(headerBuffer[:readBytes])
	if !isSupportedUploadContentType(detectedType) {
		writeAPIError(w, http.StatusBadRequest, "unsupported file type; allowed: images or videos")
		return
	}

	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to process upload")
		return
	}

	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(header.Filename)))
	if ext == "" {
		ext = extensionFromContentType(detectedType)
	}

	randomToken, tokenErr := randomMediaHex(6)
	if tokenErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to create upload token")
		return
	}

	storedName := fmt.Sprintf("%s_%s%s", time.Now().UTC().Format("20060102T150405"), randomToken, ext)
	storedPath := filepath.Join(uploadDir, storedName)

	destination, createErr := os.Create(storedPath)
	if createErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to store upload")
		return
	}
	defer destination.Close()

	written, copyErr := io.Copy(destination, file)
	if copyErr != nil {
		_ = os.Remove(storedPath)
		writeAPIError(w, http.StatusInternalServerError, "failed to save upload")
		return
	}

	if written <= 0 {
		_ = os.Remove(storedPath)
		writeAPIError(w, http.StatusBadRequest, "uploaded file is empty")
		return
	}

	_ = os.Chmod(storedPath, 0o644)

	mediaPath := "/media/" + storedName
	mediaURL := mediaPath
	baseURL := strings.TrimSpace(a.Config.BaseURL)
	if baseURL != "" {
		mediaURL = strings.TrimRight(baseURL, "/") + mediaPath
	}

	asset, assetErr := a.Store.UpsertMediaAssetByURL(r.Context(), db.MediaAssetInput{
		MediaURL:   mediaURL,
		MediaType:  mediaTypeFromContentType(detectedType),
		Filename:   &header.Filename,
		SizeBytes:  written,
		StoredPath: &mediaPath,
		Source:     "upload",
		Metadata: map[string]string{
			"content_type":      detectedType,
			"original_filename": strings.TrimSpace(header.Filename),
		},
	})
	if assetErr != nil {
		_ = os.Remove(storedPath)
		writeAPIError(w, http.StatusInternalServerError, "failed to register media asset")
		return
	}

	writeJSON(w, http.StatusCreated, mediaUploadResponse{
		AssetID:    asset.ID,
		MediaURL:   mediaURL,
		MediaType:  mediaTypeFromContentType(detectedType),
		Filename:   header.Filename,
		Size:       written,
		StoredPath: mediaPath,
	})
}

func isSupportedUploadContentType(contentType string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(trimmed, "image/") {
		return true
	}
	if strings.HasPrefix(trimmed, "video/") {
		return true
	}
	return false
}

func mediaTypeFromContentType(contentType string) string {
	trimmed := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(trimmed, "video/") {
		return "video"
	}
	if strings.HasPrefix(trimmed, "image/") {
		return "image"
	}
	return "link"
}

func extensionFromContentType(contentType string) string {
	trimmed := strings.ToLower(strings.TrimSpace(contentType))
	if exts, err := mime.ExtensionsByType(trimmed); err == nil && len(exts) > 0 {
		return exts[0]
	}
	switch {
	case strings.HasPrefix(trimmed, "image/"):
		return ".jpg"
	case strings.HasPrefix(trimmed, "video/"):
		return ".mp4"
	default:
		return ".bin"
	}
}

func randomMediaHex(byteCount int) (string, error) {
	if byteCount <= 0 {
		byteCount = 6
	}
	buffer := make([]byte, byteCount)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
