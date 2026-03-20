package cli

type assistantTurnOptions struct {
	Mode           string
	WaitTimeout    string
	IdleThreshold  string
	MaxSteps       string
	TurnBudget     string
	FollowupText   string
	Workspace      string
	Assistant      string
	Prompt         string
	AgentID        string
	Text           string
	Enter          bool
	IdempotencyKey string
}

type assistantTurnErrorPayload struct {
	OK               bool   `json:"ok"`
	Mode             string `json:"mode,omitempty"`
	Status           string `json:"status"`
	Summary          string `json:"summary"`
	NextAction       string `json:"next_action"`
	SuggestedCommand string `json:"suggested_command"`
	Error            string `json:"error"`
}

type assistantTurnEventResponse struct {
	SubstantiveOutput bool `json:"substantive_output"`
	NeedsInput        bool `json:"needs_input"`
	TimedOut          bool `json:"timed_out"`
	SessionExited     bool `json:"session_exited"`
	Changed           bool `json:"changed"`
}

type assistantTurnEvent struct {
	OK               bool                       `json:"ok"`
	Mode             string                     `json:"mode,omitempty"`
	Status           string                     `json:"status"`
	Summary          string                     `json:"summary"`
	NextAction       string                     `json:"next_action,omitempty"`
	SuggestedCommand string                     `json:"suggested_command,omitempty"`
	AgentID          string                     `json:"agent_id,omitempty"`
	WorkspaceID      string                     `json:"workspace_id,omitempty"`
	Assistant        string                     `json:"assistant,omitempty"`
	Response         assistantTurnEventResponse `json:"response"`
}

type assistantTurnMilestone struct {
	Step             int    `json:"step"`
	Status           string `json:"status"`
	Summary          string `json:"summary"`
	NextAction       string `json:"next_action,omitempty"`
	SuggestedCommand string `json:"suggested_command,omitempty"`
}

type assistantTurnProgress struct {
	Step     int `json:"step"`
	MaxSteps int `json:"max_steps"`
	Percent  int `json:"percent"`
}

type assistantTurnProgressUpdate struct {
	Step             int                           `json:"step"`
	Status           string                        `json:"status"`
	Summary          string                        `json:"summary"`
	NextAction       string                        `json:"next_action,omitempty"`
	SuggestedCommand string                        `json:"suggested_command,omitempty"`
	Progress         assistantTurnProgress         `json:"progress"`
	Message          string                        `json:"message"`
	Delivery         assistantTurnProgressDelivery `json:"delivery"`
}

type assistantTurnProgressDelivery struct {
	Key             string `json:"key"`
	Action          string `json:"action"`
	Priority        int    `json:"priority"`
	ReplacePrevious bool   `json:"replace_previous"`
	Coalesce        bool   `json:"coalesce"`
}

type assistantTurnChannelProgressUpdate struct {
	Step            int    `json:"step"`
	Status          string `json:"status"`
	ProgressPercent int    `json:"progress_percent"`
	Message         string `json:"message"`
}

type assistantTurnChannelPayload struct {
	Message              string                               `json:"message"`
	Verbosity            string                               `json:"verbosity"`
	ChunkChars           int                                  `json:"chunk_chars"`
	Chunks               []string                             `json:"chunks"`
	ChunksMeta           []assistantStepChunkMeta             `json:"chunks_meta"`
	InlineButtonsScope   string                               `json:"inline_buttons_scope"`
	InlineButtonsEnabled bool                                 `json:"inline_buttons_enabled"`
	CallbackDataMaxBytes int                                  `json:"callback_data_max_bytes"`
	InlineButtons        [][]assistantStepInlineButton        `json:"inline_buttons"`
	ActionTokens         []string                             `json:"action_tokens"`
	ActionsFallback      string                               `json:"actions_fallback"`
	ProgressUpdates      []assistantTurnChannelProgressUpdate `json:"progress_updates"`
}

type assistantTurnPayload struct {
	OK                 bool                          `json:"ok"`
	Mode               string                        `json:"mode"`
	TurnID             string                        `json:"turn_id"`
	Status             string                        `json:"status"`
	OverallStatus      string                        `json:"overall_status"`
	StatusEmoji        string                        `json:"status_emoji"`
	Verbosity          string                        `json:"verbosity"`
	Summary            string                        `json:"summary"`
	AgentID            string                        `json:"agent_id,omitempty"`
	WorkspaceID        string                        `json:"workspace_id,omitempty"`
	Assistant          string                        `json:"assistant,omitempty"`
	StepsUsed          int                           `json:"steps_used"`
	MaxSteps           int                           `json:"max_steps"`
	ElapsedSeconds     int                           `json:"elapsed_seconds"`
	TurnBudgetSeconds  int                           `json:"turn_budget_seconds"`
	BudgetExhausted    bool                          `json:"budget_exhausted"`
	ProgressPercent    int                           `json:"progress_percent"`
	TimeoutStreak      int                           `json:"timeout_streak"`
	TimeoutStreakLimit int                           `json:"timeout_streak_limit"`
	NextAction         string                        `json:"next_action,omitempty"`
	SuggestedCommand   string                        `json:"suggested_command,omitempty"`
	Delivery           assistantStepDeliveryPayload  `json:"delivery"`
	Events             []assistantTurnEvent          `json:"events"`
	Milestones         []assistantTurnMilestone      `json:"milestones"`
	ProgressUpdates    []assistantTurnProgressUpdate `json:"progress_updates"`
	QuickActions       []assistantStepQuickAction    `json:"quick_actions"`
	QuickActionMap     map[string]string             `json:"quick_action_map"`
	QuickActionPrompts map[string]string             `json:"quick_action_prompts"`
	Channel            assistantTurnChannelPayload   `json:"channel"`
}

type assistantTurnRuntime struct {
	MaxSteps             int
	TurnBudgetSeconds    int
	FollowupText         string
	TimeoutStreakLimit   int
	CoalesceMilestones   bool
	FinalReserveSeconds  int
	ChunkChars           int
	Verbosity            string
	InlineButtonsScope   string
	InlineButtonsEnabled bool
	StepScriptPath       string
	StepScriptCmdRef     string
	TurnScriptCmdRef     string
	AMUXBin              string
}
