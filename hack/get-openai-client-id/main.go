package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.github.com"
	defaultRepo    = "openai/codex"
	defaultQuery   = `repo:openai/codex "pub const CLIENT_ID"`
)

var (
	clientIDPattern    = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?const\s+CLIENT_ID\b[^=]*=\s*"(app_[A-Za-z0-9]+)"`)
	errNoSearchResults = errors.New("no code search results found")
)

type Fetcher struct {
	BaseURL string
	Repo    string
	Query   string
	Client  *http.Client
	Token   string
}

type searchResponse struct {
	Items []struct {
		Path string `json:"path"`
	} `json:"items"`
}

type contentResponse struct {
	Content string `json:"content"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fetcher := &Fetcher{
		BaseURL: defaultBaseURL,
		Repo:    defaultRepo,
		Query:   defaultQuery,
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
	token, err := resolveGitHubToken(githubToken, ghAuthToken)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fetcher.Token = token

	clientID, err := fetcher.FetchClientID(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(clientID)
}

func (f *Fetcher) FetchClientID(ctx context.Context) (string, error) {
	if strings.TrimSpace(f.Token) == "" {
		return "", errors.New("set GITHUB_TOKEN or GH_TOKEN, or authenticate with gh auth login")
	}

	path, err := f.searchPath(ctx)
	if err != nil {
		if errors.Is(err, errNoSearchResults) {
			return "", fmt.Errorf("GitHub code search で CLIENT_ID 定義を見つけられませんでした。検索条件の調整が必要です: %s", f.Query)
		}
		return "", err
	}

	source, err := f.fetchSource(ctx, path)
	if err != nil {
		return "", err
	}

	clientID, err := extractClientID(source)
	if err != nil {
		return "", fmt.Errorf("extract CLIENT_ID from %s: %w", path, err)
	}
	return clientID, nil
}

func (f *Fetcher) searchPath(ctx context.Context) (string, error) {
	values := url.Values{}
	values.Set("q", f.Query)
	values.Set("per_page", "1")

	var resp searchResponse
	if err := f.getJSON(ctx, "/search/code?"+values.Encode(), &resp); err != nil {
		return "", err
	}
	if len(resp.Items) == 0 || strings.TrimSpace(resp.Items[0].Path) == "" {
		return "", errNoSearchResults
	}

	return resp.Items[0].Path, nil
}

func (f *Fetcher) fetchSource(ctx context.Context, path string) (string, error) {
	var resp contentResponse
	endpoint := fmt.Sprintf("/repos/%s/contents/%s", f.Repo, path)
	if err := f.getJSON(ctx, endpoint, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.Content) == "" {
		return "", errors.New("content field is empty")
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(resp.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("decode base64 content: %w", err)
	}
	return string(decoded), nil
}

func (f *Fetcher) getJSON(ctx context.Context, path string, dst any) error {
	baseURL := strings.TrimRight(f.BaseURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "test-openai-appssdk-client-id-fetcher")
	req.Header.Set("Authorization", "Bearer "+f.Token)

	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return err
	}
	return nil
}

func extractClientID(source string) (string, error) {
	match := clientIDPattern.FindStringSubmatch(source)
	if len(match) < 2 {
		return "", errors.New("CLIENT_ID not found")
	}
	return match[1], nil
}

func githubToken() string {
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GH_TOKEN"))
}

func resolveGitHubToken(envLookup func() string, ghLookup func() (string, error)) (string, error) {
	if token := strings.TrimSpace(envLookup()); token != "" {
		return token, nil
	}

	token, err := ghLookup()
	if err == nil && strings.TrimSpace(token) != "" {
		return strings.TrimSpace(token), nil
	}

	return "", errors.New("set GITHUB_TOKEN or GH_TOKEN, or authenticate with gh auth login")
}

func ghAuthToken() (string, error) {
	output, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
