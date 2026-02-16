package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ParseGlobalFlags extracts global flags from CLI args.
//
// It always consumes global flags in the prefix, then attempts to consume
// additional global flags after the command path while preserving values of
// command-local flags that require arguments (for example: `agent send --text`).
func ParseGlobalFlags(args []string) (GlobalFlags, []string, error) {
	var gf GlobalFlags

	// Parse prefix globals first so command detection remains stable.
	i := 0
	for i < len(args) {
		consumed, next, err := parseGlobalFlagAt(args, i, &gf)
		if err != nil {
			return gf, nil, err
		}
		if !consumed {
			break
		}
		i = next
	}

	rest := append([]string(nil), args[i:]...)
	if len(rest) == 0 {
		return gf, nil, nil
	}

	pathTokenIndexes, localValueFlags, pathKey, err := commandPathParseRules(rest)
	if err != nil {
		return gf, nil, err
	}
	filtered := make([]string, 0, len(rest))

	expectLocalValue := false
	for j := 0; j < len(rest); j++ {
		arg := rest[j]

		if _, isPathToken := pathTokenIndexes[j]; isPathToken {
			filtered = append(filtered, arg)
			continue
		}

		if expectLocalValue {
			filtered = append(filtered, arg)
			expectLocalValue = false
			continue
		}

		if localFlagRequiresValue(localValueFlags, arg) {
			filtered = append(filtered, arg)
			if localFlagConsumesRemainder(pathKey, arg) {
				if j+1 < len(rest) {
					filtered = append(filtered, rest[j+1:]...)
				}
				break
			}
			if !strings.Contains(arg, "=") {
				expectLocalValue = true
			}
			continue
		}

		consumed, next, err := parseGlobalFlagAt(rest, j, &gf)
		if err != nil {
			return gf, nil, err
		}
		if consumed {
			j = next - 1
			continue
		}

		filtered = append(filtered, arg)
	}

	if len(filtered) == 0 {
		return gf, nil, nil
	}
	return gf, filtered, nil
}

func parseGlobalFlagAt(args []string, i int, gf *GlobalFlags) (bool, int, error) {
	if i < 0 || i >= len(args) {
		return false, i, nil
	}
	arg := args[i]
	switch arg {
	case "--json":
		if gf != nil {
			gf.JSON = true
		}
		return true, i + 1, nil
	case "--no-color":
		if gf != nil {
			gf.NoColor = true
		}
		return true, i + 1, nil
	case "--quiet", "-q":
		if gf != nil {
			gf.Quiet = true
		}
		return true, i + 1, nil
	case "--cwd":
		if i+1 >= len(args) {
			return true, i + 1, errors.New("--cwd requires a value")
		}
		if args[i+1] == "" {
			return true, i + 2, errors.New("--cwd requires a non-empty value")
		}
		if gf != nil {
			gf.Cwd = args[i+1]
		}
		return true, i + 2, nil
	case "--timeout":
		if i+1 >= len(args) {
			return true, i + 1, errors.New("--timeout requires a value")
		}
		d, err := time.ParseDuration(args[i+1])
		if err != nil {
			return true, i + 2, fmt.Errorf("invalid --timeout value: %w", err)
		}
		if gf != nil {
			gf.Timeout = d
		}
		return true, i + 2, nil
	case "--request-id":
		if i+1 >= len(args) {
			return true, i + 1, errors.New("--request-id requires a value")
		}
		if gf != nil {
			gf.RequestID = strings.TrimSpace(args[i+1])
		}
		return true, i + 2, nil
	default:
		if strings.HasPrefix(arg, "--cwd=") {
			val := strings.TrimPrefix(arg, "--cwd=")
			if val == "" {
				return true, i + 1, errors.New("--cwd requires a non-empty value")
			}
			if gf != nil {
				gf.Cwd = val
			}
			return true, i + 1, nil
		}
		if strings.HasPrefix(arg, "--timeout=") {
			val := strings.TrimPrefix(arg, "--timeout=")
			d, err := time.ParseDuration(val)
			if err != nil {
				return true, i + 1, fmt.Errorf("invalid --timeout value: %w", err)
			}
			if gf != nil {
				gf.Timeout = d
			}
			return true, i + 1, nil
		}
		if strings.HasPrefix(arg, "--request-id=") {
			if gf != nil {
				gf.RequestID = strings.TrimSpace(strings.TrimPrefix(arg, "--request-id="))
			}
			return true, i + 1, nil
		}
	}
	return false, i + 1, nil
}

func commandPathParseRules(args []string) (map[int]struct{}, map[string]struct{}, string, error) {
	if len(args) == 0 {
		return nil, nil, "", nil
	}
	if strings.HasPrefix(args[0], "-") {
		return nil, nil, "", nil
	}

	pathTokens := []string{args[0]}
	pathIndexes := []int{0}

	next := 1
	switch args[0] {
	case "workspace", "logs", "agent", "session", "project", "terminal":
		token, idx, following, ok, err := nextCommandToken(args, next)
		if err != nil {
			return nil, nil, "", err
		}
		if ok {
			pathTokens = append(pathTokens, token)
			pathIndexes = append(pathIndexes, idx)
			next = following
		}
	}

	if len(pathTokens) >= 2 && args[0] == "agent" && pathTokens[1] == "job" {
		token, idx, _, ok, err := nextCommandToken(args, next)
		if err != nil {
			return nil, nil, "", err
		}
		if ok {
			pathTokens = append(pathTokens, token)
			pathIndexes = append(pathIndexes, idx)
		}
	}

	tokenIndexSet := make(map[int]struct{}, len(pathIndexes))
	for _, idx := range pathIndexes {
		tokenIndexSet[idx] = struct{}{}
	}

	pathKey := strings.Join(pathTokens, " ")
	return tokenIndexSet, localFlagsRequiringValue(pathKey), pathKey, nil
}

func nextCommandToken(args []string, start int) (token string, tokenIndex, next int, ok bool, err error) {
	for i := start; i < len(args); {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			consumed, following, parseErr := parseGlobalFlagAt(args, i, nil)
			if parseErr != nil {
				return "", 0, 0, false, parseErr
			}
			if consumed {
				i = following
				continue
			}
			return "", 0, 0, false, nil
		}
		return arg, i, i + 1, true, nil
	}
	return "", 0, 0, false, nil
}

func localFlagsRequiringValue(pathKey string) map[string]struct{} {
	switch pathKey {
	case "workspace list", "workspace ls":
		return map[string]struct{}{"--repo": {}}
	case "workspace create":
		return map[string]struct{}{
			"--project":         {},
			"--assistant":       {},
			"--base":            {},
			"--idempotency-key": {},
		}
	case "workspace remove", "workspace rm":
		return map[string]struct{}{"--idempotency-key": {}}
	case "agent list", "agent ls":
		return map[string]struct{}{"--workspace": {}}
	case "agent capture":
		return map[string]struct{}{"--lines": {}}
	case "agent run":
		return map[string]struct{}{
			"--workspace":       {},
			"--assistant":       {},
			"--name":            {},
			"--prompt":          {},
			"--idempotency-key": {},
		}
	case "agent send":
		return map[string]struct{}{
			"--agent":           {},
			"--text":            {},
			"--idempotency-key": {},
			"--job-id":          {},
		}
	case "agent stop":
		return map[string]struct{}{
			"--agent":           {},
			"--grace-period":    {},
			"--idempotency-key": {},
		}
	case "agent job wait":
		return map[string]struct{}{
			"--timeout":  {},
			"--interval": {},
		}
	case "agent job cancel":
		return map[string]struct{}{
			"--idempotency-key": {},
		}
	case "terminal list":
		return map[string]struct{}{"--workspace": {}}
	case "terminal run":
		return map[string]struct{}{
			"--workspace": {},
			"--text":      {},
		}
	case "terminal logs":
		return map[string]struct{}{
			"--workspace":      {},
			"--lines":          {},
			"--interval":       {},
			"--idle-threshold": {},
		}
	case "logs tail":
		return map[string]struct{}{"--lines": {}}
	case "session prune":
		return map[string]struct{}{"--older-than": {}}
	default:
		return nil
	}
}

func localFlagRequiresValue(localValueFlags map[string]struct{}, arg string) bool {
	if len(localValueFlags) == 0 || !strings.HasPrefix(arg, "-") {
		return false
	}

	name := arg
	if idx := strings.Index(name, "="); idx >= 0 {
		name = name[:idx]
	}
	_, ok := localValueFlags[name]
	return ok
}

func localFlagConsumesRemainder(pathKey, arg string) bool {
	if pathKey != "terminal run" {
		return false
	}
	return arg == "--text" || strings.HasPrefix(arg, "--text=")
}
