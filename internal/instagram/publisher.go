package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"linkedin-cron/internal/model"
	"linkedin-cron/internal/publisher"
)

type Publisher struct {
	httpClient *http.Client
	baseURL    string
	token      string
	accountID  string
	logger     *slog.Logger
}

type publishContainerResponse struct {
	ID string `json:"id"`
}

type publishResultResponse struct {
	ID string `json:"id"`
}

type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func NewPublisher(baseURL, token, accountID string, logger *slog.Logger) *Publisher {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &Publisher{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    trimmedBaseURL,
		token:      strings.TrimSpace(token),
		accountID:  strings.TrimSpace(accountID),
		logger:     logger,
	}
}

func (p *Publisher) Mode() string {
	return "instagram"
}

func (p *Publisher) Configured() bool {
	return p.baseURL != "" && p.token != "" && p.accountID != ""
}

func (p *Publisher) Probe(ctx context.Context) error {
	if !p.Configured() {
		return &publisher.PublishError{Err: fmt.Errorf("instagram publisher is not configured"), Retryable: false, Category: publisher.ErrorCategoryAuthExpired}
	}

	query := url.Values{}
	query.Set("fields", "id,username")
	query.Set("access_token", p.token)

	endpoint := fmt.Sprintf("%s/%s?%s", p.baseURL, url.PathEscape(p.accountID), query.Encode())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &publisher.PublishError{Err: fmt.Errorf("build instagram probe request: %w", err), Retryable: true, Category: publisher.ErrorCategoryUpstream}
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		return &publisher.PublishError{Err: fmt.Errorf("instagram probe request failed: %w", err), Retryable: true, Category: publisher.ErrorCategoryUpstream}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
	bodyText := strings.TrimSpace(string(bodyBytes))

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}

	parsed := parseErrorResponse(bodyBytes)
	retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	errMessage := fmt.Sprintf("instagram probe failed (%d)", response.StatusCode)
	if parsed.Message != "" {
		errMessage += ": " + parsed.Message
	} else if bodyText != "" {
		errMessage += ": " + bodyText
	}
	return &publisher.PublishError{Err: errors.New(errMessage), Retryable: retryable, Category: categorizeInstagramStatus(response.StatusCode, parsed)}
}

func (p *Publisher) Publish(ctx context.Context, post model.Post) (publisher.PublishResult, error) {
	if !p.Configured() {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("instagram publisher is not configured"), Retryable: false, Category: publisher.ErrorCategoryAuthExpired}
	}

	if post.MediaURL == nil || strings.TrimSpace(*post.MediaURL) == "" {
		return publisher.PublishResult{}, &publisher.PublishError{Err: errors.New("instagram requires media_url"), Retryable: false, Category: publisher.ErrorCategoryValidation}
	}

	mediaURL := strings.TrimSpace(*post.MediaURL)
	mediaType := strings.ToLower(strings.TrimSpace(derefString(post.MediaType)))
	if mediaType == "" {
		mediaType = inferMediaType(mediaURL)
	}

	containerForm := url.Values{}
	containerForm.Set("caption", post.Text)
	containerForm.Set("access_token", p.token)
	switch mediaType {
	case string(model.MediaTypeVideo):
		containerForm.Set("media_type", "REELS")
		containerForm.Set("video_url", mediaURL)
	case string(model.MediaTypeImage), string(model.MediaTypeLink):
		containerForm.Set("image_url", mediaURL)
	default:
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("instagram does not support media_type=%s", mediaType), Retryable: false, Category: publisher.ErrorCategoryValidation}
	}

	containerEndpoint := fmt.Sprintf("%s/%s/media", p.baseURL, url.PathEscape(p.accountID))
	containerReq, err := http.NewRequestWithContext(ctx, http.MethodPost, containerEndpoint, strings.NewReader(containerForm.Encode()))
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("build instagram container request: %w", err), Retryable: true, Category: publisher.ErrorCategoryUpstream}
	}
	containerReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	containerResp, err := p.httpClient.Do(containerReq)
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("instagram container request failed: %w", err), Retryable: true, Category: publisher.ErrorCategoryUpstream}
	}
	defer containerResp.Body.Close()

	containerBody, _ := io.ReadAll(io.LimitReader(containerResp.Body, 32*1024))
	if containerResp.StatusCode < 200 || containerResp.StatusCode >= 300 {
		parsed := parseErrorResponse(containerBody)
		retryable := containerResp.StatusCode == http.StatusTooManyRequests || containerResp.StatusCode >= 500
		errMessage := fmt.Sprintf("instagram container create failed (%d)", containerResp.StatusCode)
		if parsed.Message != "" {
			errMessage += ": " + parsed.Message
		} else if bodyText := strings.TrimSpace(string(containerBody)); bodyText != "" {
			errMessage += ": " + bodyText
		}
		return publisher.PublishResult{}, &publisher.PublishError{Err: errors.New(errMessage), Retryable: retryable, Category: categorizeInstagramStatus(containerResp.StatusCode, parsed)}
	}

	containerParsed := publishContainerResponse{}
	if err := json.Unmarshal(containerBody, &containerParsed); err != nil || strings.TrimSpace(containerParsed.ID) == "" {
		return publisher.PublishResult{}, &publisher.PublishError{Err: errors.New("instagram container response missing id"), Retryable: true, Category: publisher.ErrorCategoryUpstream}
	}

	publishForm := url.Values{}
	publishForm.Set("creation_id", strings.TrimSpace(containerParsed.ID))
	publishForm.Set("access_token", p.token)

	publishEndpoint := fmt.Sprintf("%s/%s/media_publish", p.baseURL, url.PathEscape(p.accountID))
	publishReq, err := http.NewRequestWithContext(ctx, http.MethodPost, publishEndpoint, strings.NewReader(publishForm.Encode()))
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("build instagram publish request: %w", err), Retryable: true, Category: publisher.ErrorCategoryUpstream}
	}
	publishReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	publishResp, err := p.httpClient.Do(publishReq)
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("instagram publish request failed: %w", err), Retryable: true, Category: publisher.ErrorCategoryUpstream}
	}
	defer publishResp.Body.Close()

	publishBody, _ := io.ReadAll(io.LimitReader(publishResp.Body, 32*1024))
	if publishResp.StatusCode < 200 || publishResp.StatusCode >= 300 {
		parsed := parseErrorResponse(publishBody)
		retryable := publishResp.StatusCode == http.StatusTooManyRequests || publishResp.StatusCode >= 500
		errMessage := fmt.Sprintf("instagram publish failed (%d)", publishResp.StatusCode)
		if parsed.Message != "" {
			errMessage += ": " + parsed.Message
		} else if bodyText := strings.TrimSpace(string(publishBody)); bodyText != "" {
			errMessage += ": " + bodyText
		}
		return publisher.PublishResult{}, &publisher.PublishError{Err: errors.New(errMessage), Retryable: retryable, Category: categorizeInstagramStatus(publishResp.StatusCode, parsed)}
	}

	resultParsed := publishResultResponse{}
	externalID := strings.TrimSpace(containerParsed.ID)
	if err := json.Unmarshal(publishBody, &resultParsed); err == nil {
		if strings.TrimSpace(resultParsed.ID) != "" {
			externalID = strings.TrimSpace(resultParsed.ID)
		}
	}

	permalink := toInstagramPermalink(externalID)
	p.logger.LogAttrs(
		ctx,
		slog.LevelInfo,
		"instagram publish succeeded",
		slog.String("component", "publisher"),
		slog.String("publisher", "instagram"),
		slog.Int64("post_id", post.ID),
		slog.String("external_id", externalID),
		slog.String("permalink", permalink),
	)
	return publisher.PublishResult{ExternalID: externalID, Permalink: permalink, Message: "instagram publish succeeded"}, nil
}

type parsedInstagramError struct {
	Message string
	Type    string
	Code    int
}

func parseErrorResponse(body []byte) parsedInstagramError {
	parsed := errorResponse{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return parsedInstagramError{}
	}
	return parsedInstagramError{
		Message: strings.TrimSpace(parsed.Error.Message),
		Type:    strings.TrimSpace(parsed.Error.Type),
		Code:    parsed.Error.Code,
	}
}

func categorizeInstagramStatus(statusCode int, parsed parsedInstagramError) string {
	if statusCode == http.StatusTooManyRequests {
		return publisher.ErrorCategoryRateLimited
	}
	if statusCode == http.StatusUnauthorized {
		return publisher.ErrorCategoryAuthExpired
	}
	if statusCode == http.StatusForbidden {
		return publisher.ErrorCategoryScopeMissing
	}
	if statusCode >= 500 {
		return publisher.ErrorCategoryUpstream
	}

	messageLower := strings.ToLower(parsed.Message)
	if parsed.Code == 190 || strings.Contains(messageLower, "expired") || strings.Contains(messageLower, "invalid oauth") {
		return publisher.ErrorCategoryAuthExpired
	}
	if strings.Contains(messageLower, "permission") || strings.Contains(messageLower, "scope") {
		return publisher.ErrorCategoryScopeMissing
	}
	if statusCode >= 400 {
		return publisher.ErrorCategoryValidation
	}
	return publisher.ErrorCategoryUnknown
}

func inferMediaType(mediaURL string) string {
	trimmed := strings.ToLower(strings.TrimSpace(mediaURL))
	if queryIndex := strings.IndexAny(trimmed, "?#"); queryIndex >= 0 {
		trimmed = trimmed[:queryIndex]
	}
	switch {
	case strings.HasSuffix(trimmed, ".mp4"), strings.HasSuffix(trimmed, ".mov"), strings.HasSuffix(trimmed, ".webm"):
		return string(model.MediaTypeVideo)
	default:
		return string(model.MediaTypeImage)
	}
}

func toInstagramPermalink(externalID string) string {
	trimmed := strings.TrimSpace(externalID)
	if trimmed == "" {
		return ""
	}
	return "https://www.instagram.com/p/" + trimmed + "/"
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
