package cli

import (
	"io"
	"time"
)

type assistantDogfoodOptions struct {
	RepoPath      string
	WorkspaceName string
	Assistant     string
	ReportDir     string
	KeepTemp      bool
}

type assistantDogfoodInvoker struct {
	Path       string
	PrefixArgs []string
}

type assistantDogfoodRuntime struct {
	Output               io.Writer
	Err                  io.Writer
	RepoPath             string
	RepoRoot             string
	TempRoot             string
	ReportDir            string
	ReportDirCreated     bool
	DXContextFile        string
	RunTag               string
	PrimaryWorkspaceName string
	SecondaryWorkspace   string
	Assistant            string
	KeepTemp             bool
	DXInvoker            assistantDogfoodInvoker
	AssistantBin         string
	GitBin               string
	ChannelAgentID       string
	ChannelAgentCreated  bool
}

type assistantDogfoodRecordedCommand struct {
	Slug       string
	RawPath    string
	JSONPath   string
	StatusPath string
	StatusLine string
	Payload    map[string]any
}

type assistantDogfoodChannelConfig struct {
	Channel        string
	PrimaryAgent   string
	FallbackAgent  string
	RequireNonce   bool
	RequireProof   bool
	RequireMarkers bool
	Timeout        time.Duration
	TimeoutLabel   string
}
