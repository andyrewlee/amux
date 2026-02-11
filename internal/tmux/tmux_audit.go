package tmux

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func killAuditSource() string {
	for skip := 2; skip < 24; skip++ {
		pc, file, line, ok := runtime.Caller(skip)
		if !ok {
			break
		}
		// Report the first caller outside the tmux package.
		if strings.Contains(file, "/internal/tmux/") {
			continue
		}
		fn := "unknown"
		if f := runtime.FuncForPC(pc); f != nil {
			fn = shortFuncName(f.Name())
		}
		return fmt.Sprintf("%s:%d %s", filepath.Base(file), line, fn)
	}
	return "unknown"
}

func shortFuncName(full string) string {
	if i := strings.LastIndex(full, "/"); i >= 0 && i+1 < len(full) {
		return full[i+1:]
	}
	return full
}

func formatTags(tags map[string]string) string {
	if len(tags) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", key, tags[key]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
