package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildAuthorizationRequestIncludesPKCEAndOpenAIParams(t *testing.T) {
	client := newOAuthClient(testConfig())

	req, err := client.BuildAuthorizationRequest()
	if err != nil {
		t.Fatalf("BuildAuthorizationRequest: %v", err)
	}
	if req.Verifier == "" {
		t.Fatal("verifier is empty")
	}
	if req.State == "" {
		t.Fatal("state is empty")
	}

	authURL, err := url.Parse(req.URL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	query := authURL.Query()
	assertEqual(t, query.Get("response_type"), "code")
	assertEqual(t, query.Get("client_id"), testConfig().ClientID)
	assertEqual(t, query.Get("redirect_uri"), testConfig().RedirectURL)
	assertEqual(t, query.Get("scope"), testConfig().Scope)
	assertEqual(t, query.Get("code_challenge_method"), "S256")
	assertEqual(t, query.Get("state"), req.State)
	assertEqual(t, query.Get("id_token_add_organizations"), "true")
	assertEqual(t, query.Get("codex_cli_simplified_flow"), "true")
	assertEqual(t, query.Get("originator"), testConfig().Originator)
	if query.Get("code_challenge") == "" {
		t.Fatal("code_challenge is empty")
	}
}

func TestCallbackHandlerRejectsStateMismatch(t *testing.T) {
	server := newCallbackServer("expected-state")

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong&code=test-code", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "State mismatch") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestExchangeAuthorizationCode(t *testing.T) {
	var form url.Values
	cfg := testConfig()
	cfg.TokenURL = "https://auth.openai.example/oauth/token"
	cfg.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/oauth/token" {
				t.Fatalf("path = %q", r.URL.Path)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			form, err = url.ParseQuery(string(body))
			if err != nil {
				t.Fatalf("ParseQuery: %v", err)
			}
			return jsonHTTPResponse(map[string]any{
				"access_token":  "access-token",
				"refresh_token": "refresh-token",
				"expires_in":    3600,
			}), nil
		}),
	}
	client := newOAuthClient(cfg)

	token, err := client.ExchangeAuthorizationCode("auth-code", "pkce-verifier")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode: %v", err)
	}

	assertEqual(t, form.Get("grant_type"), "authorization_code")
	assertEqual(t, form.Get("client_id"), cfg.ClientID)
	assertEqual(t, form.Get("code"), "auth-code")
	assertEqual(t, form.Get("code_verifier"), "pkce-verifier")
	assertEqual(t, form.Get("redirect_uri"), cfg.RedirectURL)
	assertEqual(t, token.AccessToken, "access-token")
	assertEqual(t, token.RefreshToken, "refresh-token")
	if token.ExpiresAt.IsZero() {
		t.Fatal("ExpiresAt is zero")
	}
}

func TestRefreshAccessToken(t *testing.T) {
	var form url.Values
	cfg := testConfig()
	cfg.TokenURL = "https://auth.openai.example/oauth/token"
	cfg.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			form, err = url.ParseQuery(string(body))
			if err != nil {
				t.Fatalf("ParseQuery: %v", err)
			}
			return jsonHTTPResponse(map[string]any{
				"access_token":  "new-access",
				"refresh_token": "new-refresh",
				"expires_in":    7200,
			}), nil
		}),
	}
	client := newOAuthClient(cfg)

	token, err := client.RefreshAccessToken("old-refresh")
	if err != nil {
		t.Fatalf("RefreshAccessToken: %v", err)
	}

	assertEqual(t, form.Get("grant_type"), "refresh_token")
	assertEqual(t, form.Get("refresh_token"), "old-refresh")
	assertEqual(t, form.Get("client_id"), cfg.ClientID)
	assertEqual(t, token.AccessToken, "new-access")
	assertEqual(t, token.RefreshToken, "new-refresh")
}

func TestFileTokenStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := fileTokenStore{Path: filepath.Join(dir, "auth.json")}
	original := tokenSet{
		AccessToken:  "access",
		RefreshToken: "refresh",
		ExpiresAt:    time.Unix(1_700_000_000, 0),
		AccountID:    "acct_123",
	}

	if err := store.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	assertEqual(t, loaded.AccessToken, original.AccessToken)
	assertEqual(t, loaded.RefreshToken, original.RefreshToken)
	assertEqual(t, loaded.AccountID, original.AccountID)
	if !loaded.ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatalf("ExpiresAt = %v, want %v", loaded.ExpiresAt, original.ExpiresAt)
	}
}

func TestDecodeAccountID(t *testing.T) {
	token := jwtWithPayload(`{"https://api.openai.com/auth":{"chatgpt_account_id":"acct_123"}}`)
	accountID := getAccountID(token)
	assertEqual(t, accountID, "acct_123")
}

func TestParseAuthorizationInputAcceptsRawCode(t *testing.T) {
	parsed, err := parseAuthorizationInput("plain-code")
	if err != nil {
		t.Fatalf("parseAuthorizationInput: %v", err)
	}
	assertEqual(t, parsed.Code, "plain-code")
	assertEqual(t, parsed.State, "")
}

func TestParseAuthorizationInputAcceptsRedirectURL(t *testing.T) {
	parsed, err := parseAuthorizationInput("http://127.0.0.1:1455/auth/callback?code=test-code&state=test-state")
	if err != nil {
		t.Fatalf("parseAuthorizationInput: %v", err)
	}
	assertEqual(t, parsed.Code, "test-code")
	assertEqual(t, parsed.State, "test-state")
}

func TestResolveAuthorizationCodeFallsBackToManualInputWhenBrowserLaunchFails(t *testing.T) {
	code, err := resolveAuthorizationCode(
		context.Background(),
		"http://127.0.0.1:1455/auth/callback",
		"expected-state",
		"https://auth.openai.example/authorize",
		func(string) error { return errors.New("open browser failed") },
		func(context.Context, string, string) (string, error) { return "", errors.New("callback not started") },
		func(string) (string, error) {
			return "http://127.0.0.1:1455/auth/callback?code=manual-code&state=expected-state", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveAuthorizationCode: %v", err)
	}
	assertEqual(t, code, "manual-code")
}

func TestResolveAuthorizationCodeRejectsManualInputStateMismatch(t *testing.T) {
	_, err := resolveAuthorizationCode(
		context.Background(),
		"http://127.0.0.1:1455/auth/callback",
		"expected-state",
		"https://auth.openai.example/authorize",
		func(string) error { return errors.New("open browser failed") },
		func(context.Context, string, string) (string, error) { return "", errors.New("callback not started") },
		func(string) (string, error) {
			return "http://127.0.0.1:1455/auth/callback?code=manual-code&state=wrong-state", nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "state mismatch") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveAuthorizationCodeStillAcceptsCallbackWhenBrowserLaunchFails(t *testing.T) {
	code, err := resolveAuthorizationCode(
		context.Background(),
		"http://localhost:1455/auth/callback",
		"expected-state",
		"https://auth.openai.example/authorize",
		func(string) error { return errors.New("open browser failed") },
		func(context.Context, string, string) (string, error) { return "callback-code", nil },
		func(string) (string, error) {
			select {}
		},
	)
	if err != nil {
		t.Fatalf("resolveAuthorizationCode: %v", err)
	}
	assertEqual(t, code, "callback-code")
}

func testConfig() OAuthConfig {
	return OAuthConfig{
		ClientID:     "app_test_client",
		AuthorizeURL: "https://auth.openai.com/oauth/authorize",
		TokenURL:     "https://auth.openai.com/oauth/token",
		RedirectURL:  "http://127.0.0.1:1455/auth/callback",
		Scope:        "openid profile email offline_access",
		Originator:   "codex-oauth-pkce-test",
		HTTPClient:   http.DefaultClient,
	}
}

func jwtWithPayload(payload string) string {
	header := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0"
	body := base64URLEncode([]byte(payload))
	return header + "." + body + "."
}

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func jsonHTTPResponse(body any) *http.Response {
	payload, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(payload)),
	}
}
