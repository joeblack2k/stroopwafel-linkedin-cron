package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"linkedin-cron/internal/model"
)

type ChannelHealthSummary struct {
	ChannelID     int64
	ChannelType   model.ChannelType
	DisplayName   string
	Configured    bool
	LastSuccessAt *time.Time
	LastAttemptAt *time.Time
	SentCount     int
	FailedCount   int
	RetryCount    int
}

func (s *Store) ListChannelHealthSummaries(ctx context.Context) ([]ChannelHealthSummary, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			c.id,
			c.type,
			c.display_name,
			c.linkedin_access_token,
			c.linkedin_author_urn,
			c.facebook_page_access_token,
			c.facebook_page_id,
			c.instagram_access_token,
			c.instagram_business_account_id,
			MAX(pa.attempted_at) AS last_attempt_at,
			MAX(CASE WHEN pa.status = 'sent' THEN pa.attempted_at ELSE NULL END) AS last_success_at,
			COALESCE(SUM(CASE WHEN pa.status = 'sent' THEN 1 ELSE 0 END), 0) AS sent_count,
			COALESCE(SUM(CASE WHEN pa.status = 'failed' THEN 1 ELSE 0 END), 0) AS failed_count,
			COALESCE(SUM(CASE WHEN pa.status = 'retry' THEN 1 ELSE 0 END), 0) AS retry_count
		 FROM channels c
		 LEFT JOIN publish_attempts pa ON pa.channel_id = c.id
		 GROUP BY
			c.id,
			c.type,
			c.display_name,
			c.linkedin_access_token,
			c.linkedin_author_urn,
			c.facebook_page_access_token,
			c.facebook_page_id,
			c.instagram_access_token,
			c.instagram_business_account_id
		 ORDER BY c.id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list channel health summaries: %w", err)
	}
	defer rows.Close()

	summaries := make([]ChannelHealthSummary, 0)
	for rows.Next() {
		var (
			summary                    ChannelHealthSummary
			channelType                string
			linkedInAccessToken        sql.NullString
			linkedInAuthorURN          sql.NullString
			facebookPageAccessToken    sql.NullString
			facebookPageID             sql.NullString
			instagramAccessToken       sql.NullString
			instagramBusinessAccountID sql.NullString
			lastAttemptAt              sql.NullString
			lastSuccessAt              sql.NullString
		)

		if scanErr := rows.Scan(
			&summary.ChannelID,
			&channelType,
			&summary.DisplayName,
			&linkedInAccessToken,
			&linkedInAuthorURN,
			&facebookPageAccessToken,
			&facebookPageID,
			&instagramAccessToken,
			&instagramBusinessAccountID,
			&lastAttemptAt,
			&lastSuccessAt,
			&summary.SentCount,
			&summary.FailedCount,
			&summary.RetryCount,
		); scanErr != nil {
			return nil, fmt.Errorf("scan channel health summary row: %w", scanErr)
		}

		summary.ChannelType = model.ChannelType(channelType)
		summary.Configured = isConfiguredChannel(ChannelInput{
			Type:                       summary.ChannelType,
			DisplayName:                summary.DisplayName,
			LinkedInAccessToken:        nullableTrimmedPointer(linkedInAccessToken),
			LinkedInAuthorURN:          nullableTrimmedPointer(linkedInAuthorURN),
			FacebookPageAccessToken:    nullableTrimmedPointer(facebookPageAccessToken),
			FacebookPageID:             nullableTrimmedPointer(facebookPageID),
			InstagramAccessToken:       nullableTrimmedPointer(instagramAccessToken),
			InstagramBusinessAccountID: nullableTrimmedPointer(instagramBusinessAccountID),
		})

		parsedLastAttemptAt, parseErr := parseNullableDBTime(lastAttemptAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parse channel health summary last_attempt_at: %w", parseErr)
		}
		summary.LastAttemptAt = parsedLastAttemptAt

		parsedLastSuccessAt, parseErr := parseNullableDBTime(lastSuccessAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parse channel health summary last_success_at: %w", parseErr)
		}
		summary.LastSuccessAt = parsedLastSuccessAt

		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channel health summary rows: %w", err)
	}

	return summaries, nil
}

func nullableTrimmedPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	trimmed := strings.TrimSpace(value.String)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func isConfiguredChannel(input ChannelInput) bool {
	_, err := validateChannelConfig(input)
	return err == nil
}

func parseNullableDBTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseDBTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
