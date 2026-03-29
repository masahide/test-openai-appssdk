package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
)

func TestExtractClientIDFromSource(t *testing.T) {
	source := `
pub const CLIENT_ID: &str = "app_EMoamEEZ73f0CkXaXp7hrann";
`

	clientID, err := extractClientID(source)
	if err != nil {
		t.Fatalf("extractClientID: %v", err)
	}
	if clientID != "app_EMoamEEZ73f0CkXaXp7hrann" {
		t.Fatalf("clientID = %q", clientID)
	}
}

func TestExtractClientIDIgnoresOtherAppIdentifiers(t *testing.T) {
	source := `
let app_server = "not-a-client-id";
pub const CLIENT_ID: &str = "app_EMoamEEZ73f0CkXaXp7hrann";
`

	clientID, err := extractClientID(source)
	if err != nil {
		t.Fatalf("extractClientID: %v", err)
	}
	if clientID != "app_EMoamEEZ73f0CkXaXp7hrann" {
		t.Fatalf("clientID = %q", clientID)
	}
}

func TestFetchClientID(t *testing.T) {
	source := `pub const CLIENT_ID: &str = "app_EMoamEEZ73f0CkXaXp7hrann";`
	content := base64.StdEncoding.EncodeToString([]byte(source))

	fetcher := &Fetcher{
		BaseURL: "https://api.github.com",
		Repo:    "openai/codex",
		Query:   `repo:openai/codex "pub const CLIENT_ID"`,
		Token:   "test-token",
		Client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
					t.Fatalf("Authorization = %q", got)
				}
				switch req.URL.Path {
				case "/search/code":
					return jsonResponse(`{"items":[{"path":"codex-rs/login/src/auth/manager.rs"}]}`), nil
				case "/repos/openai/codex/contents/codex-rs/login/src/auth/manager.rs":
					return jsonResponse(`{"content":"` + content + `"}`), nil
				default:
					t.Fatalf("unexpected path: %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	clientID, err := fetcher.FetchClientID(context.Background())
	if err != nil {
		t.Fatalf("FetchClientID: %v", err)
	}
	if clientID != "app_EMoamEEZ73f0CkXaXp7hrann" {
		t.Fatalf("clientID = %q", clientID)
	}
}

func TestFetchClientIDRequiresGitHubToken(t *testing.T) {
	fetcher := &Fetcher{
		BaseURL: "https://api.github.com",
		Repo:    "openai/codex",
		Query:   `repo:openai/codex "pub const CLIENT_ID"`,
		Client:  &http.Client{},
	}

	_, err := fetcher.FetchClientID(context.Background())
	if err == nil || err.Error() != "set GITHUB_TOKEN or GH_TOKEN, or authenticate with gh auth login" {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveGitHubTokenFallsBackToGhAuthToken(t *testing.T) {
	token, err := resolveGitHubToken(func() string { return "" }, func() (string, error) {
		return "gh-token\n", nil
	})
	if err != nil {
		t.Fatalf("resolveGitHubToken: %v", err)
	}
	if token != "gh-token" {
		t.Fatalf("token = %q", token)
	}
}

func TestFetchClientIDShowsHelpfulJapaneseErrorWhenSearchMisses(t *testing.T) {
	fetcher := &Fetcher{
		BaseURL: "https://api.github.com",
		Repo:    "openai/codex",
		Query:   `repo:openai/codex "pub const CLIENT_ID"`,
		Token:   "test-token",
		Client: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(`{"items":[]}`), nil
			}),
		},
	}

	_, err := fetcher.FetchClientID(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	want := `GitHub code search で CLIENT_ID 定義を見つけられませんでした。検索条件の調整が必要です: repo:openai/codex "pub const CLIENT_ID"`
	if err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
