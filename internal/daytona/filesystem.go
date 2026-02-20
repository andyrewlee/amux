package daytona

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileSystem provides file operations in a sandbox.
type FileSystem struct {
	toolbox func() (*toolboxClient, error)
}

type fileUpload struct {
	source      any
	destination string
}

type fileDownloadRequest struct {
	source      string
	destination string
}

type fileDownloadResponse struct {
	source string
	path   string
	data   []byte
	err    string
}

// UploadFile uploads a single file or buffer to the sandbox.
func (fs *FileSystem) UploadFile(src any, remotePath string, timeout time.Duration) error {
	return fs.uploadFiles([]fileUpload{{source: src, destination: remotePath}}, timeout)
}

// DownloadFile downloads a file from the sandbox and returns its bytes.
func (fs *FileSystem) DownloadFile(remotePath string, timeout time.Duration) ([]byte, error) {
	results, err := fs.downloadFiles([]fileDownloadRequest{{source: remotePath}}, timeout)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, errors.New("no data received for this file")
	}
	if results[0].err != "" {
		return nil, errors.New(results[0].err)
	}
	return results[0].data, nil
}

// DownloadFileTo downloads a file from the sandbox to a local path.
func (fs *FileSystem) DownloadFileTo(remotePath, localPath string, timeout time.Duration) error {
	results, err := fs.downloadFiles([]fileDownloadRequest{{source: remotePath, destination: localPath}}, timeout)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return errors.New("no data received for this file")
	}
	if results[0].err != "" {
		return errors.New(results[0].err)
	}
	return nil
}

func (fs *FileSystem) downloadFiles(files []fileDownloadRequest, timeout time.Duration) ([]fileDownloadResponse, error) {
	if len(files) == 0 {
		return nil, nil
	}

	client, err := fs.toolbox()
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(files))
	meta := make(map[string]fileDownloadResponse)
	for _, f := range files {
		paths = append(paths, f.source)
		meta[f.source] = fileDownloadResponse{source: f.source, path: f.destination}
	}

	payload, _ := json.Marshal(map[string]any{"paths": paths})
	ctx, cancel := withTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := client.doRequest(ctx, httpMethodPost, "/files/bulk-download", nil, bytes.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return nil, fmt.Errorf("unexpected Content-Type: %s", contentType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errors.New("missing multipart boundary")
	}

	reader := multipart.NewReader(resp.Body, boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		source := part.FileName()
		entry, ok := meta[source]
		if !ok && len(meta) == 1 {
			for key, val := range meta {
				source = key
				entry = val
				break
			}
		}
		switch part.FormName() {
		case "error":
			buf, _ := io.ReadAll(part)
			entry.err = strings.TrimSpace(string(buf))
		case "file":
			if entry.path != "" {
				if err := os.MkdirAll(filepath.Dir(entry.path), 0o755); err != nil {
					entry.err = err.Error()
					break
				}
				out, err := os.Create(entry.path)
				if err != nil {
					entry.err = err.Error()
					break
				}
				_, err = io.Copy(out, part)
				_ = out.Close()
				if err != nil {
					entry.err = err.Error()
				}
			} else {
				buf, _ := io.ReadAll(part)
				entry.data = buf
			}
		}
		meta[source] = entry
	}

	results := make([]fileDownloadResponse, 0, len(files))
	for _, f := range files {
		entry := meta[f.source]
		if entry.err == "" && entry.path == "" && entry.data == nil {
			entry.err = "No data received for this file"
		}
		results = append(results, entry)
	}
	return results, nil
}

func (fs *FileSystem) uploadFiles(files []fileUpload, timeout time.Duration) error {
	if len(files) == 0 {
		return nil
	}
	client, err := fs.toolbox()
	if err != nil {
		return err
	}

	fields := map[string]string{}
	fileFields := map[string]multipartFile{}
	for i, f := range files {
		fields[fmt.Sprintf("files[%d].path", i)] = f.destination
		var reader io.Reader
		var name string
		switch v := f.source.(type) {
		case []byte:
			reader = bytes.NewReader(v)
			name = filepath.Base(f.destination)
		case string:
			file, err := os.Open(v)
			if err != nil {
				return err
			}
			defer file.Close()
			reader = file
			name = filepath.Base(f.destination)
		default:
			return errors.New("unsupported source type")
		}
		fileFields[fmt.Sprintf("files[%d].file", i)] = multipartFile{Name: name, Reader: reader}
	}

	return client.uploadMultipart(context.Background(), "/files/bulk-upload", fields, fileFields, timeout)
}
