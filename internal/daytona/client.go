package daytona

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const defaultAPIURL = "https://app.daytona.io/api"

// Daytona is the main client for interacting with the API.
type Daytona struct {
	apiKey string
	apiURL string
	target string

	headers http.Header
	client  *http.Client

	proxyToolboxOnce sync.Once
	proxyToolboxURL  string
	proxyToolboxErr  error

	Volume   *VolumeService
	Snapshot *SnapshotService
}

// NewDaytona creates a new client.
func NewDaytona(cfg *DaytonaConfig) (*Daytona, error) {
	if cfg == nil || cfg.APIKey == "" {
		return nil, errors.New("API key is required")
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	client := &Daytona{
		apiKey: cfg.APIKey,
		apiURL: strings.TrimRight(apiURL, "/"),
		target: cfg.Target,
		client: &http.Client{Timeout: 24 * time.Hour},
	}

	headers := make(http.Header)
	headers.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.APIKey))
	headers.Set("X-Daytona-Source", "amux")
	client.headers = headers

	client.Volume = &VolumeService{client: client}
	client.Snapshot = &SnapshotService{client: client}
	return client, nil
}

func (d *Daytona) getProxyToolboxURL(ctx context.Context) (string, error) {
	d.proxyToolboxOnce.Do(func() {
		var resp struct {
			ProxyToolboxURL string `json:"proxyToolboxUrl"`
		}
		if err := d.doJSON(ctx, http.MethodGet, "/config", nil, &resp); err != nil {
			d.proxyToolboxErr = err
			return
		}
		if resp.ProxyToolboxURL == "" {
			d.proxyToolboxErr = errors.New("proxy toolbox URL not available")
			return
		}
		d.proxyToolboxURL = strings.TrimRight(resp.ProxyToolboxURL, "/")
	})
	if d.proxyToolboxErr != nil {
		return "", d.proxyToolboxErr
	}
	return d.proxyToolboxURL, nil
}

func (d *Daytona) endpoint(path string) string {
	return d.apiURL + path
}

func (d *Daytona) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	var body *bytes.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	} else {
		body = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, d.endpoint(path), body)
	if err != nil {
		return err
	}
	for k, vals := range d.headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if out != nil {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseAPIError(resp)
	}
	if out != nil {
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(out); err != nil {
			return err
		}
	}
	return nil
}

func (d *Daytona) doRequest(ctx context.Context, method, path string, query url.Values, payload any, out any) error {
	full := d.endpoint(path)
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	var body *bytes.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	} else {
		body = bytes.NewReader(nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, full, body)
	if err != nil {
		return err
	}
	for k, vals := range d.headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if out != nil {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return parseAPIError(resp)
	}
	if out != nil {
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(out); err != nil {
			return err
		}
	}
	return nil
}

// Create creates a sandbox.
func (d *Daytona) Create(params *CreateSandboxParams, opts *CreateOptions) (*Sandbox, error) {
	if params == nil {
		params = &CreateSandboxParams{Language: "python"}
	}
	labels := map[string]string{}
	for k, v := range params.Labels {
		labels[k] = v
	}
	if params.Language != "" {
		labels["code-toolbox-language"] = params.Language
	}

	payload := map[string]any{}
	if params.Snapshot != "" {
		payload["snapshot"] = params.Snapshot
	}
	if len(params.EnvVars) > 0 {
		payload["env"] = params.EnvVars
	}
	if len(labels) > 0 {
		payload["labels"] = labels
	}
	if params.AutoStopInterval != 0 {
		payload["autoStopInterval"] = params.AutoStopInterval
	}
	if len(params.Volumes) > 0 {
		payload["volumes"] = params.Volumes
	}
	if d.target != "" {
		payload["target"] = d.target
	}

	ctx := context.Background()
	if opts != nil && opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	var dto sandboxDTO
	if err := d.doRequest(ctx, http.MethodPost, "/sandbox", nil, payload, &dto); err != nil {
		return nil, err
	}
	return newSandboxFromDTO(&dto, d), nil
}

// Get retrieves a sandbox by ID.
func (d *Daytona) Get(id string) (*Sandbox, error) {
	var dto sandboxDTO
	if err := d.doJSON(context.Background(), http.MethodGet, "/sandbox/"+url.PathEscape(id), nil, &dto); err != nil {
		return nil, err
	}
	return newSandboxFromDTO(&dto, d), nil
}

// List returns all sandboxes.
func (d *Daytona) List() ([]*Sandbox, error) {
	var items []sandboxDTO
	if err := d.doJSON(context.Background(), http.MethodGet, "/sandbox", nil, &items); err != nil {
		return nil, err
	}
	out := make([]*Sandbox, 0, len(items))
	for i := range items {
		out = append(out, newSandboxFromDTO(&items[i], d))
	}
	return out, nil
}

// Delete deletes a sandbox.
func (d *Daytona) Delete(sandbox *Sandbox) error {
	if sandbox == nil {
		return errors.New("sandbox is required")
	}
	return d.doJSON(context.Background(), http.MethodDelete, "/sandbox/"+url.PathEscape(sandbox.ID), nil, nil)
}

// Stop stops a sandbox.
func (d *Daytona) Stop(sandbox *Sandbox, timeout time.Duration) error {
	if sandbox == nil {
		return errors.New("sandbox is required")
	}
	return sandbox.Stop(timeout)
}

func parseAPIError(resp *http.Response) error {
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	msg := strings.TrimSpace(buf.String())
	if msg == "" {
		msg = resp.Status
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

// APIError represents a non-2xx response.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("api error (%d): %s", e.StatusCode, e.Message)
}

func isNotFound(err error) bool {
	apiErr, ok := err.(*APIError)
	return ok && apiErr.StatusCode == http.StatusNotFound
}
