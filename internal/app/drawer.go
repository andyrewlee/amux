package app

import (
	"fmt"
	"time"

	"github.com/andyrewlee/amux/internal/ui/drawer"
)

func (a *App) updateDrawer() {
	if a.drawer == nil || a.center == nil {
		return
	}
	processes := []drawer.ProcessInfo{}
	tabs := a.center.MonitorTabs()
	for _, tab := range tabs {
		status := "idle"
		if tab.Running {
			status = "running"
		}
		name := tab.Name
		if name == "" {
			name = tab.Assistant
		}
		started := tab.StartedAt
		if started.IsZero() {
			started = time.Now()
		}
		var exitCode *int
		if !tab.Running && !tab.StoppedAt.IsZero() {
			code := tab.ExitCode
			exitCode = &code
		}
		processes = append(processes, drawer.ProcessInfo{
			ID:          string(tab.ID),
			Name:        fmt.Sprintf("%s (%s)", name, tab.Workspace.Name),
			Status:      status,
			Kind:        "agent",
			WorktreeID:  string(tab.Workspace.ID()),
			StartedAt:   started,
			CompletedAt: tab.StoppedAt,
			ExitCode:    exitCode,
		})
	}
	for _, rec := range a.sortedProcessRecords() {
		exitCode := rec.ExitCode
		processes = append(processes, drawer.ProcessInfo{
			ID:           rec.ID,
			Name:         rec.Name,
			Status:       rec.Status,
			Kind:         rec.Kind,
			WorktreeRoot: rec.WorkspaceRoot,
			WorktreeID:   rec.WorkspaceID,
			ScriptType:   rec.ScriptType,
			StartedAt:    rec.StartedAt,
			CompletedAt:  rec.CompletedAt,
			ExitCode:     exitCode,
		})
	}
	a.drawer.SetProcesses(processes)
}
