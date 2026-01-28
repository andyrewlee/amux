package attempt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Metadata represents .amux/attempt.json inside a worktree.
type Metadata struct {
	AttemptID       string    `json:"attemptId"`
	IssueID         string    `json:"issueId"`
	IssueIdentifier string    `json:"issueIdentifier"`
	IssueURL        string    `json:"issueUrl"`
	TeamID          string    `json:"teamId"`
	ProjectID       string    `json:"projectId"`
	ParentAttemptID string    `json:"parentAttemptId"`
	BranchName      string    `json:"branchName"`
	BaseRef         string    `json:"baseRef"`
	PRURL           string    `json:"prUrl"`
	CreatedAt       time.Time `json:"createdAt"`
	Host            string    `json:"host"`
	AgentProfile    string    `json:"agentProfile"`
	Status          string    `json:"status"`
	LastSyncedAt    time.Time `json:"lastSyncedAt"`
}

// NewMetadata creates a new attempt metadata record.
func NewMetadata() *Metadata {
	return &Metadata{
		AttemptID:    NewAttemptID(),
		CreatedAt:    time.Now(),
		LastSyncedAt: time.Now(),
		Status:       "in_progress",
	}
}

// NewAttemptID generates a random attempt ID.
func NewAttemptID() string {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		// Fallback to timestamp-based ID.
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000")))
	}
	return hex.EncodeToString(buf)
}

// ShortID returns a short suffix for branch naming.
func ShortID(id string) string {
	if len(id) <= 4 {
		return id
	}
	return id[:4]
}

// Load reads attempt metadata from a worktree root.
func Load(root string) (*Metadata, error) {
	path := attemptPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// Save writes attempt metadata to a worktree root.
func Save(root string, meta *Metadata) error {
	if meta == nil {
		return nil
	}
	path := attemptPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func attemptPath(root string) string {
	return filepath.Join(root, ".amux", "attempt.json")
}
