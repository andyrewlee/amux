package daytona

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SnapshotService manages snapshots.
type SnapshotService struct {
	client *Daytona
}

// List retrieves all snapshots.
func (s *SnapshotService) List() ([]*Snapshot, error) {
	var resp []*Snapshot
	if err := s.client.doJSON(context.Background(), httpMethodGet, "/snapshots", nil, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Get retrieves a snapshot by name or ID.
func (s *SnapshotService) Get(name string) (*Snapshot, error) {
	var resp Snapshot
	if err := s.client.doJSON(context.Background(), httpMethodGet, "/snapshots/"+url.PathEscape(name), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Create creates a snapshot and optionally streams build logs.
func (s *SnapshotService) Create(params CreateSnapshotParams, options *SnapshotCreateOptions) (*Snapshot, error) {
	payload := map[string]any{"name": params.Name}
	switch img := params.Image.(type) {
	case string:
		payload["imageName"] = img
	case *Image:
		payload["buildInfo"] = map[string]any{"dockerfileContent": img.Dockerfile()}
	case nil:
		return nil, fmt.Errorf("image is required")
	default:
		return nil, fmt.Errorf("image must be a string or *Image")
	}

	var created Snapshot
	if err := s.client.doJSON(context.Background(), httpMethodPost, "/snapshots", payload, &created); err != nil {
		return nil, err
	}
	if created.ID == "" {
		return nil, fmt.Errorf("failed to create snapshot")
	}

	terminal := map[string]bool{
		"active":       true,
		"error":        true,
		"build_failed": true,
	}

	if options != nil && options.OnLogs != nil {
		options.OnLogs(fmt.Sprintf("Creating snapshot %s (%s)", created.Name, created.State))
	}

	if options != nil && options.OnLogs != nil && created.State != "pending" && !terminal[created.State] {
		_ = s.streamLogs(created.ID, options.OnLogs, terminal)
	}

	for !terminal[created.State] {
		time.Sleep(1 * time.Second)
		latest, err := s.Get(created.ID)
		if err != nil {
			return nil, err
		}
		created = *latest
	}

	return &created, nil
}

func (s *SnapshotService) streamLogs(id string, onLogs func(string), terminal map[string]bool) error {
	url := fmt.Sprintf("%s/snapshots/%s/build-logs?follow=true", strings.TrimRight(s.client.apiURL, "/"), id)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, vals := range s.client.headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	resp, err := s.client.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to stream logs: %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\n")
		onLogs(line)
		latest, err := s.Get(id)
		if err != nil {
			return err
		}
		if terminal[latest.State] {
			return nil
		}
	}
	return scanner.Err()
}
