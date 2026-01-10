package daytona

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// VolumeService manages volumes.
type VolumeService struct {
	client *Daytona
}

// VolumeWaitOptions configures volume wait behavior.
type VolumeWaitOptions struct {
	Timeout  time.Duration
	Interval time.Duration
}

// Get gets a volume by name. If createIfMissing is true, creates if not found.
func (v *VolumeService) Get(name string, createIfMissing bool) (*Volume, error) {
	var resp Volume
	path := "/volumes/by-name/" + url.PathEscape(name)
	if err := v.client.doJSON(context.Background(), httpMethodGet, path, nil, &resp); err != nil {
		if isNotFound(err) && createIfMissing {
			return v.create(name)
		}
		return nil, err
	}
	return &resp, nil
}

// WaitForReady waits until a volume reaches the ready state.
func (v *VolumeService) WaitForReady(name string, options *VolumeWaitOptions) (*Volume, error) {
	timeout := 60 * time.Second
	interval := 1500 * time.Millisecond
	if options != nil {
		if options.Timeout > 0 {
			timeout = options.Timeout
		}
		if options.Interval > 0 {
			interval = options.Interval
		}
	}

	start := time.Now()
	for {
		volume, err := v.Get(name, true)
		if err != nil {
			return nil, err
		}
		if volume.State == "ready" {
			return volume, nil
		}
		if volume.State == "error" || volume.State == "deleted" || volume.State == "deleting" || volume.State == "pending_delete" {
			reason := volume.ErrorReason
			if reason != "" {
				return nil, fmt.Errorf("volume '%s' is in state %s: %s", name, volume.State, reason)
			}
			return nil, fmt.Errorf("volume '%s' is in state %s", name, volume.State)
		}
		if timeout > 0 && time.Since(start) > timeout {
			return nil, fmt.Errorf("volume '%s' not ready after %ds (state: %s)", name, int(timeout.Seconds()), volume.State)
		}
		time.Sleep(interval)
	}
}

func (v *VolumeService) create(name string) (*Volume, error) {
	payload := map[string]any{"name": name}
	var resp Volume
	if err := v.client.doJSON(context.Background(), httpMethodPost, "/volumes", payload, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
