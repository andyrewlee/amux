package update

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer spins up an httptest.Server with the given handler and returns
// a GitHubClient whose API base URL points at it.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*GitHubClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newGitHubClientForTest(srv.URL, srv.Client()), srv
}

func TestFetchLatestReleaseSuccess(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/andyrewlee/amux/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		rel := Release{
			TagName: "v1.2.3",
			Assets: []Asset{
				{Name: "amux_1.2.3_linux_amd64.tar.gz", BrowserDownloadURL: srvURL + "/dl/amux.tar.gz"},
				{Name: "checksums.txt", BrowserDownloadURL: srvURL + "/dl/checksums.txt"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	c := newGitHubClientForTest(srv.URL, srv.Client())

	rel, err := c.FetchLatestRelease()
	if err != nil {
		t.Fatalf("FetchLatestRelease() error = %v", err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("TagName = %q, want %q", rel.TagName, "v1.2.3")
	}
	if len(rel.Assets) != 2 {
		t.Fatalf("len(Assets) = %d, want 2", len(rel.Assets))
	}

	// The checksums asset URL must point back at the test server so a
	// follow-up FetchChecksums call is exercisable end-to-end.
	var checksumURL string
	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			checksumURL = a.BrowserDownloadURL
		}
	}
	if !strings.HasPrefix(checksumURL, srv.URL) {
		t.Errorf("checksums.txt URL = %q, want prefix %q", checksumURL, srv.URL)
	}
}

func TestFetchLatestReleaseNotFound(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := c.FetchLatestRelease()
	if err == nil {
		t.Fatal("FetchLatestRelease() expected error for 404, got nil")
	}
	if got := err.Error(); got != "no releases found" {
		t.Errorf("error = %q, want %q", got, "no releases found")
	}
}

func TestFetchLatestReleaseServerError(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := c.FetchLatestRelease()
	if err == nil {
		t.Fatal("FetchLatestRelease() expected error for 500, got nil")
	}
	if got := err.Error(); !strings.HasPrefix(got, "unexpected status:") {
		t.Errorf("error = %q, want prefix %q", got, "unexpected status:")
	}
}

func TestFetchLatestReleaseMalformedJSON(t *testing.T) {
	c, _ := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{not valid json"))
	})

	_, err := c.FetchLatestRelease()
	if err == nil {
		t.Fatal("FetchLatestRelease() expected error for malformed JSON, got nil")
	}
	if got := err.Error(); !strings.HasPrefix(got, "decoding response:") {
		t.Errorf("error = %q, want prefix %q", got, "decoding response:")
	}
}

func TestDownloadAssetSuccess(t *testing.T) {
	payload := []byte("binary-asset-bytes")
	c, srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	})

	var buf bytes.Buffer
	if err := c.DownloadAsset(srv.URL+"/dl/amux.tar.gz", &buf); err != nil {
		t.Fatalf("DownloadAsset() error = %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("downloaded = %q, want %q", buf.Bytes(), payload)
	}
}

func TestDownloadAssetNon200(t *testing.T) {
	c, srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	var buf bytes.Buffer
	err := c.DownloadAsset(srv.URL+"/dl/amux.tar.gz", &buf)
	if err == nil {
		t.Fatal("DownloadAsset() expected error for non-200, got nil")
	}
	if got := err.Error(); !strings.HasPrefix(got, "unexpected status:") {
		t.Errorf("error = %q, want prefix %q", got, "unexpected status:")
	}
}

func TestFetchChecksumsSuccess(t *testing.T) {
	const body = "abc123  amux_1.2.3_linux_amd64.tar.gz\n" +
		"def456  amux_1.2.3_darwin_arm64.tar.gz\n"
	c, srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})

	rel := &Release{Assets: []Asset{
		{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/dl/checksums.txt"},
	}}

	sums, err := c.FetchChecksums(rel)
	if err != nil {
		t.Fatalf("FetchChecksums() error = %v", err)
	}
	if got := sums["amux_1.2.3_linux_amd64.tar.gz"]; got != "abc123" {
		t.Errorf("linux checksum = %q, want %q", got, "abc123")
	}
	if got := sums["amux_1.2.3_darwin_arm64.tar.gz"]; got != "def456" {
		t.Errorf("darwin checksum = %q, want %q", got, "def456")
	}
}

func TestFetchChecksumsMissingAsset(t *testing.T) {
	c := newGitHubClientForTest("http://unused.invalid", http.DefaultClient)

	rel := &Release{Assets: []Asset{
		{Name: "amux_1.2.3_linux_amd64.tar.gz", BrowserDownloadURL: "http://unused.invalid/dl"},
	}}

	_, err := c.FetchChecksums(rel)
	if err == nil {
		t.Fatal("FetchChecksums() expected error when checksums.txt absent, got nil")
	}
	if got := err.Error(); got != "checksums.txt not found in release" {
		t.Errorf("error = %q, want %q", got, "checksums.txt not found in release")
	}
}

func TestFetchChecksumsNon200(t *testing.T) {
	c, srv := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	rel := &Release{Assets: []Asset{
		{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/dl/checksums.txt"},
	}}

	_, err := c.FetchChecksums(rel)
	if err == nil {
		t.Fatal("FetchChecksums() expected error for non-200, got nil")
	}
	if got := err.Error(); !strings.HasPrefix(got, "unexpected status:") {
		t.Errorf("error = %q, want prefix %q", got, "unexpected status:")
	}
}
