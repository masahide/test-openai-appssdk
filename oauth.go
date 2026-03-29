package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const chatGPTAccountClaim = "https://api.openai.com/auth"

type OAuthClient struct {
	config OAuthConfig
}

type AuthorizationRequest struct {
	URL      string
	Verifier string
	State    string
}

type tokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	AccountID    string    `json:"account_id,omitempty"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type authorizationInput struct {
	Code  string
	State string
}

type browserOpener func(string) error
type callbackWaiter func(context.Context, string, string) (string, error)
type codePrompter func(string) (string, error)

func newOAuthClient(config OAuthConfig) *OAuthClient {
	return &OAuthClient{config: config}
}

func (c *OAuthClient) BuildAuthorizationRequest() (AuthorizationRequest, error) {
	if strings.TrimSpace(c.config.ClientID) == "" {
		return AuthorizationRequest{}, errors.New("OPENAI_OAUTH_CLIENT_ID is required")
	}

	verifier, err := randomBase64URL(32)
	if err != nil {
		return AuthorizationRequest{}, err
	}
	state, err := randomBase64URL(24)
	if err != nil {
		return AuthorizationRequest{}, err
	}

	sum := sha256.Sum256([]byte(verifier))
	challenge := base64URLEncode(sum[:])

	authURL, err := url.Parse(c.config.AuthorizeURL)
	if err != nil {
		return AuthorizationRequest{}, err
	}
	query := authURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", c.config.ClientID)
	query.Set("redirect_uri", c.config.RedirectURL)
	query.Set("scope", c.config.Scope)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	query.Set("id_token_add_organizations", "true")
	query.Set("codex_cli_simplified_flow", "true")
	query.Set("originator", c.config.Originator)
	authURL.RawQuery = query.Encode()

	return AuthorizationRequest{
		URL:      authURL.String(),
		Verifier: verifier,
		State:    state,
	}, nil
}

func (c *OAuthClient) ExchangeAuthorizationCode(code, verifier string) (tokenSet, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {c.config.ClientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {c.config.RedirectURL},
	}
	return c.exchangeForm(form)
}

func (c *OAuthClient) RefreshAccessToken(refreshToken string) (tokenSet, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {c.config.ClientID},
	}
	return c.exchangeForm(form)
}

func (c *OAuthClient) exchangeForm(form url.Values) (tokenSet, error) {
	req, err := http.NewRequest(http.MethodPost, c.config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenSet{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.config.HTTPClient.Do(req)
	if err != nil {
		return tokenSet{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return tokenSet{}, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return tokenSet{}, err
	}
	if payload.AccessToken == "" || payload.RefreshToken == "" || payload.ExpiresIn <= 0 {
		return tokenSet{}, errors.New("token response missing required fields")
	}

	return tokenSet{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second),
		AccountID:    getAccountID(payload.AccessToken),
	}, nil
}

type callbackResult struct {
	Code string
	Err  error
}

type callbackServer struct {
	expectedState string
	resultCh      chan callbackResult
}

func newCallbackServer(expectedState string) *callbackServer {
	return &callbackServer{
		expectedState: expectedState,
		resultCh:      make(chan callbackResult, 1),
	}
}

func (s *callbackServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u := r.URL
	if u.Path != "/auth/callback" {
		http.Error(w, "Callback route not found.", http.StatusNotFound)
		return
	}
	if u.Query().Get("state") != s.expectedState {
		http.Error(w, "State mismatch.", http.StatusBadRequest)
		s.trySend(callbackResult{Err: errors.New("state mismatch")})
		return
	}
	code := u.Query().Get("code")
	if code == "" {
		http.Error(w, "Missing authorization code.", http.StatusBadRequest)
		s.trySend(callbackResult{Err: errors.New("missing authorization code")})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, "OpenAI authentication completed. You can close this window.")
	s.trySend(callbackResult{Code: code})
}

func (s *callbackServer) trySend(result callbackResult) {
	select {
	case s.resultCh <- result:
	default:
	}
}

func (s *callbackServer) Wait(ctx context.Context) (string, error) {
	select {
	case result := <-s.resultCh:
		return result.Code, result.Err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func listenForCallback(ctx context.Context, redirectURL, state string) (string, error) {
	callbackURL, err := url.Parse(redirectURL)
	if err != nil {
		return "", err
	}
	server := newCallbackServer(state)
	httpServer := &http.Server{Handler: server}
	listener, err := net.Listen("tcp", callbackURL.Host)
	if err != nil {
		return "", err
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	go func() {
		_ = httpServer.Serve(listener)
	}()

	code, waitErr := server.Wait(ctx)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	return code, waitErr
}

func resolveAuthorizationCode(
	ctx context.Context,
	redirectURL, expectedState, authURL string,
	open browserOpener,
	wait callbackWaiter,
	prompt codePrompter,
) (string, error) {
	if wait == nil {
		wait = listenForCallback
	}

	browserErr := error(nil)
	if open != nil {
		browserErr = open(authURL)
	}

	if browserErr == nil {
		code, err := wait(ctx, redirectURL, expectedState)
		if err == nil {
			return code, nil
		}
		if prompt == nil {
			return "", err
		}
	}

	if prompt == nil && browserErr != nil {
		code, err := wait(ctx, redirectURL, expectedState)
		if err == nil {
			return code, nil
		}
		return "", browserErr
	}

	if browserErr != nil {
		return resolveFromCallbackOrPrompt(ctx, redirectURL, expectedState, wait, prompt, browserErr)
	}

	input, err := prompt("Paste the authorization code (or full redirect URL):")
	if err != nil {
		return "", err
	}

	return codeFromPromptInput(input, expectedState)
}

func resolveFromCallbackOrPrompt(
	ctx context.Context,
	redirectURL, expectedState string,
	wait callbackWaiter,
	prompt codePrompter,
	browserErr error,
) (string, error) {
	type result struct {
		code string
		err  error
	}

	results := make(chan result, 2)

	go func() {
		code, err := wait(ctx, redirectURL, expectedState)
		results <- result{code: code, err: err}
	}()

	if prompt != nil {
		go func() {
			input, err := prompt("Paste the authorization code (or full redirect URL):")
			if err != nil {
				results <- result{err: fmt.Errorf("browser open failed: %w; manual input failed: %v", browserErr, err)}
				return
			}

			code, err := codeFromPromptInput(input, expectedState)
			results <- result{code: code, err: err}
		}()
	}

	var firstErr error
	expectedResults := 1
	if prompt != nil {
		expectedResults = 2
	}

	for range expectedResults {
		result := <-results
		if result.err == nil {
			return result.code, nil
		}
		if firstErr == nil {
			firstErr = result.err
		}
	}

	if firstErr != nil {
		return "", firstErr
	}
	return "", browserErr
}

func codeFromPromptInput(input, expectedState string) (string, error) {
	parsed, err := parseAuthorizationInput(input)
	if err != nil {
		return "", err
	}
	if parsed.State != "" && parsed.State != expectedState {
		return "", errors.New("state mismatch")
	}
	return parsed.Code, nil
}

func parseAuthorizationInput(input string) (authorizationInput, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return authorizationInput{}, errors.New("authorization input is empty")
	}

	parsedURL, err := url.Parse(trimmed)
	if err == nil && parsedURL.Scheme != "" && parsedURL.Host != "" {
		code := parsedURL.Query().Get("code")
		if code == "" {
			return authorizationInput{}, errors.New("missing authorization code")
		}
		return authorizationInput{
			Code:  code,
			State: parsedURL.Query().Get("state"),
		}, nil
	}

	return authorizationInput{Code: trimmed}, nil
}

func openBrowser(targetURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", targetURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Start()
}

func randomBase64URL(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64URLEncode(buf), nil
}

func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func getAccountID(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return ""
	}
	auth, ok := decoded[chatGPTAccountClaim].(map[string]any)
	if !ok {
		return ""
	}
	accountID, _ := auth["chatgpt_account_id"].(string)
	return accountID
}
