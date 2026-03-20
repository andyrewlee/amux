package cli

type assistantDXQuickAction struct {
	ID       string `json:"id"`
	ActionID string `json:"action_id"`
	Label    string `json:"label"`
	Command  string `json:"command"`
	Style    string `json:"style"`
	Prompt   string `json:"prompt,omitempty"`
}

type assistantDXChunkMeta struct {
	Index int    `json:"index"`
	Total int    `json:"total"`
	Text  string `json:"text"`
}

type assistantDXChannelPayload struct {
	Message       string                 `json:"message"`
	Chunks        []string               `json:"chunks"`
	ChunksMeta    []assistantDXChunkMeta `json:"chunks_meta"`
	InlineButtons []any                  `json:"inline_buttons"`
}

type assistantDXAssistantUXPayload struct {
	SelectedChannel string `json:"selected_channel"`
}

type assistantDXPayload struct {
	OK               bool                          `json:"ok"`
	Command          string                        `json:"command"`
	Status           string                        `json:"status"`
	Summary          string                        `json:"summary"`
	NextAction       string                        `json:"next_action"`
	SuggestedCommand string                        `json:"suggested_command"`
	Data             any                           `json:"data"`
	QuickActions     []assistantDXQuickAction      `json:"quick_actions"`
	QuickActionByID  map[string]string             `json:"quick_action_by_id"`
	Channel          assistantDXChannelPayload     `json:"channel"`
	AssistantUX      assistantDXAssistantUXPayload `json:"assistant_ux"`
}

type assistantDXCallResult struct {
	Envelope *Envelope
	Stdout   string
	Stderr   string
	ExitCode int
}

type assistantDXInvoker struct {
	version     string
	amuxBin     string
	useExternal bool
}

type assistantDXRunner struct {
	version    string
	selfScript string
	invoker    assistantDXInvoker
}

const assistantDXDefaultReviewPrompt = "Review current uncommitted changes. Return findings first ordered by severity with file references, then residual risks and test gaps."

const (
	assistantDXProbeOK                 = "ok"
	assistantDXProbeError              = "error"
	assistantDXProbeUnsupported        = "unsupported"
	assistantDXProbeUnsupportedAll     = "unsupported_all"
	assistantDXProbeUnsupportedArchive = "unsupported_archived"
)
