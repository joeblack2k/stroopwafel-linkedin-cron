package publisher

import (
	"context"
	"errors"

	"linkedin-cron/internal/model"
)

type Publisher interface {
	Mode() string
	Configured() bool
	Publish(ctx context.Context, post model.Post) (PublishResult, error)
}

type PublishResult struct {
	ExternalID string
	Message    string
}

type PublishError struct {
	Err       error
	Retryable bool
}

func (e *PublishError) Error() string {
	if e == nil || e.Err == nil {
		return "publish error"
	}
	return e.Err.Error()
}

func (e *PublishError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func IsRetryable(err error) bool {
	var publishErr *PublishError
	if errors.As(err, &publishErr) {
		return publishErr.Retryable
	}
	return true
}
