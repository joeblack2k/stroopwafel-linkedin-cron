package linkedin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"linkedin-cron/internal/model"
	"linkedin-cron/internal/publisher"
)

type Publisher struct {
	httpClient *http.Client
	baseURL    string
	token      string
	authorURN  string
	logger     *slog.Logger
}

func NewPublisher(baseURL, token, authorURN string, logger *slog.Logger) *Publisher {
	return &Publisher{
		httpClient: &http.Client{Timeout: 20 * time.Second},
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:      strings.TrimSpace(token),
		authorURN:  strings.TrimSpace(authorURN),
		logger:     logger,
	}
}

func (p *Publisher) Mode() string {
	return "linkedin"
}

func (p *Publisher) Configured() bool {
	return p.token != "" && p.authorURN != "" && p.baseURL != ""
}

func (p *Publisher) Probe(ctx context.Context) error {
	if !p.Configured() {
		return &publisher.PublishError{Err: fmt.Errorf("linkedin publisher is not configured"), Retryable: false}
	}
	if !strings.HasPrefix(p.authorURN, "urn:li:") {
		return &publisher.PublishError{Err: errors.New("linkedin_author_urn must start with urn:li:"), Retryable: false}
	}

	endpoint := p.baseURL + "/v2/userinfo"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &publisher.PublishError{Err: fmt.Errorf("build linkedin probe request: %w", err), Retryable: true}
	}
	request.Header.Set("Authorization", "Bearer "+p.token)
	request.Header.Set("Accept", "application/json")

	response, err := p.httpClient.Do(request)
	if err != nil {
		return &publisher.PublishError{Err: fmt.Errorf("linkedin probe request failed: %w", err), Retryable: true}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
	bodyText := strings.TrimSpace(string(bodyBytes))

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}

	retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	errMessage := fmt.Sprintf("linkedin probe failed (%d)", response.StatusCode)
	if bodyText != "" {
		errMessage += ": " + bodyText
	}
	return &publisher.PublishError{Err: errors.New(errMessage), Retryable: retryable}
}

func (p *Publisher) Publish(ctx context.Context, post model.Post) (publisher.PublishResult, error) {
	if !p.Configured() {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("linkedin publisher is not configured"), Retryable: false}
	}

	body := map[string]any{
		"author":         p.authorURN,
		"lifecycleState": "PUBLISHED",
		"specificContent": map[string]any{
			"com.linkedin.ugc.ShareContent": map[string]any{
				"shareCommentary": map[string]any{
					"text": post.Text,
				},
				"shareMediaCategory": "NONE",
			},
		},
		"visibility": map[string]any{
			"com.linkedin.ugc.MemberNetworkVisibility": "PUBLIC",
		},
	}

	if post.MediaURL != nil && strings.TrimSpace(*post.MediaURL) != "" {
		body["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)["shareMediaCategory"] = "ARTICLE"
		body["specificContent"].(map[string]any)["com.linkedin.ugc.ShareContent"].(map[string]any)["media"] = []map[string]any{
			{
				"status":      "READY",
				"originalUrl": strings.TrimSpace(*post.MediaURL),
				"description": map[string]any{"text": "Shared from linkedin-cron"},
				"title":       map[string]any{"text": "External link"},
			},
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("marshal linkedin payload: %w", err), Retryable: false}
	}

	endpoint := p.baseURL + "/v2/ugcPosts"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("build linkedin request: %w", err), Retryable: true}
	}
	request.Header.Set("Authorization", "Bearer "+p.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	response, err := p.httpClient.Do(request)
	if err != nil {
		return publisher.PublishResult{}, &publisher.PublishError{Err: fmt.Errorf("linkedin request failed: %w", err), Retryable: true}
	}
	defer response.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
	bodyText := strings.TrimSpace(string(bodyBytes))

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		resourceID := response.Header.Get("x-restli-id")
		if resourceID == "" {
			resourceID = "linkedin-posted"
		}
		p.logger.LogAttrs(
			ctx,
			slog.LevelInfo,
			"linkedin publish succeeded",
			slog.String("component", "publisher"),
			slog.String("publisher", "linkedin"),
			slog.Int64("post_id", post.ID),
			slog.String("external_id", resourceID),
		)
		return publisher.PublishResult{ExternalID: resourceID, Message: "linkedin publish succeeded"}, nil
	}

	retryable := response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
	errMessage := fmt.Sprintf("linkedin publish failed (%d)", response.StatusCode)
	if bodyText != "" {
		errMessage += ": " + bodyText
	}

	return publisher.PublishResult{}, &publisher.PublishError{Err: errors.New(errMessage), Retryable: retryable}
}
