package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "dry-run", want: "dry-run"},
		{in: "linkedin", want: "linkedin"},
		{in: "facebook-page", want: "facebook-page"},
		{in: "facebook", want: "facebook-page"},
		{in: "unknown", want: "dry-run"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := normalizeMode(tt.in); got != tt.want {
				t.Fatalf("normalizeMode(%q)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFacebookConfigured(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	if cfg.FacebookConfigured() {
		t.Fatal("expected facebook to be unconfigured")
	}

	cfg.FacebookPageToken = "token"
	if cfg.FacebookConfigured() {
		t.Fatal("expected facebook to be unconfigured without page id")
	}

	cfg.FacebookPageID = "1234"
	if !cfg.FacebookConfigured() {
		t.Fatal("expected facebook to be configured")
	}
}

func TestParseStaticAPIKeys(t *testing.T) {
	t.Parallel()

	parsed := parseStaticAPIKeys("bot-main:lcak_a, lcak_b ; bot-c=lcak_c")
	if len(parsed) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(parsed))
	}
	if got := parsed["lcak_a"]; got != "bot-main" {
		t.Fatalf("expected bot-main for lcak_a, got %q", got)
	}
	if got := parsed["lcak_c"]; got != "bot-c" {
		t.Fatalf("expected bot-c for lcak_c, got %q", got)
	}
	if got := parsed["lcak_b"]; got != "env-key-1" {
		t.Fatalf("expected generated name env-key-1, got %q", got)
	}
}

func TestLoadCreatesConfigFileFromEnv(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.json")
	dbPath := filepath.Join(dataDir, "linkedin-cron.db")

	t.Setenv("APP_DATA_DIR", dataDir)
	t.Setenv("APP_CONFIG_PATH", configPath)
	t.Setenv("APP_DB_PATH", dbPath)
	t.Setenv("APP_BASIC_AUTH_USER", "alice")
	t.Setenv("APP_BASIC_AUTH_PASS", "secret")
	t.Setenv("APP_TIMEZONE", "UTC")
	t.Setenv("APP_STATIC_API_KEYS", "bot:lcak_123")
	t.Setenv("PUBLISHER_MODE", "dry-run")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.BasicAuthUser != "alice" {
		t.Fatalf("expected basic auth user alice, got %q", cfg.BasicAuthUser)
	}
	if cfg.BasicAuthPass != "secret" {
		t.Fatalf("expected basic auth pass secret, got %q", cfg.BasicAuthPass)
	}
	if got := cfg.StaticAPIKeys["lcak_123"]; got != "bot" {
		t.Fatalf("expected static api key bot, got %q", got)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be created: %v", err)
	}
}

func TestLoadReadsPersistedConfig(t *testing.T) {
	dataDir := t.TempDir()
	configPath := filepath.Join(dataDir, "config.json")
	dbPath := filepath.Join(dataDir, "linkedin-cron.db")

	persisted := `{
  "version": 1,
  "basic_auth_user": "stored-user",
  "basic_auth_pass": "stored-pass",
  "timezone": "UTC",
  "publisher_mode": "facebook-page",
  "static_api_keys": {
    "lcak_token": "bot-stored"
  },
  "facebook_page_access_token": "fb-token",
  "facebook_page_id": "12345"
}
`
	if err := os.WriteFile(configPath, []byte(persisted), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	t.Setenv("APP_DATA_DIR", dataDir)
	t.Setenv("APP_CONFIG_PATH", configPath)
	t.Setenv("APP_DB_PATH", dbPath)
	t.Setenv("APP_BASIC_AUTH_USER", "")
	t.Setenv("APP_BASIC_AUTH_PASS", "")
	t.Setenv("APP_STATIC_API_KEYS", "")
	t.Setenv("APP_TIMEZONE", "")
	t.Setenv("PUBLISHER_MODE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.BasicAuthUser != "stored-user" {
		t.Fatalf("expected stored user, got %q", cfg.BasicAuthUser)
	}
	if cfg.BasicAuthPass != "stored-pass" {
		t.Fatalf("expected stored pass, got %q", cfg.BasicAuthPass)
	}
	if cfg.PublisherMode != "facebook-page" {
		t.Fatalf("expected publisher mode facebook-page, got %q", cfg.PublisherMode)
	}
	if got := cfg.StaticAPIKeys["lcak_token"]; got != "bot-stored" {
		t.Fatalf("expected static key name bot-stored, got %q", got)
	}
	if !cfg.FacebookConfigured() {
		t.Fatal("expected facebook config from persisted config")
	}
}
