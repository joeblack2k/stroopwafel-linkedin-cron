package publisher

import (
	"context"
	"errors"
	"strings"

	"stroopwafel/internal/model"
)

const (
	ErrorCategoryUnknown      = "unknown"
	ErrorCategoryAuthExpired  = "auth_expired"
	ErrorCategoryScopeMissing = "scope_missing"
	ErrorCategoryRateLimited  = "rate_limited"
	ErrorCategoryValidation   = "validation_error"
	ErrorCategoryUpstream     = "upstream_error"
)

type Publisher interface {
	Mode() string
	Configured() bool
	Publish(ctx context.Context, post model.Post) (PublishResult, error)
}

type PublishResult struct {
	ExternalID string
	Permalink  string
	Message    string
}

type PublishError struct {
	Err       error
	Retryable bool
	Category  string
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

func ErrorCategory(err error) string {
	var publishErr *PublishError
	if errors.As(err, &publishErr) {
		if strings.TrimSpace(publishErr.Category) != "" {
			return strings.TrimSpace(publishErr.Category)
		}
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "not enough permissions") || strings.Contains(message, "scope") || strings.Contains(message, "access_denied"):
		return ErrorCategoryScopeMissing
	case strings.Contains(message, "token") && strings.Contains(message, "expired"):
		return ErrorCategoryAuthExpired
	case strings.Contains(message, "401") || strings.Contains(message, "invalid token") || strings.Contains(message, "invalid oauth"):
		return ErrorCategoryAuthExpired
	case strings.Contains(message, "429") || strings.Contains(message, "rate limit") || strings.Contains(message, "too many requests"):
		return ErrorCategoryRateLimited
	case strings.Contains(message, "validation") || strings.Contains(message, "required"):
		return ErrorCategoryValidation
	default:
		return ErrorCategoryUnknown
	}
}
