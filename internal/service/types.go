package service

import (
	"encoding/json"
	"time"

	"github.com/andyrewlee/medusa/internal/data"
)

// TabState represents the lifecycle state of a tab.
type TabState string

const (
	TabStateStarting TabState = "starting" // Process is being created
	TabStateRunning  TabState = "running"  // Claude is actively processing
	TabStateIdle     TabState = "idle"     // Waiting for user input
	TabStateStopped  TabState = "stopped"  // Process exited, session resumable
	TabStateError    TabState = "error"    // Process exited with error
	TabStateClosed   TabState = "closed"   // Explicitly closed by user
)

// TabKind distinguishes between Claude (structured JSONL) and PTY-based agents.
type TabKind string

const (
	TabKindClaude TabKind = "claude" // Uses --output-format stream-json
	TabKindPTY    TabKind = "pty"    // Uses PTY/tmux (Codex, Gemini, Amp, etc.)
)

// TabInfo describes a single agent tab on the server.
type TabInfo struct {
	ID          string           `json:"id"`
	WorkspaceID data.WorkspaceID `json:"workspace_id"`
	Kind        TabKind          `json:"kind"`
	Assistant   string           `json:"assistant"`   // "claude", "codex", "gemini", etc.
	State       TabState         `json:"state"`
	SessionID   string           `json:"session_id"`  // Claude session UUID (empty for PTY tabs)
	CreatedAt   time.Time        `json:"created_at"`
	TotalCost   float64          `json:"total_cost_usd,omitempty"`
	Model       string           `json:"model,omitempty"`
	TurnCount   int              `json:"turn_count,omitempty"`
}

// SDKMessage represents a single message from Claude Code's stream-json output.
// Each line of the JSONL stream is one SDKMessage.
type SDKMessage struct {
	Type      string          `json:"type"`                 // system, assistant, user, result, stream_event
	Subtype   string          `json:"subtype,omitempty"`    // init, success, error_*, compact_boundary
	UUID      string          `json:"uuid,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`    // For assistant/user types
	Event     json.RawMessage `json:"event,omitempty"`      // For stream_event type
	Result    string          `json:"result,omitempty"`     // For result type
	TotalCost float64         `json:"total_cost_usd,omitempty"`
	Timestamp time.Time       `json:"timestamp,omitempty"`  // Server-side timestamp

	// Fields from system init message
	CWD   string   `json:"cwd,omitempty"`
	Model string   `json:"model,omitempty"`
	Tools []string `json:"tools,omitempty"`

	// Fields from result message
	DurationMs    int64   `json:"duration_ms,omitempty"`
	DurationAPIMs int64   `json:"duration_api_ms,omitempty"`
	IsError       bool    `json:"is_error,omitempty"`
	NumTurns      int     `json:"num_turns,omitempty"`
}

// AssistantContent represents the content array inside an assistant message.
type AssistantContent struct {
	Type  string          `json:"type"`  // "text" or "tool_use"
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`    // tool_use ID
	Name  string          `json:"name,omitempty"`  // tool name (Read, Edit, Bash, etc.)
	Input json.RawMessage `json:"input,omitempty"` // tool input parameters
}

// AssistantMessage is the parsed content of an SDKMessage with type "assistant".
type AssistantMessage struct {
	ID         string             `json:"id"`
	Role       string             `json:"role"`
	Model      string             `json:"model"`
	Content    []AssistantContent `json:"content"`
	StopReason string             `json:"stop_reason"`
	Usage      *UsageInfo         `json:"usage,omitempty"`
}

// UsageInfo tracks token usage for a message.
type UsageInfo struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ToolResult represents a tool execution result.
type ToolResult struct {
	Type      string `json:"type"` // "tool_result"
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// UserMessage is the parsed content of an SDKMessage with type "user".
type UserMessage struct {
	Role    string       `json:"role"`
	Content []ToolResult `json:"content"`
}

// StreamEvent represents a token-level streaming event.
type StreamEvent struct {
	Type  string          `json:"type"` // content_block_delta, content_block_start, etc.
	Index int             `json:"index,omitempty"`
	Delta json.RawMessage `json:"delta,omitempty"`
}

// TextDelta is the delta payload for text streaming.
type TextDelta struct {
	Type string `json:"type"` // "text_delta"
	Text string `json:"text"`
}

// LaunchOpts configures how a Claude agent is launched.
type LaunchOpts struct {
	Profile         string   `json:"profile,omitempty"`
	SkipPermissions bool     `json:"skip_permissions,omitempty"`
	AllowEdits      bool     `json:"allow_edits,omitempty"`
	Isolated        bool     `json:"isolated,omitempty"`
	AllowedTools    []string `json:"allowed_tools,omitempty"`
	MaxTurns        int      `json:"max_turns,omitempty"`
	MaxBudgetUSD    float64  `json:"max_budget_usd,omitempty"`
	ResumeSessionID string   `json:"resume_session_id,omitempty"` // Non-empty to resume
	Prompt          string   `json:"prompt,omitempty"`            // Initial prompt
}

// CreateWorkspaceOpts configures workspace creation.
type CreateWorkspaceOpts struct {
	ProjectPath     string `json:"project_path"`
	Name            string `json:"name"`
	AllowEdits      bool   `json:"allow_edits,omitempty"`
	Isolated        bool   `json:"isolated,omitempty"`
	SkipPermissions bool   `json:"skip_permissions,omitempty"`
	BranchMode      string `json:"branch_mode,omitempty"`   // "remote", "local", "custom"
	CustomBranch    string `json:"custom_branch,omitempty"`
}

// GroupWorkspaceOpts configures group workspace creation.
type GroupWorkspaceOpts struct {
	GroupName       string `json:"group_name"`
	Name            string `json:"name"`
	AllowEdits      bool   `json:"allow_edits,omitempty"`
	Isolated        bool   `json:"isolated,omitempty"`
	SkipPermissions bool   `json:"skip_permissions,omitempty"`
}

// ResumableSession describes a tab that can be resumed after server restart.
type ResumableSession struct {
	TabID       string           `json:"tab_id"`
	WorkspaceID data.WorkspaceID `json:"workspace_id"`
	SessionID   string           `json:"session_id"`
	Assistant   string           `json:"assistant"`
	MessageCount int             `json:"message_count"`
	LastActivity time.Time       `json:"last_activity"`
	TotalCost   float64          `json:"total_cost_usd"`
}
