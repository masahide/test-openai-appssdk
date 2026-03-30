package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	defaultTokenURL     = "https://auth.openai.com/oauth/token"
	defaultRedirectURL  = "http://localhost:1455/auth/callback"
	defaultScope        = "openid profile email offline_access"
	defaultOriginator   = "codex_cli"
	defaultCallbackPort = "1455"
)

type OAuthConfig struct {
	ClientID     string
	AuthorizeURL string
	TokenURL     string
	RedirectURL  string
	Scope        string
	Originator   string
	AuthFilePath string
	HTTPClient   *http.Client
}

func loadConfig() OAuthConfig {
	return OAuthConfig{
		ClientID:     envOr("OPENAI_OAUTH_CLIENT_ID", ""),
		AuthorizeURL: envOr("OPENAI_OAUTH_AUTHORIZE_URL", defaultAuthorizeURL),
		TokenURL:     envOr("OPENAI_OAUTH_TOKEN_URL", defaultTokenURL),
		RedirectURL:  envOr("OPENAI_OAUTH_REDIRECT_URL", defaultRedirectURL),
		Scope:        envOr("OPENAI_OAUTH_SCOPE", defaultScope),
		Originator:   envOr("OPENAI_OAUTH_ORIGINATOR", defaultOriginator),
		AuthFilePath: envOr("OPENAI_OAUTH_AUTH_FILE", defaultAuthFilePath()),
		HTTPClient:   http.DefaultClient,
	}
}

func defaultAuthFilePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "auth.json"
	}
	return filepath.Join(configDir, "codex-oauth-pkce", "auth.json")
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
