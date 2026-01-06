package pty

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/andyrewlee/amux/internal/data"
)

const maxSessionScanSize = 2 * 1024 * 1024

type sessionCandidate struct {
	id      string
	modTime time.Time
	matched bool
}

// ResolveResumeID attempts to resolve a session/thread ID for an agent.
func ResolveResumeID(agentType AgentType, worktreeRoot string, startedAt time.Time) data.ResumeInfo {
	var id string
	switch agentType {
	case AgentCodex:
		id = resolveCodexSessionID(worktreeRoot, startedAt)
	case AgentGemini:
		id = resolveGeminiSessionID(worktreeRoot, startedAt)
	case AgentOpencode:
		id = resolveOpencodeSessionID(worktreeRoot)
	case AgentAmp:
		id = resolveAmpThreadID(worktreeRoot)
	default:
		return data.ResumeInfo{}
	}

	if id == "" {
		return data.ResumeInfo{}
	}

	return data.ResumeInfo{
		Mode: data.ResumeModeID,
		ID:   id,
	}
}

func resolveCodexSessionID(worktreeRoot string, startedAt time.Time) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	sessionsDir := filepath.Join(home, ".codex", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return ""
	}

	var candidates []sessionCandidate
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		id := entry.Name()
		if ext := filepath.Ext(id); ext != "" {
			id = strings.TrimSuffix(id, ext)
		}
		if id == "" {
			continue
		}
		entryPath := filepath.Join(sessionsDir, entry.Name())
		matched := false
		if worktreeRoot != "" {
			if entry.IsDir() {
				matched = dirContainsString(entryPath, worktreeRoot)
			} else {
				matched = fileContainsString(entryPath, worktreeRoot)
			}
		}
		candidates = append(candidates, sessionCandidate{
			id:      id,
			modTime: info.ModTime(),
			matched: matched,
		})
	}

	return selectBestCandidate(candidates, startedAt)
}

func resolveGeminiSessionID(worktreeRoot string, startedAt time.Time) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	tmpDir := filepath.Join(home, ".gemini", "tmp")
	projectDirs, err := os.ReadDir(tmpDir)
	if err != nil {
		return ""
	}

	var candidates []sessionCandidate
	for _, projectDir := range projectDirs {
		if !projectDir.IsDir() {
			continue
		}
		chatsDir := filepath.Join(tmpDir, projectDir.Name(), "chats")
		chats, err := os.ReadDir(chatsDir)
		if err != nil {
			continue
		}
		for _, chat := range chats {
			if chat.IsDir() {
				continue
			}
			info, err := chat.Info()
			if err != nil {
				continue
			}
			id := chat.Name()
			if ext := filepath.Ext(id); ext != "" {
				id = strings.TrimSuffix(id, ext)
			}
			if id == "" {
				continue
			}
			chatPath := filepath.Join(chatsDir, chat.Name())
			matched := false
			if worktreeRoot != "" {
				matched = fileContainsString(chatPath, worktreeRoot)
			}
			candidates = append(candidates, sessionCandidate{
				id:      id,
				modTime: info.ModTime(),
				matched: matched,
			})
		}
	}

	return selectBestCandidate(candidates, startedAt)
}

func resolveOpencodeSessionID(worktreeRoot string) string {
	out, err := runListCommand(worktreeRoot, []string{"opencode", "session", "list", "--format", "json"})
	if err != nil {
		return ""
	}
	sessions := parseJSONSessions(out)
	return selectSessionFromJSON(sessions, worktreeRoot)
}

func resolveAmpThreadID(worktreeRoot string) string {
	commands := [][]string{
		{"amp", "threads", "list", "--format", "json"},
		{"amp", "threads", "list", "--json"},
		{"amp", "threads", "list"},
	}

	for _, args := range commands {
		out, err := runListCommand(worktreeRoot, args)
		if err != nil {
			continue
		}
		if sessions := parseJSONSessions(out); len(sessions) > 0 {
			if id := selectSessionFromJSON(sessions, worktreeRoot); id != "" {
				return id
			}
		}
		if id := extractThreadIDFromText(string(out), worktreeRoot); id != "" {
			return id
		}
	}

	return ""
}

func runListCommand(worktreeRoot string, args []string) ([]byte, error) {
	if len(args) == 0 {
		return nil, exec.ErrNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if worktreeRoot != "" {
		cmd.Dir = worktreeRoot
	}
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return output, err
}

func selectBestCandidate(candidates []sessionCandidate, startedAt time.Time) string {
	if len(candidates) == 0 {
		return ""
	}

	var filtered []sessionCandidate
	if !startedAt.IsZero() {
		cutoff := startedAt.Add(-2 * time.Second)
		for _, c := range candidates {
			if c.modTime.After(cutoff) {
				filtered = append(filtered, c)
			}
		}
	}
	if len(filtered) == 0 {
		filtered = candidates
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].matched != filtered[j].matched {
			return filtered[i].matched
		}
		return filtered[i].modTime.After(filtered[j].modTime)
	})

	return filtered[0].id
}

func fileContainsString(path string, needle string) bool {
	if needle == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.Size() > maxSessionScanSize {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(needle))
}

func dirContainsString(dir string, needle string) bool {
	if needle == "" {
		return false
	}
	candidates := []string{"metadata.json", "session.json", "state.json", "config.json"}
	for _, name := range candidates {
		if fileContainsString(filepath.Join(dir, name), needle) {
			return true
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if fileContainsString(filepath.Join(dir, entry.Name()), needle) {
			return true
		}
	}
	return false
}

func parseJSONSessions(data []byte) []map[string]any {
	var list []map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &list); err == nil {
		return list
	}

	var wrapped map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(data), &wrapped); err != nil {
		return nil
	}
	for _, key := range []string{"sessions", "threads", "items", "data"} {
		if raw, ok := wrapped[key]; ok {
			if items, ok := raw.([]any); ok {
				for _, item := range items {
					if m, ok := item.(map[string]any); ok {
						list = append(list, m)
					}
				}
				break
			}
		}
	}
	return list
}

func selectSessionFromJSON(sessions []map[string]any, worktreeRoot string) string {
	if len(sessions) == 0 {
		return ""
	}

	matches := make([]map[string]any, 0, len(sessions))
	if worktreeRoot != "" {
		for _, session := range sessions {
			if jsonContainsString(session, worktreeRoot) {
				matches = append(matches, session)
			}
		}
	}

	candidates := sessions
	if len(matches) > 0 {
		candidates = matches
	}

	type timedSession struct {
		id   string
		when time.Time
	}

	var timed []timedSession
	for _, session := range candidates {
		id := sessionIDFromMap(session)
		if id == "" {
			continue
		}
		when := sessionTimeFromMap(session)
		timed = append(timed, timedSession{id: id, when: when})
	}

	if len(timed) == 0 {
		return ""
	}

	sort.SliceStable(timed, func(i, j int) bool {
		if timed[i].when.IsZero() || timed[j].when.IsZero() {
			return i < j
		}
		return timed[i].when.After(timed[j].when)
	})

	return timed[0].id
}

func sessionIDFromMap(session map[string]any) string {
	for _, key := range []string{"id", "session_id", "sessionId", "thread_id", "threadId"} {
		if value, ok := session[key]; ok {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func sessionTimeFromMap(session map[string]any) time.Time {
	for _, key := range []string{"updated_at", "updatedAt", "created_at", "createdAt", "timestamp", "time"} {
		if value, ok := session[key]; ok {
			if s, ok := value.(string); ok {
				if t, err := parseTime(s); err == nil {
					return t
				}
			}
		}
	}
	return time.Time{}
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, os.ErrInvalid
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, os.ErrInvalid
}

func jsonContainsString(value any, needle string) bool {
	if needle == "" || value == nil {
		return false
	}
	switch v := value.(type) {
	case string:
		return strings.Contains(v, needle)
	case []any:
		for _, item := range v {
			if jsonContainsString(item, needle) {
				return true
			}
		}
	case map[string]any:
		for _, item := range v {
			if jsonContainsString(item, needle) {
				return true
			}
		}
	}
	return false
}

var ampThreadRegex = regexp.MustCompile(`(?i)(?:threads/|thread[-_ ]?id[: ]*)(T-[a-z0-9-]+)|\bT-[a-z0-9-]+\b`)

func extractThreadIDFromText(text string, worktreeRoot string) string {
	if match := ampThreadRegex.FindStringSubmatch(text); len(match) > 1 {
		if match[1] != "" {
			return match[1]
		}
	}
	if match := ampThreadRegex.FindString(text); match != "" {
		return strings.TrimSpace(match)
	}
	return ""
}
