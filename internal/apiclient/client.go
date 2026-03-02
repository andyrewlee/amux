// Package apiclient provides a Go HTTP/WebSocket client for the Medusa server API.
// Used by the TUI to operate in client mode, connecting to a remote Medusa server.
package apiclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client connects to a Medusa server.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates an API client.
func New(baseURL, token string) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasPrefix(baseURL, "http") {
		baseURL = "http://" + baseURL
	}
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// request performs an authenticated JSON API request.
func (c *Client) request(method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		if errResp.Error != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
		return fmt.Errorf("API error: %s", resp.Status)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// Health checks server connectivity.
func (c *Client) Health() error {
	var result map[string]string
	return c.request("GET", "/api/v1/health", nil, &result)
}

// --- Projects ---

// ProjectResponse mirrors the server's project response.
type ProjectResponse struct {
	Name       string              `json:"name"`
	Path       string              `json:"path"`
	Profile    string              `json:"profile"`
	Workspaces []WorkspaceResponse `json:"workspaces"`
}

// WorkspaceResponse mirrors the server's workspace response.
type WorkspaceResponse struct {
	Name            string `json:"name"`
	Branch          string `json:"branch"`
	Base            string `json:"base"`
	Repo            string `json:"repo"`
	Root            string `json:"root"`
	Profile         string `json:"profile"`
	Archived        bool   `json:"archived"`
	AllowEdits      bool   `json:"allow_edits"`
	Isolated        bool   `json:"isolated"`
	SkipPermissions bool   `json:"skip_permissions"`
}

func (c *Client) ListProjects() ([]ProjectResponse, error) {
	var projects []ProjectResponse
	err := c.request("GET", "/api/v1/projects", nil, &projects)
	return projects, err
}

func (c *Client) AddProject(path string) error {
	return c.request("POST", "/api/v1/projects", map[string]string{"path": path}, nil)
}

func (c *Client) RemoveProject(path string) error {
	encoded := url.PathEscape(path[1:]) // strip leading /
	return c.request("DELETE", "/api/v1/projects/"+encoded, nil, nil)
}

func (c *Client) SetProjectProfile(path, profile string) error {
	encoded := url.PathEscape(path[1:])
	return c.request("PUT", "/api/v1/projects/"+encoded+"/profile", map[string]string{"profile": profile}, nil)
}

func (c *Client) RescanWorkspaces() error {
	return c.request("POST", "/api/v1/projects/rescan", nil, nil)
}

// --- Workspaces ---

func (c *Client) GetWorkspace(wsID string) (*WorkspaceResponse, error) {
	var ws WorkspaceResponse
	err := c.request("GET", "/api/v1/workspaces/"+wsID, nil, &ws)
	return &ws, err
}

type CreateWorkspaceRequest struct {
	ProjectPath  string `json:"project_path"`
	Name         string `json:"name"`
	BranchMode   string `json:"branch_mode,omitempty"`
	CustomBranch string `json:"custom_branch,omitempty"`
}

func (c *Client) CreateWorkspace(req CreateWorkspaceRequest) (*WorkspaceResponse, error) {
	var ws WorkspaceResponse
	err := c.request("POST", "/api/v1/workspaces", req, &ws)
	return &ws, err
}

func (c *Client) DeleteWorkspace(wsID string) error {
	return c.request("DELETE", "/api/v1/workspaces/"+wsID, nil, nil)
}

func (c *Client) RenameWorkspace(wsID, name string) error {
	return c.request("PUT", "/api/v1/workspaces/"+wsID+"/name", map[string]string{"name": name}, nil)
}

// --- Tabs ---

// TabResponse mirrors the server's tab info response.
type TabResponse struct {
	ID           string  `json:"id"`
	WorkspaceID  string  `json:"workspace_id"`
	Kind         string  `json:"kind"`
	Assistant    string  `json:"assistant"`
	State        string  `json:"state"`
	SessionID    string  `json:"session_id"`
	CreatedAt    string  `json:"created_at"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	Model        string  `json:"model"`
	TurnCount    int     `json:"turn_count"`
}

func (c *Client) ListTabs(wsID string) ([]TabResponse, error) {
	var tabs []TabResponse
	err := c.request("GET", "/api/v1/workspaces/"+wsID+"/tabs", nil, &tabs)
	return tabs, err
}

type LaunchTabRequest struct {
	Assistant       string   `json:"assistant,omitempty"`
	Prompt          string   `json:"prompt,omitempty"`
	SkipPermissions bool     `json:"skip_permissions,omitempty"`
	AllowedTools    []string `json:"allowed_tools,omitempty"`
}

func (c *Client) LaunchTab(wsID string, req LaunchTabRequest) (string, error) {
	var result struct {
		TabID string `json:"tab_id"`
	}
	err := c.request("POST", "/api/v1/workspaces/"+wsID+"/tabs", req, &result)
	return result.TabID, err
}

func (c *Client) CloseTab(tabID string) error {
	return c.request("DELETE", "/api/v1/tabs/"+tabID, nil, nil)
}

func (c *Client) ResumeTab(tabID string) error {
	return c.request("POST", "/api/v1/tabs/"+tabID+"/resume", nil, nil)
}

func (c *Client) InterruptTab(tabID string) error {
	return c.request("POST", "/api/v1/tabs/"+tabID+"/interrupt", nil, nil)
}

func (c *Client) SendPrompt(tabID, text string) error {
	return c.request("POST", "/api/v1/tabs/"+tabID+"/prompt", map[string]string{"text": text}, nil)
}

// SDKMessageResponse mirrors the server's SDKMessage.
type SDKMessageResponse struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	UUID      string          `json:"uuid,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
	Result    string          `json:"result,omitempty"`
	TotalCost float64         `json:"total_cost_usd,omitempty"`
}

func (c *Client) GetTabHistory(tabID string, since string) ([]SDKMessageResponse, error) {
	path := "/api/v1/tabs/" + tabID + "/history"
	if since != "" {
		path += "?since=" + url.QueryEscape(since)
	}
	var msgs []SDKMessageResponse
	err := c.request("GET", path, nil, &msgs)
	return msgs, err
}

func (c *Client) GetTabState(tabID string) (*TabResponse, error) {
	var tab TabResponse
	err := c.request("GET", "/api/v1/tabs/"+tabID+"/state", nil, &tab)
	return &tab, err
}

// --- Config ---

func (c *Client) ListProfiles() ([]string, error) {
	var profiles []string
	err := c.request("GET", "/api/v1/profiles", nil, &profiles)
	return profiles, err
}

func (c *Client) CreateProfile(name string) error {
	return c.request("POST", "/api/v1/profiles", map[string]string{"name": name}, nil)
}

func (c *Client) DeleteProfile(name string) error {
	return c.request("DELETE", "/api/v1/profiles/"+name, nil, nil)
}

// --- WebSocket URLs ---

func (c *Client) TabWSURL(tabID string) string {
	wsURL := strings.Replace(c.BaseURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	return wsURL + "/api/v1/tabs/" + tabID + "/ws?token=" + c.Token
}

func (c *Client) TabPTYWSURL(tabID string) string {
	wsURL := strings.Replace(c.BaseURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	return wsURL + "/api/v1/tabs/" + tabID + "/pty?token=" + c.Token
}

func (c *Client) SSEURL() string {
	return c.BaseURL + "/api/v1/events?token=" + c.Token
}
