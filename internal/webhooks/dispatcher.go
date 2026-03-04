package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Dispatcher struct {
	urls    []string
	secret  string
	client  *http.Client
	logger  *slog.Logger
	source  string
	enabled bool
}

type EventEnvelope struct {
	ID         string         `json:"id"`
	Event      string         `json:"event"`
	OccurredAt string         `json:"occurred_at"`
	Source     string         `json:"source"`
	Payload    map[string]any `json:"payload"`
}

type DeliveryOutcome struct {
	EventID     string
	EventName   string
	TargetURL   string
	Source      string
	OccurredAt  time.Time
	DeliveredAt time.Time
	DurationMS  int64
	Delivered   bool
	HTTPStatus  *int
	Error       *string
	Payload     map[string]any
}

func NewDispatcher(urls []string, secret string, source string, logger *slog.Logger) *Dispatcher {
	sanitized := normalizeURLs(urls)
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	return &Dispatcher{
		urls:    sanitized,
		secret:  strings.TrimSpace(secret),
		source:  strings.TrimSpace(source),
		logger:  logger,
		client:  &http.Client{Timeout: 4 * time.Second},
		enabled: len(sanitized) > 0,
	}
}

func (d *Dispatcher) Enabled() bool {
	return d != nil && d.enabled
}

func (d *Dispatcher) Emit(ctx context.Context, eventName string, payload map[string]any) []DeliveryOutcome {
	if !d.Enabled() {
		return nil
	}

	occurredAt := time.Now().UTC()
	envelope := EventEnvelope{
		ID:         buildEventID(),
		Event:      strings.TrimSpace(eventName),
		OccurredAt: occurredAt.Format(time.RFC3339),
		Source:     defaultSource(d.source),
		Payload:    payload,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		d.logger.LogAttrs(ctx, slog.LevelWarn, "failed to encode webhook event", slog.String("component", "webhook"), slog.String("error", err.Error()))
		return nil
	}

	results := make([]DeliveryOutcome, 0, len(d.urls))
	for _, endpoint := range d.urls {
		httpStatus, sendErr, durationMS := d.deliver(ctx, endpoint, envelope, encoded)
		deliveredAt := time.Now().UTC()

		outcome := DeliveryOutcome{
			EventID:     envelope.ID,
			EventName:   envelope.Event,
			TargetURL:   endpoint,
			Source:      envelope.Source,
			OccurredAt:  occurredAt,
			DeliveredAt: deliveredAt,
			DurationMS:  durationMS,
			Delivered:   sendErr == nil,
		}
		if httpStatus > 0 {
			status := httpStatus
			outcome.HTTPStatus = &status
		}
		if sendErr != nil {
			errText := sendErr.Error()
			outcome.Error = &errText
			d.logger.LogAttrs(ctx, slog.LevelWarn, "webhook delivery failed", slog.String("component", "webhook"), slog.String("event", envelope.Event), slog.String("url", endpoint), slog.String("error", sendErr.Error()))
		} else {
			d.logger.LogAttrs(ctx, slog.LevelInfo, "webhook delivered", slog.String("component", "webhook"), slog.String("event", envelope.Event), slog.String("url", endpoint), slog.String("event_id", envelope.ID))
		}

		results = append(results, outcome)
	}

	return results
}

func (d *Dispatcher) deliver(parent context.Context, endpoint string, envelope EventEnvelope, body []byte) (int, error, int64) {
	startedAt := time.Now()
	deliverCtx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()

	request, err := http.NewRequestWithContext(deliverCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build webhook request: %w", err), time.Since(startedAt).Milliseconds()
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Stroopwafel-Event", envelope.Event)
	request.Header.Set("X-Stroopwafel-Event-Id", envelope.ID)
	request.Header.Set("X-Stroopwafel-Timestamp", envelope.OccurredAt)
	if d.secret != "" {
		request.Header.Set("X-Stroopwafel-Signature", d.signature(envelope.OccurredAt, body))
	}

	response, err := d.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("send webhook request: %w", err), time.Since(startedAt).Milliseconds()
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, fmt.Errorf("unexpected webhook status %d", response.StatusCode), time.Since(startedAt).Milliseconds()
	}
	return response.StatusCode, nil, time.Since(startedAt).Milliseconds()
}

func (d *Dispatcher) signature(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(d.secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func normalizeURLs(urls []string) []string {
	values := make([]string, 0, len(urls))
	seen := make(map[string]struct{}, len(urls))
	for _, raw := range urls {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		values = append(values, trimmed)
	}
	return values
}

func clonePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func defaultSource(source string) string {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return "stroopwafel-social-media-manager"
	}
	return trimmed
}

func buildEventID() string {
	return fmt.Sprintf("evt_%d", time.Now().UTC().UnixNano())
}
