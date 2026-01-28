package linear

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Cache manages issue cache persistence.
type Cache struct {
	Root string
}

// CacheFile is the JSON schema stored on disk.
type CacheFile struct {
	AccountName string    `json:"accountName"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Issues      []Issue   `json:"issues"`
}

// NewCache creates a cache with the given root.
func NewCache(root string) *Cache {
	return &Cache{Root: root}
}

// SaveIssues writes issues to cache.
func (c *Cache) SaveIssues(accountName, viewerID string, issues []Issue) error {
	if c == nil {
		return nil
	}
	if accountName == "" {
		return fmt.Errorf("linear: cache account name required")
	}
	if viewerID == "" {
		viewerID = "unknown"
	}
	path := filepath.Join(c.Root, "linear", accountName, viewerID, "issues.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	payload := CacheFile{AccountName: accountName, UpdatedAt: time.Now(), Issues: issues}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadIssues loads issues from cache.
// If viewerID is empty, it loads the most recent cache for the account.
func (c *Cache) LoadIssues(accountName, viewerID string) ([]Issue, error) {
	if c == nil {
		return nil, nil
	}
	if accountName == "" {
		return nil, nil
	}
	if viewerID != "" {
		path := filepath.Join(c.Root, "linear", accountName, viewerID, "issues.json")
		return readCacheFile(path)
	}

	// find latest cache under account
	root := filepath.Join(c.Root, "linear", accountName)
	entries := []string{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "issues.json" {
			entries = append(entries, path)
		}
		return nil
	})
	if len(entries) == 0 {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool {
		iInfo, errI := os.Stat(entries[i])
		jInfo, errJ := os.Stat(entries[j])
		if errI != nil || errJ != nil {
			return entries[i] > entries[j]
		}
		return iInfo.ModTime().After(jInfo.ModTime())
	})
	return readCacheFile(entries[0])
}

func readCacheFile(path string) ([]Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var payload CacheFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Issues, nil
}
