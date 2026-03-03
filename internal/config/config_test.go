package config

import "testing"

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
