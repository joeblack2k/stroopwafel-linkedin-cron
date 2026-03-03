package facebook

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
	pageToken  string
	pageID     string
	logger     *slog.Logger
}

type publishResponse struct {
	ID string `json:"id"`
}

type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func NewPublisher(baseURL, pageToken, pageID string, logger *slog.Logger) *Publisher {
	trimmedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return &Publisher{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		baseURL:    trimmedBaseURL,
		pageToken:  strings.TrimSpace(pageToken),
		pageID:     strings.TrimSpace(pageID),
		logger:     logger,
	}
}

func (p *Publisher) Mode() string {
	return "facebook-page"
}

func (p *Publisher) Configured() bool {
	return p.baseURL != "" && p.pageToken != "" && p.pageID != ""
}

func (p *Publisher) Probe(ctx context.Context) error {
	if !p.Configured() {
		return &publisher.PublishError{Err: fmt.Errorf("facebook page publisher is not configured"), Retryable: false}
	}

	query := url.Values{}
	query.Set("fields", "id,name")
	query.Set("access_token", p.pageToken)

	endpoint := fmt.Sprintf("%s/%s?%s", p.baseURL, url.PathEscape(p.pageID), query.Encode())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &publisher.PublishError{Err: fmt.Errorf("build facebook probe request: %w", err), Retryable: true}
	}

	response, err := p.httpClient.Do(request)
	if err != nil {
		return &publisher.PublishError{Err: fmt.Errorf("facebook probe request failed: %w", err), Retryable: true}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
	bodyText := strings.TrimSpace(string(bodyBytes))

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}

	retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	errMessage := fmt.Sprintf("facebook probe failed (%d)", response.StatusCode)
	if parsed := parseErrorMessage(bodyBytes); parsed != "" {
		errMessage += ": " + parsed
	} else if bodyText != "" {
		errMessage += ": " + bodyText
	}
	return &publisher.PublishError{Err: errors.New(errMessage), Retryable: retryable}
}

func (p *Publisher) Publish(ctx context.Context, post model.Post) (publisher.PublishResult, error) {
	if !p.Configured() {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("facebook page publisher is not configured"), Retryable: false}
	}

	form := url.Values{}
	form.Set("message", post.Text)
	if post.MediaURL != nil {
		mediaURL := strings.TrimSpace(*post.MediaURL)
		if mediaURL != "" {
			form.Set("link", mediaURL)
		}
	}
	form.Set("access_token", p.pageToken)

	endpoint := fmt.Sprintf("%s/%s/feed", p.baseURL, url.PathEscape(p.pageID))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("build facebook request: %w", err), Retryable: true}
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := p.httpClient.Do(request)
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("facebook request failed: %w", err), Retryable: true}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
	bodyText := strings.TrimSpace(string(bodyBytes))

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		apiResponse := publishResponse{}
		externalID := "facebook-posted"
		if err := json.Unmarshal(bodyBytes, &apiResponse); err == nil {
			if strings.TrimSpace(apiResponse.ID) != "" {
				externalID = strings.TrimSpace(apiResponse.ID)
			}
		}
		p.logger.LogAttrs(
			ctx,
			slog.LevelInfo,
			"facebook publish succeeded",
			slog.String("component", "publisher"),
			slog.String("publisher", "facebook-page"),
			slog.Int64("post_id", post.ID),
			slog.String("external_id", externalID),
		)
		return publisher.PublishResult{ExternalID: externalID, Message: "facebook publish succeeded"}, nil
	}

	retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	errMessage := fmt.Sprintf("facebook publish failed (%d)", response.StatusCode)
	if parsed := parseErrorMessage(bodyBytes); parsed != "" {
		errMessage += ": " + parsed
	} else if bodyText != "" {
		errMessage += ": " + bodyText
	}

	return publisher.PublishResult{}, &publisher.PublishError{Err: errors.New(errMessage), Retryable: retryable}
}

func parseErrorMessage(body []byte) string {
	parsed := errorResponse{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Error.Message)
}
