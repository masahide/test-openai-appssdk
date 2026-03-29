package main

import "testing"

func TestLoadConfigUsesCodexLikeDefaults(t *testing.T) {
	t.Setenv("OPENAI_OAUTH_CLIENT_ID", "")
	t.Setenv("OPENAI_OAUTH_AUTHORIZE_URL", "")
	t.Setenv("OPENAI_OAUTH_TOKEN_URL", "")
	t.Setenv("OPENAI_OAUTH_REDIRECT_URL", "")
	t.Setenv("OPENAI_OAUTH_SCOPE", "")
	t.Setenv("OPENAI_OAUTH_ORIGINATOR", "")
	t.Setenv("OPENAI_OAUTH_AUTH_FILE", "")

	cfg := loadConfig()

	if cfg.RedirectURL != "http://localhost:1455/auth/callback" {
		t.Fatalf("RedirectURL = %q", cfg.RedirectURL)
	}
	if cfg.Originator != "codex_cli" {
		t.Fatalf("Originator = %q", cfg.Originator)
	}
}
