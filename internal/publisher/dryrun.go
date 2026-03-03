package publisher

import (
	"context"
	"fmt"
	"log/slog"

	"linkedin-cron/internal/model"
)

type DryRunPublisher struct {
	logger *slog.Logger
}

func NewDryRunPublisher(logger *slog.Logger) *DryRunPublisher {
	return &DryRunPublisher{logger: logger}
}

func (p *DryRunPublisher) Mode() string {
	return "dry-run"
}

func (p *DryRunPublisher) Configured() bool {
	return true
}

func (p *DryRunPublisher) Publish(ctx context.Context, post model.Post) (PublishResult, error) {
	p.logger.LogAttrs(
		ctx,
		slog.LevelInfo,
		"dry-run publish",
		slog.String("component", "publisher"),
		slog.String("publisher", "dry-run"),
		slog.Int64("post_id", post.ID),
		slog.String("status", string(post.Status)),
	)

	return PublishResult{
		ExternalID: fmt.Sprintf("dry-run-%d", post.ID),
		Permalink:  fmt.Sprintf("/posts/%d", post.ID),
		Message:    "dry run successful",
	}, nil
}
