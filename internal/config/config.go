package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr              string
	Env               string
	BaseURL           string
	DBPath            string
	BasicAuthUser     string
	BasicAuthPass     string
	StaticAPIKeys     map[string]string
	SessionSecure     bool
	Timezone          string
	Location          *time.Location
	PublisherMode     string
	LinkedInToken     string
	LinkedInAuthorURN string
	LinkedInAPIBase   string
	FacebookPageToken string
	FacebookPageID    string
	FacebookAPIBase   string
}

func Load() (Config, error) {
	timezone := getEnv("APP_TIMEZONE", "UTC")
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Config{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	cfg := Config{
		Addr:              getEnv("APP_ADDR", ":8080"),
		Env:               getEnv("APP_ENV", "development"),
		BaseURL:           getEnv("APP_BASE_URL", "http://localhost:8080"),
		DBPath:            getEnv("APP_DB_PATH", "./data/linkedin-cron.db"),
		BasicAuthUser:     getEnv("APP_BASIC_AUTH_USER", "admin"),
		BasicAuthPass:     getEnv("APP_BASIC_AUTH_PASS", "admin"),
		StaticAPIKeys:     parseStaticAPIKeys(os.Getenv("APP_STATIC_API_KEYS")),
		SessionSecure:     getEnvBool("APP_SESSION_SECURE", false),
		Timezone:          timezone,
		Location:          location,
		PublisherMode:     normalizeMode(getEnv("PUBLISHER_MODE", "dry-run")),
		LinkedInToken:     strings.TrimSpace(os.Getenv("LINKEDIN_ACCESS_TOKEN")),
		LinkedInAuthorURN: strings.TrimSpace(os.Getenv("LINKEDIN_AUTHOR_URN")),
		LinkedInAPIBase:   getEnv("LINKEDIN_API_BASE_URL", "https://api.linkedin.com"),
		FacebookPageToken: strings.TrimSpace(os.Getenv("FACEBOOK_PAGE_ACCESS_TOKEN")),
		FacebookPageID:    strings.TrimSpace(os.Getenv("FACEBOOK_PAGE_ID")),
		FacebookAPIBase:   strings.TrimRight(getEnv("FACEBOOK_API_BASE_URL", "https://graph.facebook.com/v22.0"), "/"),
	}

	return cfg, nil
}

func (c Config) BasicAuthConfigured() bool {
	return c.BasicAuthUser != "" && c.BasicAuthPass != ""
}

func (c Config) LinkedInConfigured() bool {
	return c.LinkedInToken != "" && c.LinkedInAuthorURN != ""
}

func (c Config) FacebookConfigured() bool {
	return c.FacebookPageToken != "" && c.FacebookPageID != ""
}

func MaskSecret(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 4 {
		return strings.Repeat("*", len(trimmed))
	}
	return trimmed[:2] + strings.Repeat("*", len(trimmed)-4) + trimmed[len(trimmed)-2:]
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func normalizeMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "linkedin" {
		return "linkedin"
	}
	if value == "facebook" || value == "facebook-page" {
		return "facebook-page"
	}
	return "dry-run"
}

func parseStaticAPIKeys(raw string) map[string]string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	replacer := strings.NewReplacer("\n", ",", ";", ",")
	parts := strings.Split(replacer.Replace(trimmed), ",")
	parsed := make(map[string]string)
	unnamedIndex := 0

	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}

		separator := strings.Index(item, ":")
		if separator < 0 {
			separator = strings.Index(item, "=")
		}

		if separator < 0 {
			unnamedIndex++
			name := "env-key-" + strconv.Itoa(unnamedIndex)
			parsed[item] = name
			continue
		}

		name := strings.TrimSpace(item[:separator])
		token := strings.TrimSpace(item[separator+1:])
		if token == "" {
			continue
		}
		if name == "" {
			unnamedIndex++
			name = "env-key-" + strconv.Itoa(unnamedIndex)
		}
		parsed[token] = name
	}

	if len(parsed) == 0 {
		return nil
	}
	return parsed
}
