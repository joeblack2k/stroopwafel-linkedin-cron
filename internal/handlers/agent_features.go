package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"stroopwafel/internal/db"
	"stroopwafel/internal/model"
)

const minScheduleSpacing = 30 * time.Minute

type scheduleWarning struct {
	Code            string  `json:"code"`
	Message         string  `json:"message"`
	ConflictPostIDs []int64 `json:"conflict_post_ids,omitempty"`
}

type postMutationResponse struct {
	Post     postResponse      `json:"post"`
	Warnings []scheduleWarning `json:"warnings,omitempty"`
}

type guardrailsPayload struct {
	ScheduledAt   string  `json:"scheduled_at"`
	ChannelIDs    []int64 `json:"channel_ids,omitempty"`
	ExcludePostID *int64  `json:"exclude_post_id,omitempty"`
}

type channelRulePayload struct {
	MaxTextLength  *int    `json:"max_text_length,omitempty"`
	MaxHashtags    *int    `json:"max_hashtags,omitempty"`
	RequiredPhrase *string `json:"required_phrase,omitempty"`
}

type channelRuleResponse struct {
	ChannelID      int64   `json:"channel_id"`
	MaxTextLength  *int    `json:"max_text_length,omitempty"`
	MaxHashtags    *int    `json:"max_hashtags,omitempty"`
	RequiredPhrase *string `json:"required_phrase,omitempty"`
	Enabled        bool    `json:"enabled"`
	UpdatedAt      *string `json:"updated_at,omitempty"`
}

type channelRetryPolicyPayload struct {
	MaxRetries              *int `json:"max_retries"`
	BackoffFirstSeconds     *int `json:"backoff_first_seconds"`
	BackoffSecondSeconds    *int `json:"backoff_second_seconds"`
	BackoffThirdSeconds     *int `json:"backoff_third_seconds"`
	RateLimitBackoffSeconds *int `json:"rate_limit_backoff_seconds"`
	MaxPostsPerDay          *int `json:"max_posts_per_day,omitempty"`
}

type channelRetryPolicyResponse struct {
	ChannelID               int64   `json:"channel_id"`
	MaxRetries              int     `json:"max_retries"`
	BackoffFirstSeconds     int     `json:"backoff_first_seconds"`
	BackoffSecondSeconds    int     `json:"backoff_second_seconds"`
	BackoffThirdSeconds     int     `json:"backoff_third_seconds"`
	RateLimitBackoffSeconds int     `json:"rate_limit_backoff_seconds"`
	MaxPostsPerDay          *int    `json:"max_posts_per_day,omitempty"`
	Configured              bool    `json:"configured"`
	UpdatedAt               *string `json:"updated_at,omitempty"`
}

type weeklySnapshotResponse struct {
	Start             string         `json:"start"`
	End               string         `json:"end"`
	PlannedPosts      int            `json:"planned_posts"`
	PublishedAttempts int            `json:"published_attempts"`
	FailedAttempts    int            `json:"failed_attempts"`
	RetryAttempts     int            `json:"retry_attempts"`
	TopPost           map[string]any `json:"top_post,omitempty"`
	Reach             map[string]any `json:"reach"`
	Interaction       map[string]any `json:"interaction"`
}

type analyticsOverviewChannelResponse struct {
	ChannelID   int64  `json:"channel_id"`
	ChannelType string `json:"channel_type"`
	DisplayName string `json:"display_name"`
	SentCount   int    `json:"sent_count"`
	FailedCount int    `json:"failed_count"`
	RetryCount  int    `json:"retry_count"`
}

type analyticsOverviewResponse struct {
	TotalPosts  int                                `json:"total_posts"`
	SentCount   int                                `json:"sent_count"`
	FailedCount int                                `json:"failed_count"`
	RetryCount  int                                `json:"retry_count"`
	Channels    []analyticsOverviewChannelResponse `json:"channels"`
}

func (a *App) APIAnalyticsOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := a.Store.GetAnalyticsOverview(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load analytics overview")
		return
	}

	channels := make([]analyticsOverviewChannelResponse, 0, len(overview.Channels))
	for _, channel := range overview.Channels {
		channels = append(channels, analyticsOverviewChannelResponse{
			ChannelID:   channel.ChannelID,
			ChannelType: string(channel.ChannelType),
			DisplayName: channel.DisplayName,
			SentCount:   channel.SentCount,
			FailedCount: channel.FailedCount,
			RetryCount:  channel.RetryCount,
		})
	}

	writeJSON(w, http.StatusOK, analyticsOverviewResponse{
		TotalPosts:  overview.TotalPosts,
		SentCount:   overview.SentCount,
		FailedCount: overview.FailedCount,
		RetryCount:  overview.RetryCount,
		Channels:    channels,
	})
}

func (a *App) APICheckPostGuardrails(w http.ResponseWriter, r *http.Request) {
	var payload guardrailsPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	scheduledAt, err := parseRFC3339(payload.ScheduledAt)
	if err != nil || scheduledAt == nil {
		writeAPIError(w, http.StatusBadRequest, "scheduled_at must be RFC3339")
		return
	}

	excludeID := int64(0)
	if payload.ExcludePostID != nil {
		excludeID = *payload.ExcludePostID
	}

	warnings, warnErr := a.computeSchedulingWarnings(r.Context(), *scheduledAt, dedupeChannelIDs(payload.ChannelIDs), excludeID)
	if warnErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to compute guardrails")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"warnings": warnings})
}

func (a *App) APIGetChannelRules(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if _, err := a.Store.GetChannel(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load channel")
		return
	}

	rule, found, err := a.Store.GetChannelRule(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load channel rules")
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, channelRuleResponse{ChannelID: id, Enabled: false})
		return
	}
	writeJSON(w, http.StatusOK, mapChannelRuleResponse(rule))
}

func (a *App) APIUpdateChannelRules(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if _, err := a.Store.GetChannel(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load channel")
		return
	}

	var payload channelRulePayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	rule, err := a.Store.UpsertChannelRule(r.Context(), id, db.ChannelRuleInput{
		MaxTextLength:  payload.MaxTextLength,
		MaxHashtags:    payload.MaxHashtags,
		RequiredPhrase: payload.RequiredPhrase,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapChannelRuleResponse(rule))
}

func (a *App) APIGetChannelRetryPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if _, err := a.Store.GetChannel(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load channel")
		return
	}

	policy, found, err := a.Store.GetChannelRetryPolicy(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load channel retry policy")
		return
	}
	if !found {
		policy = model.DefaultChannelRetryPolicy(id)
	}

	writeJSON(w, http.StatusOK, mapChannelRetryPolicyResponse(policy, found))
}

func (a *App) APIUpdateChannelRetryPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid channel id")
		return
	}

	if _, err := a.Store.GetChannel(r.Context(), id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "channel not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "failed to load channel")
		return
	}

	var payload channelRetryPolicyPayload
	if err := readJSONBody(r, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	if payload.MaxRetries == nil || payload.BackoffFirstSeconds == nil || payload.BackoffSecondSeconds == nil || payload.BackoffThirdSeconds == nil || payload.RateLimitBackoffSeconds == nil {
		writeAPIError(w, http.StatusBadRequest, "max_retries, backoff_first_seconds, backoff_second_seconds, backoff_third_seconds, and rate_limit_backoff_seconds are required")
		return
	}

	policy, err := a.Store.UpsertChannelRetryPolicy(r.Context(), id, db.ChannelRetryPolicyInput{
		MaxRetries:              *payload.MaxRetries,
		BackoffFirstSeconds:     *payload.BackoffFirstSeconds,
		BackoffSecondSeconds:    *payload.BackoffSecondSeconds,
		BackoffThirdSeconds:     *payload.BackoffThirdSeconds,
		RateLimitBackoffSeconds: *payload.RateLimitBackoffSeconds,
		MaxPostsPerDay:          payload.MaxPostsPerDay,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, mapChannelRetryPolicyResponse(policy, true))
}

func (a *App) APIWeeklySnapshot(w http.ResponseWriter, r *http.Request) {
	location := a.Config.Location
	if location == nil {
		location = time.UTC
	}

	start := strings.TrimSpace(r.URL.Query().Get("start"))
	var startTime time.Time
	if start == "" {
		now := time.Now().In(location)
		weekStart := beginningOfWeek(now)
		startTime = time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, location).UTC()
	} else {
		parsed, err := time.Parse(time.RFC3339, start)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "start must be RFC3339")
			return
		}
		startTime = parsed.UTC()
	}
	endTime := startTime.Add(7 * 24 * time.Hour)

	planned, err := a.Store.ListPostsByScheduledRange(r.Context(), startTime, endTime)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load planned posts")
		return
	}

	attempts, err := a.Store.ListPublishAttemptsByRange(r.Context(), startTime, endTime)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to load publish attempts")
		return
	}

	publishedAttempts := 0
	failedAttempts := 0
	retryAttempts := 0
	topCounts := map[int64]int{}
	for _, attempt := range attempts {
		switch attempt.Status {
		case model.PublishAttemptStatusSent:
			publishedAttempts++
			topCounts[attempt.PostID]++
		case model.PublishAttemptStatusFailed:
			failedAttempts++
		case model.PublishAttemptStatusRetry:
			retryAttempts++
		}
	}

	var topPost map[string]any
	if len(topCounts) > 0 {
		type rankedPost struct {
			PostID int64
			Count  int
		}
		ranked := make([]rankedPost, 0, len(topCounts))
		for postID, count := range topCounts {
			ranked = append(ranked, rankedPost{PostID: postID, Count: count})
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].Count == ranked[j].Count {
				return ranked[i].PostID < ranked[j].PostID
			}
			return ranked[i].Count > ranked[j].Count
		})

		topID := ranked[0].PostID
		post, postErr := a.Store.GetPost(r.Context(), topID)
		if postErr == nil {
			topPost = map[string]any{
				"post_id":             topID,
				"successful_attempts": ranked[0].Count,
				"status":              post.Status,
				"text_preview":        clipText(post.Text, 140),
			}
		}
	}

	writeJSON(w, http.StatusOK, weeklySnapshotResponse{
		Start:             startTime.Format(time.RFC3339),
		End:               endTime.Format(time.RFC3339),
		PlannedPosts:      len(planned),
		PublishedAttempts: publishedAttempts,
		FailedAttempts:    failedAttempts,
		RetryAttempts:     retryAttempts,
		TopPost:           topPost,
		Reach: map[string]any{
			"available": false,
			"message":   "Native reach metrics require per-platform analytics permissions and are not available in MVP.",
		},
		Interaction: map[string]any{
			"available": false,
			"message":   "Native interaction metrics require per-platform analytics permissions and are not available in MVP.",
		},
	})
}

func mapChannelRuleResponse(rule model.ChannelRule) channelRuleResponse {
	response := channelRuleResponse{
		ChannelID:      rule.ChannelID,
		MaxTextLength:  rule.MaxTextLength,
		MaxHashtags:    rule.MaxHashtags,
		RequiredPhrase: rule.RequiredPhrase,
		Enabled:        rule.Enabled(),
	}
	if !rule.UpdatedAt.IsZero() {
		formatted := rule.UpdatedAt.UTC().Format(time.RFC3339)
		response.UpdatedAt = &formatted
	}
	return response
}

func mapChannelRetryPolicyResponse(policy model.ChannelRetryPolicy, configured bool) channelRetryPolicyResponse {
	response := channelRetryPolicyResponse{
		ChannelID:               policy.ChannelID,
		MaxRetries:              policy.MaxRetries,
		BackoffFirstSeconds:     policy.BackoffFirstSeconds,
		BackoffSecondSeconds:    policy.BackoffSecondSeconds,
		BackoffThirdSeconds:     policy.BackoffThirdSeconds,
		RateLimitBackoffSeconds: policy.RateLimitBackoffSeconds,
		MaxPostsPerDay:          policy.MaxPostsPerDay,
		Configured:              configured,
	}
	if !policy.UpdatedAt.IsZero() {
		formatted := policy.UpdatedAt.UTC().Format(time.RFC3339)
		response.UpdatedAt = &formatted
	}
	return response
}

func (a *App) validatePostAgainstChannelRules(ctx context.Context, text string, channelIDs []int64) error {
	uniqueChannelIDs := dedupeChannelIDs(channelIDs)
	if len(uniqueChannelIDs) == 0 {
		return nil
	}

	channels, err := a.Store.ListChannelsByIDs(ctx, uniqueChannelIDs)
	if err != nil {
		return fmt.Errorf("failed to load channels")
	}
	if len(channels) != len(uniqueChannelIDs) {
		return fmt.Errorf("one or more channels were not found")
	}

	rules, err := a.Store.ListChannelRulesByChannelIDs(ctx, uniqueChannelIDs)
	if err != nil {
		return fmt.Errorf("failed to load channel rules")
	}

	violations := make([]string, 0)
	for _, channel := range channels {
		rule, exists := rules[channel.ID]
		if !exists || !rule.Enabled() {
			continue
		}
		if violation := validateTextWithRule(text, channel.DisplayName, rule); violation != "" {
			violations = append(violations, violation)
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("channel rule violation: %s", strings.Join(violations, "; "))
	}
	return nil
}

func (a *App) validatePostAgainstChannelCapabilities(ctx context.Context, mediaType *string, mediaURL *string, channelIDs []int64) error {
	uniqueChannelIDs := dedupeChannelIDs(channelIDs)
	if len(uniqueChannelIDs) == 0 {
		return nil
	}

	channels, err := a.Store.ListChannelsByIDs(ctx, uniqueChannelIDs)
	if err != nil {
		return fmt.Errorf("failed to load channels")
	}
	if len(channels) != len(uniqueChannelIDs) {
		return fmt.Errorf("one or more channels were not found")
	}

	hasMedia := strings.TrimSpace(derefString(mediaURL)) != ""
	effectiveMediaType := model.NormalizeMediaType(mediaType)
	if hasMedia && effectiveMediaType == nil {
		effectiveMediaType = model.InferMediaTypeFromURL(derefString(mediaURL))
	}

	violations := make([]string, 0)
	for _, channel := range channels {
		capabilities := channel.Capabilities()
		if capabilities.RequiresMedia && !hasMedia {
			violations = append(violations, fmt.Sprintf("%s requires media", channel.DisplayName))
			continue
		}
		if !hasMedia {
			continue
		}
		if !capabilities.SupportsMedia {
			violations = append(violations, fmt.Sprintf("%s does not support media", channel.DisplayName))
			continue
		}
		if effectiveMediaType != nil && !capabilities.SupportsMediaType(*effectiveMediaType) {
			violations = append(violations, fmt.Sprintf("%s does not support media_type=%s", channel.DisplayName, *effectiveMediaType))
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("channel capability violation: %s", strings.Join(violations, "; "))
	}

	return nil
}

func validateTextWithRule(text, channelName string, rule model.ChannelRule) string {
	trimmed := strings.TrimSpace(text)
	if rule.MaxTextLength != nil {
		if len([]rune(trimmed)) > *rule.MaxTextLength {
			return fmt.Sprintf("%s max_text_length=%d exceeded", channelName, *rule.MaxTextLength)
		}
	}
	if rule.MaxHashtags != nil {
		if countHashtagsInText(trimmed) > *rule.MaxHashtags {
			return fmt.Sprintf("%s max_hashtags=%d exceeded", channelName, *rule.MaxHashtags)
		}
	}
	if rule.RequiredPhrase != nil {
		required := strings.TrimSpace(*rule.RequiredPhrase)
		if required != "" {
			if !strings.Contains(strings.ToLower(trimmed), strings.ToLower(required)) {
				return fmt.Sprintf("%s required_phrase is missing", channelName)
			}
		}
	}
	return ""
}

func countHashtagsInText(text string) int {
	count := 0
	for _, token := range strings.Fields(text) {
		trimmed := strings.TrimSpace(token)
		if len(trimmed) <= 1 {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			count++
		}
	}
	return count
}

func (a *App) computeSchedulingWarnings(ctx context.Context, scheduledAt time.Time, channelIDs []int64, excludePostID int64) ([]scheduleWarning, error) {
	windowStart := scheduledAt.Add(-minScheduleSpacing).UTC()
	windowEnd := scheduledAt.Add(minScheduleSpacing).UTC()

	candidates, err := a.Store.ListScheduledPostsInWindow(ctx, windowStart, windowEnd, excludePostID)
	if err != nil {
		return nil, err
	}

	selectedChannels := selectedChannelIDMap(dedupeChannelIDs(channelIDs))
	duplicateIDs := make([]int64, 0)
	tooCloseIDs := make([]int64, 0)

	for _, candidate := range candidates {
		if candidate.ScheduledAt == nil {
			continue
		}
		if len(selectedChannels) > 0 {
			candidateChannels, channelErr := a.Store.ListPostChannelIDs(ctx, candidate.ID)
			if channelErr != nil {
				return nil, channelErr
			}
			overlaps := false
			for _, candidateChannelID := range candidateChannels {
				if selectedChannels[candidateChannelID] {
					overlaps = true
					break
				}
			}
			if !overlaps {
				continue
			}
		}

		delta := candidate.ScheduledAt.Sub(scheduledAt)
		if delta < 0 {
			delta = -delta
		}

		if delta == 0 {
			duplicateIDs = append(duplicateIDs, candidate.ID)
			continue
		}
		if delta < minScheduleSpacing {
			tooCloseIDs = append(tooCloseIDs, candidate.ID)
		}
	}

	warnings := make([]scheduleWarning, 0, 2)
	if len(duplicateIDs) > 0 {
		warnings = append(warnings, scheduleWarning{
			Code:            "duplicate_time_slot",
			Message:         fmt.Sprintf("Exact hetzelfde tijdslot bestaat al voor %d andere post(s)", len(duplicateIDs)),
			ConflictPostIDs: duplicateIDs,
		})
	}
	if len(tooCloseIDs) > 0 {
		warnings = append(warnings, scheduleWarning{
			Code:            "too_close_interval",
			Message:         fmt.Sprintf("Er zijn %d post(s) binnen %d minuten van dit tijdslot", len(tooCloseIDs), int(minScheduleSpacing.Minutes())),
			ConflictPostIDs: tooCloseIDs,
		})
	}

	if len(selectedChannels) > 0 {
		dayStart := time.Date(scheduledAt.UTC().Year(), scheduledAt.UTC().Month(), scheduledAt.UTC().Day(), 0, 0, 0, 0, time.UTC)
		dayEnd := dayStart.Add(24 * time.Hour)

		channels, err := a.Store.ListChannelsByIDs(ctx, dedupeChannelIDs(channelIDs))
		if err != nil {
			return nil, err
		}
		policies, err := a.Store.ListChannelRetryPoliciesByChannelIDs(ctx, dedupeChannelIDs(channelIDs))
		if err != nil {
			return nil, err
		}

		for _, channel := range channels {
			policy, exists := policies[channel.ID]
			if !exists || !policy.HasDailyLimit() {
				continue
			}

			plannedCount, countErr := a.Store.CountPlannedPostsForChannelBetween(ctx, channel.ID, dayStart, dayEnd, excludePostID)
			if countErr != nil {
				return nil, countErr
			}
			if plannedCount >= *policy.MaxPostsPerDay {
				warnings = append(warnings, scheduleWarning{
					Code:    "channel_daily_limit",
					Message: fmt.Sprintf("%s heeft al %d post(s) op deze dag (limiet %d)", channel.DisplayName, plannedCount, *policy.MaxPostsPerDay),
				})
			}
		}
	}

	return warnings, nil
}
