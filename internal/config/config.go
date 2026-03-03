package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr              string
	Env               string
	BaseURL           string
	DataDir           string
	ConfigPath        string
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
	WebhookURLs       []string
	WebhookSecret     string
}

type PersistedConfig struct {
	Version           int               `json:"version"`
	BasicAuthUser     string            `json:"basic_auth_user"`
	BasicAuthPass     string            `json:"basic_auth_pass"`
	StaticAPIKeys     map[string]string `json:"static_api_keys,omitempty"`
	Timezone          string            `json:"timezone"`
	PublisherMode     string            `json:"publisher_mode"`
	LinkedInToken     string            `json:"linkedin_access_token,omitempty"`
	LinkedInAuthorURN string            `json:"linkedin_author_urn,omitempty"`
	LinkedInAPIBase   string            `json:"linkedin_api_base_url,omitempty"`
	FacebookPageToken string            `json:"facebook_page_access_token,omitempty"`
	FacebookPageID    string            `json:"facebook_page_id,omitempty"`
	FacebookAPIBase   string            `json:"facebook_api_base_url,omitempty"`
}

const persistedConfigVersion = 1

func Load() (Config, error) {
	dataDir := getEnv("APP_DATA_DIR", "./data")
	configPath := getEnv("APP_CONFIG_PATH", filepath.Join(dataDir, "config.json"))
	dbPath := getEnv("APP_DB_PATH", filepath.Join(dataDir, "linkedin-cron.db"))

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("create data dir %q: %w", dataDir, err)
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return Config{}, fmt.Errorf("create config dir for %q: %w", configPath, err)
	}

	persisted, err := loadOrCreatePersistedConfig(configPath)
	if err != nil {
		return Config{}, err
	}
	persisted = applyPersistedEnvOverrides(persisted)
	persisted = normalizePersistedConfig(persisted)

	timezone := strings.TrimSpace(persisted.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Config{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	cfg := Config{
		Addr:              getEnv("APP_ADDR", ":8080"),
		Env:               getEnv("APP_ENV", "development"),
		BaseURL:           getEnv("APP_BASE_URL", "http://localhost:8080"),
		DataDir:           dataDir,
		ConfigPath:        configPath,
		DBPath:            dbPath,
		BasicAuthUser:     persisted.BasicAuthUser,
		BasicAuthPass:     persisted.BasicAuthPass,
		StaticAPIKeys:     persisted.StaticAPIKeys,
		SessionSecure:     getEnvBool("APP_SESSION_SECURE", false),
		Timezone:          timezone,
		Location:          location,
		PublisherMode:     normalizeMode(persisted.PublisherMode),
		LinkedInToken:     strings.TrimSpace(persisted.LinkedInToken),
		LinkedInAuthorURN: strings.TrimSpace(persisted.LinkedInAuthorURN),
		LinkedInAPIBase:   defaultIfEmpty(strings.TrimSpace(persisted.LinkedInAPIBase), "https://api.linkedin.com"),
		FacebookPageToken: strings.TrimSpace(persisted.FacebookPageToken),
		FacebookPageID:    strings.TrimSpace(persisted.FacebookPageID),
		FacebookAPIBase:   strings.TrimRight(defaultIfEmpty(strings.TrimSpace(persisted.FacebookAPIBase), "https://graph.facebook.com/v22.0"), "/"),
		WebhookURLs:       parseWebhookURLs(os.Getenv("APP_WEBHOOK_URLS")),
		WebhookSecret:     strings.TrimSpace(os.Getenv("APP_WEBHOOK_SECRET")),
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

func loadOrCreatePersistedConfig(path string) (PersistedConfig, error) {
	persisted, err := readPersistedConfig(path)
	if err == nil {
		return normalizePersistedConfig(persisted), nil
	}
	if !os.IsNotExist(err) {
		return PersistedConfig{}, fmt.Errorf("read config file %q: %w", path, err)
	}

	bootstrap := normalizePersistedConfig(persistedConfigFromEnv())
	if err := writePersistedConfig(path, bootstrap); err != nil {
		return PersistedConfig{}, fmt.Errorf("write initial config file %q: %w", path, err)
	}
	return bootstrap, nil
}

func readPersistedConfig(path string) (PersistedConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return PersistedConfig{}, err
	}
	var persisted PersistedConfig
	if err := json.Unmarshal(content, &persisted); err != nil {
		return PersistedConfig{}, fmt.Errorf("decode config json: %w", err)
	}
	return persisted, nil
}

func writePersistedConfig(path string, persisted PersistedConfig) error {
	encoded, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config json: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return err
	}
	return nil
}

func persistedConfigFromEnv() PersistedConfig {
	return PersistedConfig{
		Version:           persistedConfigVersion,
		BasicAuthUser:     getEnv("APP_BASIC_AUTH_USER", "admin"),
		BasicAuthPass:     getEnv("APP_BASIC_AUTH_PASS", "admin"),
		StaticAPIKeys:     parseStaticAPIKeys(os.Getenv("APP_STATIC_API_KEYS")),
		Timezone:          getEnv("APP_TIMEZONE", "UTC"),
		PublisherMode:     normalizeMode(getEnv("PUBLISHER_MODE", "dry-run")),
		LinkedInToken:     strings.TrimSpace(os.Getenv("LINKEDIN_ACCESS_TOKEN")),
		LinkedInAuthorURN: strings.TrimSpace(os.Getenv("LINKEDIN_AUTHOR_URN")),
		LinkedInAPIBase:   getEnv("LINKEDIN_API_BASE_URL", "https://api.linkedin.com"),
		FacebookPageToken: strings.TrimSpace(os.Getenv("FACEBOOK_PAGE_ACCESS_TOKEN")),
		FacebookPageID:    strings.TrimSpace(os.Getenv("FACEBOOK_PAGE_ID")),
		FacebookAPIBase:   strings.TrimRight(getEnv("FACEBOOK_API_BASE_URL", "https://graph.facebook.com/v22.0"), "/"),
	}
}

func applyPersistedEnvOverrides(persisted PersistedConfig) PersistedConfig {
	if value, ok := lookupEnvTrimmed("APP_BASIC_AUTH_USER"); ok {
		persisted.BasicAuthUser = value
	}
	if value, ok := lookupEnvTrimmed("APP_BASIC_AUTH_PASS"); ok {
		persisted.BasicAuthPass = value
	}
	if value, ok := lookupEnvTrimmed("APP_TIMEZONE"); ok {
		persisted.Timezone = value
	}
	if value, ok := lookupEnvTrimmed("PUBLISHER_MODE"); ok {
		persisted.PublisherMode = value
	}
	if value, ok := lookupEnvTrimmed("LINKEDIN_ACCESS_TOKEN"); ok {
		persisted.LinkedInToken = value
	}
	if value, ok := lookupEnvTrimmed("LINKEDIN_AUTHOR_URN"); ok {
		persisted.LinkedInAuthorURN = value
	}
	if value, ok := lookupEnvTrimmed("LINKEDIN_API_BASE_URL"); ok {
		persisted.LinkedInAPIBase = value
	}
	if value, ok := lookupEnvTrimmed("FACEBOOK_PAGE_ACCESS_TOKEN"); ok {
		persisted.FacebookPageToken = value
	}
	if value, ok := lookupEnvTrimmed("FACEBOOK_PAGE_ID"); ok {
		persisted.FacebookPageID = value
	}
	if value, ok := lookupEnvTrimmed("FACEBOOK_API_BASE_URL"); ok {
		persisted.FacebookAPIBase = value
	}
	if value, ok := lookupEnvTrimmed("APP_STATIC_API_KEYS"); ok {
		persisted.StaticAPIKeys = parseStaticAPIKeys(value)
	}
	return persisted
}

func normalizePersistedConfig(persisted PersistedConfig) PersistedConfig {
	persisted.Version = persistedConfigVersion
	persisted.BasicAuthUser = defaultIfEmpty(strings.TrimSpace(persisted.BasicAuthUser), "admin")
	persisted.BasicAuthPass = defaultIfEmpty(strings.TrimSpace(persisted.BasicAuthPass), "admin")
	persisted.Timezone = defaultIfEmpty(strings.TrimSpace(persisted.Timezone), "UTC")
	persisted.PublisherMode = normalizeMode(strings.TrimSpace(persisted.PublisherMode))
	persisted.LinkedInToken = strings.TrimSpace(persisted.LinkedInToken)
	persisted.LinkedInAuthorURN = strings.TrimSpace(persisted.LinkedInAuthorURN)
	persisted.LinkedInAPIBase = defaultIfEmpty(strings.TrimSpace(persisted.LinkedInAPIBase), "https://api.linkedin.com")
	persisted.FacebookPageToken = strings.TrimSpace(persisted.FacebookPageToken)
	persisted.FacebookPageID = strings.TrimSpace(persisted.FacebookPageID)
	persisted.FacebookAPIBase = strings.TrimRight(defaultIfEmpty(strings.TrimSpace(persisted.FacebookAPIBase), "https://graph.facebook.com/v22.0"), "/")
	if len(persisted.StaticAPIKeys) == 0 {
		persisted.StaticAPIKeys = nil
	}
	return persisted
}

func lookupEnvTrimmed(key string) (string, bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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

func parseWebhookURLs(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	replacer := strings.NewReplacer("\n", ",", ";", ",")
	parts := strings.Split(replacer.Replace(trimmed), ",")
	urls := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if !strings.HasPrefix(item, "http://") && !strings.HasPrefix(item, "https://") {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		urls = append(urls, item)
	}

	if len(urls) == 0 {
		return nil
	}
	return urls
}
