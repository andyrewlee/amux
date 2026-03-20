package cli

import "regexp"

const (
	assistantStepCommandName = "assistant.step"
	assistantStepModeRun     = "run"
	assistantStepModeSend    = "send"
)

var (
	assistantStepJSONTailREs = []*regexp.Regexp{
		regexp.MustCompile(`\\",[[:space:]]*\\\"(ok|mode|status|summary|latest_line|next_action|suggested_command|agent_id|workspace_id|assistant|message|delta|needs_input|input_hint|timed_out|session_exited|changed|response|data|error)\\\"[[:space:]]*:[[:space:]].*$`),
		regexp.MustCompile(`",[[:space:]]*"(ok|mode|status|summary|latest_line|next_action|suggested_command|agent_id|workspace_id|assistant|message|delta|needs_input|input_hint|timed_out|session_exited|changed|response|data|error)"[[:space:]]*:[[:space:]].*$`),
	}
	assistantStepSecretRedactions = []struct {
		re   *regexp.Regexp
		repl string
	}{
		{regexp.MustCompile(`(sk-ant-api[0-9]*-[A-Za-z0-9_-]{10})[A-Za-z0-9_-]*`), `${1}***`},
		{regexp.MustCompile(`(sk-[A-Za-z0-9_-]{20})[A-Za-z0-9_-]*`), `${1}***`},
		{regexp.MustCompile(`(ghp_[A-Za-z0-9]{5})[A-Za-z0-9]*`), `${1}***`},
		{regexp.MustCompile(`(gho_[A-Za-z0-9]{5})[A-Za-z0-9]*`), `${1}***`},
		{regexp.MustCompile(`(github_pat_[A-Za-z0-9_]{5})[A-Za-z0-9_]*`), `${1}***`},
		{regexp.MustCompile(`(ghs_[A-Za-z0-9]{5})[A-Za-z0-9]*`), `${1}***`},
		{regexp.MustCompile(`(glpat-[A-Za-z0-9_-]{5})[A-Za-z0-9_-]*`), `${1}***`},
		{regexp.MustCompile(`(xoxb-[A-Za-z0-9]{5})[A-Za-z0-9-]*`), `${1}***`},
		{regexp.MustCompile(`(AKIA[0-9A-Z]{4})[0-9A-Z]{12}`), `${1}************`},
		{regexp.MustCompile(`(Bearer )[A-Za-z0-9+/_=.-]{8,}`), `${1}***`},
		{regexp.MustCompile(`((TOKEN|SECRET|PASSWORD|API_KEY|APIKEY|AUTH_TOKEN|PRIVATE_KEY|ACCESS_KEY|CLIENT_SECRET|WEBHOOK_SECRET)=)[^\s'"]{8,}`), `${1}***`},
	}
)

type assistantStepOptions struct {
	Mode           string
	WaitTimeout    string
	IdleThreshold  string
	IdempotencyKey string
	Workspace      string
	Assistant      string
	Prompt         string
	AgentID        string
	Text           string
	Enter          bool
}

type assistantStepErrorPayload struct {
	OK      bool   `json:"ok"`
	Mode    string `json:"mode,omitempty"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Error   string `json:"error"`
}

type assistantStepUnderlyingEnvelope struct {
	OK    bool                    `json:"ok"`
	Data  assistantStepUnderlying `json:"data"`
	Error *ErrorInfo              `json:"error"`
}

type assistantStepUnderlying struct {
	SessionName string              `json:"session_name"`
	AgentID     string              `json:"agent_id"`
	WorkspaceID string              `json:"workspace_id"`
	ID          string              `json:"id"`
	Assistant   string              `json:"assistant"`
	Response    *waitResponseResult `json:"response"`
}

type assistantStepQuickAction struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Command      string `json:"command"`
	Style        string `json:"style"`
	CallbackData string `json:"callback_data"`
	Prompt       string `json:"prompt,omitempty"`
}

type assistantStepResponsePayload struct {
	LatestLine        string `json:"latest_line,omitempty"`
	Summary           string `json:"summary,omitempty"`
	Delta             string `json:"delta,omitempty"`
	DeltaCompact      string `json:"delta_compact,omitempty"`
	NeedsInput        bool   `json:"needs_input"`
	InputHint         string `json:"input_hint,omitempty"`
	TimedOut          bool   `json:"timed_out"`
	TimedOutStartup   bool   `json:"timed_out_startup"`
	SessionExited     bool   `json:"session_exited"`
	Changed           bool   `json:"changed"`
	SubstantiveOutput bool   `json:"substantive_output"`
}

type assistantStepRecoveryPayload struct {
	Attempted bool `json:"attempted"`
	PollsUsed int  `json:"polls_used"`
}

type assistantStepDeliveryPayload struct {
	Key               string `json:"key"`
	Action            string `json:"action"`
	Priority          int    `json:"priority"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
	ReplacePrevious   bool   `json:"replace_previous"`
	DropPending       bool   `json:"drop_pending"`
	Coalesce          bool   `json:"coalesce"`
}

type assistantStepInlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
	Style        string `json:"style"`
}

type assistantStepChunkMeta struct {
	Index int    `json:"index"`
	Total int    `json:"total"`
	Text  string `json:"text"`
}

type assistantStepChannelPayload struct {
	Message              string                        `json:"message"`
	Verbosity            string                        `json:"verbosity"`
	ChunkChars           int                           `json:"chunk_chars"`
	Chunks               []string                      `json:"chunks"`
	ChunksMeta           []assistantStepChunkMeta      `json:"chunks_meta"`
	InlineButtonsScope   string                        `json:"inline_buttons_scope"`
	InlineButtonsEnabled bool                          `json:"inline_buttons_enabled"`
	CallbackDataMaxBytes int                           `json:"callback_data_max_bytes"`
	InlineButtons        [][]assistantStepInlineButton `json:"inline_buttons"`
	ActionTokens         []string                      `json:"action_tokens"`
	ActionsFallback      string                        `json:"actions_fallback"`
}

type assistantStepPayload struct {
	OK                    bool                         `json:"ok"`
	Mode                  string                       `json:"mode"`
	Status                string                       `json:"status"`
	StatusEmoji           string                       `json:"status_emoji"`
	Verbosity             string                       `json:"verbosity"`
	Summary               string                       `json:"summary"`
	SessionName           string                       `json:"session_name,omitempty"`
	AgentID               string                       `json:"agent_id,omitempty"`
	WorkspaceID           string                       `json:"workspace_id,omitempty"`
	Assistant             string                       `json:"assistant,omitempty"`
	IdempotencyKey        string                       `json:"idempotency_key,omitempty"`
	Response              assistantStepResponsePayload `json:"response"`
	BlockedPermissionMode bool                         `json:"blocked_permission_mode"`
	RecoveredFromCapture  bool                         `json:"recovered_from_capture"`
	Recovery              assistantStepRecoveryPayload `json:"recovery"`
	Delivery              assistantStepDeliveryPayload `json:"delivery"`
	NextAction            string                       `json:"next_action,omitempty"`
	SuggestedCommand      string                       `json:"suggested_command,omitempty"`
	QuickActions          []assistantStepQuickAction   `json:"quick_actions"`
	QuickActionMap        map[string]string            `json:"quick_action_map"`
	QuickActionPrompts    map[string]string            `json:"quick_action_prompts"`
	Channel               assistantStepChannelPayload  `json:"channel"`
}

type assistantStepActionBundle struct {
	QuickActions       []assistantStepQuickAction
	QuickActionMap     map[string]string
	QuickActionPrompts map[string]string
	ActionTokens       []string
	NextAction         string
	SuggestedCommand   string
	StatusSendCommand  string
	RestartCommand     string
	SwitchCodexCommand string
}
