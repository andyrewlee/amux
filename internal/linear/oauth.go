package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	oauthAuthorizeURL = "https://linear.app/oauth/authorize"
	oauthTokenURL     = "https://api.linear.app/oauth/token"
	defaultOAuthScope = "read write"
)

// OAuthToken represents an OAuth token response.
type OAuthToken struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Scope        string
	ExpiresIn    int
	ExpiresAt    time.Time
}

// BuildOAuthURL builds the Linear OAuth authorization URL.
func BuildOAuthURL(cfg OAuthConfig, state string) (string, error) {
	if cfg.ClientID == "" || cfg.RedirectURI == "" {
		return "", fmt.Errorf("linear: oauth client id and redirect uri required")
	}
	u, err := url.Parse(oauthAuthorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", cfg.RedirectURI)
	q.Set("response_type", "code")
	if state != "" {
		q.Set("state", state)
	}
	q.Set("scope", defaultOAuthScope)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ExchangeOAuthCode exchanges an authorization code for an access token.
func ExchangeOAuthCode(ctx context.Context, cfg OAuthConfig, code string) (OAuthToken, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURI == "" {
		return OAuthToken{}, fmt.Errorf("linear: oauth config missing")
	}
	if code == "" {
		return OAuthToken{}, fmt.Errorf("linear: oauth code missing")
	}

	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("redirect_uri", cfg.RedirectURI)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return OAuthToken{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return OAuthToken{}, fmt.Errorf("linear: oauth token status %d", resp.StatusCode)
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return OAuthToken{}, err
	}
	if payload.AccessToken == "" {
		return OAuthToken{}, fmt.Errorf("linear: oauth token missing access token")
	}
	token := OAuthToken{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
		Scope:        payload.Scope,
		ExpiresIn:    payload.ExpiresIn,
	}
	if token.ExpiresIn > 0 {
		token.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return token, nil
}
