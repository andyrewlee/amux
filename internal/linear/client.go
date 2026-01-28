package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultEndpoint = "https://api.linear.app/graphql"

// Client is a minimal GraphQL client for Linear.
type Client struct {
	Endpoint   string
	Token      string
	TokenType  string // optional: "Bearer" for OAuth
	HTTPClient *http.Client
	Actor      string // optional, e.g. "app"
}

// NewClient creates a client with the given token.
func NewClient(token string) *Client {
	return &Client{
		Endpoint:   defaultEndpoint,
		Token:      token,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

type graphQLRequest struct {
	Query     string      `json:"query"`
	Variables interface{} `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

// RateLimitError represents a Linear rate limit response.
type RateLimitError struct {
	Status int
	Body   string
	Reset  time.Time
}

func (e *RateLimitError) Error() string {
	if !e.Reset.IsZero() {
		return fmt.Sprintf("linear: rate limited (status %d), reset at %s", e.Status, e.Reset.Format(time.RFC3339))
	}
	return fmt.Sprintf("linear: rate limited (status %d)", e.Status)
}

func parseRateLimit(resp *http.Response) time.Time {
	if resp == nil {
		return time.Time{}
	}
	reset := resp.Header.Get("X-RateLimit-Reset")
	if reset == "" {
		return time.Time{}
	}
	// Linear returns epoch seconds.
	if sec, err := strconv.ParseInt(reset, 10, 64); err == nil {
		return time.Unix(sec, 0)
	}
	return time.Time{}
}

// Do executes a GraphQL request and unmarshals into out.
func (c *Client) Do(ctx context.Context, query string, variables interface{}, out interface{}) error {
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}

	payload := graphQLRequest{Query: query, Variables: variables}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("linear: marshal request: %w", err)
	}

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if c.Actor != "" {
		u, err := url.Parse(endpoint)
		if err == nil {
			q := u.Query()
			q.Set("actor", c.Actor)
			u.RawQuery = q.Encode()
			endpoint = u.String()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("linear: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if header := c.authHeader(); header != "" {
		req.Header.Set("Authorization", header)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("linear: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("linear: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			return &RateLimitError{Status: resp.StatusCode, Body: string(respBody), Reset: parseRateLimit(resp)}
		}
		return fmt.Errorf("linear: status %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("linear: decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("linear: %s", gqlResp.Errors[0].Message)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(gqlResp.Data, out); err != nil {
		return fmt.Errorf("linear: decode data: %w", err)
	}
	return nil
}

func (c *Client) authHeader() string {
	if c.Token == "" {
		return ""
	}
	if strings.EqualFold(c.TokenType, "bearer") {
		return "Bearer " + c.Token
	}
	return c.Token
}
