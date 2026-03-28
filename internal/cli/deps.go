package cli

import "github.com/andyrewlee/amux/internal/sandbox"

var (
	loadCLIConfig        = sandbox.LoadConfig
	resolveCLIProvider   = sandbox.ResolveProvider
	loadCLISandboxMeta   = sandbox.LoadSandboxMeta
	runAgentAliasRunner  = runAgent
	runCLIInteractiveCmd = sandbox.RunAgentInteractive
)
