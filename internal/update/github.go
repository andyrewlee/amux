package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const (
	// GitHubOwner is the GitHub owner for amux.
	GitHubOwner = "andyrewlee"
	// GitHubRepo is the GitHub repo for amux.
	GitHubRepo = "amux"
	// GitHubAPIBase is the base URL for GitHub API.
	GitHubAPIBase = "https://api.github.com"
)

// Release represents a GitHub release.
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

// Asset represents a release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
}

// GitHubClient handles GitHub API interactions.
type GitHubClient struct {
	httpClient *http.Client
	owner      string
	repo       string
}

// NewGitHubClient creates a new GitHub client.
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		owner: GitHubOwner,
		repo:  GitHubRepo,
	}
}

// FetchLatestRelease fetches the latest release from GitHub.
func (c *GitHubClient) FetchLatestRelease() (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", GitHubAPIBase, c.owner, c.repo)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "amux-updater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &release, nil
}

// DownloadAsset downloads an asset to the specified writer.
func (c *GitHubClient) DownloadAsset(url string, w io.Writer) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "amux-updater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return fmt.Errorf("writing: %w", err)
	}

	return nil
}

// FetchChecksums downloads and returns the checksums.txt content.
func (c *GitHubClient) FetchChecksums(release *Release) (map[string]string, error) {
	var checksumURL string
	for _, asset := range release.Assets {
		if asset.Name == "checksums.txt" {
			checksumURL = asset.BrowserDownloadURL
			break
		}
	}
	if checksumURL == "" {
		return nil, fmt.Errorf("checksums.txt not found in release")
	}

	req, err := http.NewRequest("GET", checksumURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "amux-updater")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching checksums: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading checksums: %w", err)
	}

	return parseChecksums(string(body)), nil
}

// parseChecksums parses GoReleaser checksums.txt format.
// Format: sha256sum  filename
func parseChecksums(content string) map[string]string {
	checksums := make(map[string]string)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			checksums[parts[1]] = parts[0]
		}
	}
	return checksums
}

// GetPlatformAssetName returns the expected asset name for the current platform.
func GetPlatformAssetName(version string) string {
	os := runtime.GOOS
	arch := runtime.GOARCH

	// Remove "v" prefix if present for GoReleaser naming
	v := strings.TrimPrefix(version, "v")

	return fmt.Sprintf("amux_%s_%s_%s.tar.gz", v, os, arch)
}

// FindPlatformAsset finds the appropriate asset for the current platform.
func FindPlatformAsset(release *Release) *Asset {
	expectedName := GetPlatformAssetName(release.TagName)
	for i := range release.Assets {
		if release.Assets[i].Name == expectedName {
			return &release.Assets[i]
		}
	}
	return nil
}
