package cli

import "strings"

func parseErrorWantsJSON(args []string, gf GlobalFlags) bool {
	if gf.JSON {
		return true
	}
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--json" {
			return true
		}
		// Parse prefix globals. For malformed globals, keep scanning to allow
		// a later explicit --json to opt into JSON error formatting.
		consumed, next, _ := parseGlobalFlagAt(args, i, nil)
		if !consumed {
			rest := args[i:]
			pathTokenIndexes, localValueFlags := commandPathParseRulesForParseError(rest)
			expectLocalValue := false
			for j := 0; j < len(rest); j++ {
				restArg := rest[j]
				if _, isPathToken := pathTokenIndexes[j]; isPathToken {
					continue
				}
				if expectLocalValue {
					expectLocalValue = false
					continue
				}
				if localFlagRequiresValue(localValueFlags, restArg) {
					if !strings.Contains(restArg, "=") {
						expectLocalValue = true
					}
					continue
				}
				if restArg == "--json" {
					return true
				}
				consumedRest, nextRest, _ := parseGlobalFlagAt(rest, j, nil)
				if consumedRest {
					if nextRest > j {
						j = nextRest - 1
					}
					continue
				}
			}
			return false
		}
		if next <= i {
			i++
		} else {
			i = next
		}
	}
	return false
}

func commandPathParseRulesForParseError(args []string) (map[int]struct{}, map[string]struct{}) {
	if len(args) == 0 {
		return nil, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return nil, nil
	}

	pathTokens := []string{args[0]}
	pathIndexes := []int{0}

	next := 1
	switch args[0] {
	case "workspace", "logs", "agent":
		token, idx, following, ok := nextCommandTokenForParseError(args, next)
		if ok {
			pathTokens = append(pathTokens, token)
			pathIndexes = append(pathIndexes, idx)
			next = following
		}
	}

	if len(pathTokens) >= 2 && args[0] == "agent" && pathTokens[1] == "job" {
		token, idx, _, ok := nextCommandTokenForParseError(args, next)
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
	return tokenIndexSet, localFlagsRequiringValue(pathKey)
}

func nextCommandTokenForParseError(args []string, start int) (token string, tokenIndex, next int, ok bool) {
	for i := start; i < len(args); {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			consumed, following, _ := parseGlobalFlagAt(args, i, nil)
			if consumed {
				if following <= i {
					i++
				} else {
					i = following
				}
				continue
			}
			return "", 0, 0, false
		}
		return arg, i, i + 1, true
	}
	return "", 0, 0, false
}
