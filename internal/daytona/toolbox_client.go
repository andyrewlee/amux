package daytona

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type toolboxClient struct {
	baseURL    string
	headers    http.Header
	httpClient *http.Client
}

func newToolboxClient(baseURL string, headers http.Header, timeout time.Duration) *toolboxClient {
	client := &http.Client{}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return &toolboxClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		headers:    headers.Clone(),
		httpClient: client,
	}
}

func (c *toolboxClient) urlFor(p string, query url.Values) string {
	u, _ := url.Parse(c.baseURL)
	u.Path = path.Join(u.Path, p)
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (c *toolboxClient) doJSON(ctx context.Context, method, p string, query url.Values, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.urlFor(p, query), bodyReader)
	if err != nil {
		return err
	}
	for k, vals := range c.headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(buf))
		if msg == "" {
			msg = fmt.Sprintf("request failed with status %d", resp.StatusCode)
		}
		return &APIError{StatusCode: resp.StatusCode, Message: msg}
	}

	if out != nil {
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(out); err != nil && err != io.EOF {
			return err
		}
	}
	return nil
}

func (c *toolboxClient) doRequest(ctx context.Context, method, p string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.urlFor(p, query), body)
	if err != nil {
		return nil, err
	}
	for k, vals := range c.headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(resp.Body)
		msg := strings.TrimSpace(string(buf))
		if msg == "" {
			msg = fmt.Sprintf("request failed with status %d", resp.StatusCode)
		}
		resp.Body.Close()
		return nil, &APIError{StatusCode: resp.StatusCode, Message: msg}
	}
	return resp, nil
}

func (c *toolboxClient) uploadMultipart(ctx context.Context, p string, fields map[string]string, files map[string]multipartFile, timeout time.Duration) error {
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		for k, v := range fields {
			_ = writer.WriteField(k, v)
		}
		for field, file := range files {
			part, err := writer.CreateFormFile(field, file.Name)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			if _, err := io.Copy(part, file.Reader); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
		_ = writer.Close()
	}()

	ctxReq, cancel := withTimeout(ctx, timeout)
	defer cancel()
	resp, err := c.doRequest(ctxReq, httpMethodPost, p, nil, pr, writer.FormDataContentType())
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

type multipartFile struct {
	Name   string
	Reader io.Reader
}
