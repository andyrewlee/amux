package cli

type agentRunResult struct {
	SessionName string `json:"session_name"`
	AgentID     string `json:"agent_id,omitempty"`
	WorkspaceID string `json:"workspace_id"`
	Assistant   string `json:"assistant"`
	TabID       string `json:"tab_id"`
}

type agentSendResult struct {
	SessionName string `json:"session_name"`
	AgentID     string `json:"agent_id,omitempty"`
	JobID       string `json:"job_id"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	Sent        bool   `json:"sent"`
}
