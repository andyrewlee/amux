package cli

import "strings"

type commandPathPattern struct {
	Prefix []string
	Depth  int
}

type commandFlagSpec struct {
	valueFlags     map[string]struct{}
	switchFlags    map[string]struct{}
	remainderFlags map[string]struct{}
}

var commandPathPatterns = []commandPathPattern{
	{Prefix: []string{"workspace"}, Depth: 2},
	{Prefix: []string{"doctor"}, Depth: 2},
	{Prefix: []string{"logs"}, Depth: 2},
	{Prefix: []string{"agent"}, Depth: 2},
	{Prefix: []string{"agent", "job"}, Depth: 3},
	{Prefix: []string{"task"}, Depth: 2},
	{Prefix: []string{"dev"}, Depth: 2},
	{Prefix: []string{"assistant"}, Depth: 2},
	{Prefix: []string{"session"}, Depth: 2},
	{Prefix: []string{"project"}, Depth: 2},
	{Prefix: []string{"terminal"}, Depth: 2},
}

var commandFlagSpecs = map[string]commandFlagSpec{
	"doctor tmux":    {valueFlags: setOf("--older-than")},
	"workspace list": {valueFlags: setOf("--repo", "--project")},
	"workspace ls":   {valueFlags: setOf("--repo", "--project")},
	// "workspace create" takes the workspace name as a positional token, not
	// a "--name" flag; keep only actual local flags here.
	"workspace create": {valueFlags: setOf("--project", "--assistant", "--base", "--idempotency-key")},
	"workspace remove": {valueFlags: setOf("--idempotency-key")},
	"workspace rm":     {valueFlags: setOf("--idempotency-key")},
	"agent list":       {valueFlags: setOf("--workspace")},
	"agent ls":         {valueFlags: setOf("--workspace")},
	"agent capture":    {valueFlags: setOf("--lines")},
	"agent run": {
		valueFlags: setOf(
			"--workspace",
			"--assistant",
			"--name",
			"--prompt",
			"--idempotency-key",
			"--wait-timeout",
			"--idle-threshold",
		),
	},
	"agent send": {
		valueFlags: setOf(
			"--agent",
			"--text",
			"--idempotency-key",
			"--job-id",
			"--wait-timeout",
			"--idle-threshold",
		),
	},
	"agent stop": {
		valueFlags: setOf(
			"--agent",
			"--grace-period",
			"--idempotency-key",
		),
	},
	"agent watch": {
		valueFlags: setOf(
			"--lines",
			"--interval",
			"--idle-threshold",
			"--heartbeat",
		),
	},
	"task start": {
		valueFlags: setOf(
			"--workspace",
			"--assistant",
			"--prompt",
			"--wait-timeout",
			"--idle-threshold",
			"--start-lock-ttl",
			"--idempotency-key",
		),
	},
	"task status": {
		valueFlags: setOf(
			"--workspace",
			"--assistant",
		),
	},
	"dev perf-compare": {
		valueFlags: setOf(
			"--baseline-file",
			"--tolerance",
			"--frames",
			"--scrollback-frames",
			"--warmup",
			"--width",
			"--height",
		),
	},
	"dev openclaw-sync": {
		valueFlags: setOf(
			"--skill-src",
			"--main-workspace",
			"--dev-workspace",
		),
		switchFlags: setOf("--skip-verify"),
	},
	"assistant step": {
		valueFlags: setOf(
			"--wait-timeout",
			"--idle-threshold",
			"--idempotency-key",
			"--workspace",
			"--assistant",
			"--prompt",
			"--agent",
			"--text",
		),
	},
	"assistant turn": {
		valueFlags: setOf(
			"--wait-timeout",
			"--idle-threshold",
			"--max-steps",
			"--turn-budget",
			"--followup-text",
			"--workspace",
			"--assistant",
			"--prompt",
			"--agent",
			"--text",
			"--idempotency-key",
		),
	},
	"assistant dx": {
		valueFlags: setOf(
			"--workspace",
			"--assistant",
			"--prompt",
			"--wait-timeout",
			"--idle-threshold",
			"--start-lock-ttl",
			"--idempotency-key",
			"--max-steps",
			"--turn-budget",
			"--monitor-timeout",
			"--poll-interval",
			"--agent",
			"--text",
			"--task",
			"--project",
			"--repo",
			"--path",
			"--base",
			"--lines",
			"--older-than",
			"--message",
		),
		remainderFlags: setOf("--prompt", "--text", "--task", "--message"),
	},
	"assistant present": {
		valueFlags: nil,
	},
	"assistant dogfood": {
		valueFlags: setOf("--repo", "--workspace", "--assistant", "--report-dir"),
	},
	"assistant poll-agent": {
		valueFlags: setOf(
			"--session",
			"--lines",
			"--interval",
			"--timeout",
		),
	},
	"assistant wait-for-idle": {
		valueFlags: setOf(
			"--session",
			"--timeout",
			"--idle-threshold",
		),
	},
	"assistant format-capture": {
		valueFlags: nil,
	},
	// --timeout intentionally shadows the global --timeout flag; local flags
	// are checked before global parsing, so the value is preserved for this
	// subcommand. The global --timeout can still be set via prefix position.
	"agent job wait": {valueFlags: setOf("--timeout", "--interval")},
	"agent job cancel": {
		valueFlags: setOf("--idempotency-key"),
	},
	"terminal list": {valueFlags: setOf("--workspace")},
	"terminal run": {
		valueFlags:     setOf("--workspace", "--text"),
		remainderFlags: setOf("--text"),
	},
	"terminal logs": {
		valueFlags: setOf("--workspace", "--lines", "--interval", "--idle-threshold"),
	},
	"logs tail":     {valueFlags: setOf("--lines")},
	"session prune": {valueFlags: setOf("--older-than")},
}

func setOf(values ...string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func commandPathDepthFor(tokens []string) (int, bool) {
	for _, pattern := range commandPathPatterns {
		if len(tokens) != len(pattern.Prefix) {
			continue
		}
		match := true
		for i := range tokens {
			if tokens[i] != pattern.Prefix[i] {
				match = false
				break
			}
		}
		if match {
			return pattern.Depth, true
		}
	}
	return 0, false
}

func parseCommandPathTokens(args []string) (tokens []string, tokenIndexes []int, err error) {
	if len(args) == 0 {
		return nil, nil, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return nil, nil, nil
	}

	tokens = []string{args[0]}
	tokenIndexes = []int{0}
	depth := 1
	if parsedDepth, ok := commandPathDepthFor(tokens); ok {
		depth = parsedDepth
	}
	next := 1

	for len(tokens) < depth {
		token, idx, following, ok, parseErr := nextCommandToken(args, next)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		if !ok {
			break
		}
		tokens = append(tokens, token)
		tokenIndexes = append(tokenIndexes, idx)
		next = following

		if parsedDepth, ok := commandPathDepthFor(tokens); ok {
			depth = parsedDepth
		}
	}
	return tokens, tokenIndexes, nil
}

func commandFlagSpecForPath(pathKey string) commandFlagSpec {
	spec, ok := commandFlagSpecs[pathKey]
	if !ok {
		return commandFlagSpec{}
	}
	return spec
}

func localFlagsWithoutValue(pathKey string) map[string]struct{} {
	spec := commandFlagSpecForPath(pathKey)
	return spec.switchFlags
}
