package app

import (
	"errors"
	"os/exec"
	"sort"
	"time"

	"github.com/andyrewlee/amux/internal/ui/common"
)

func (a *App) updateScriptActivity(processID string, err error) {
	if processID == "" {
		return
	}
	status := common.StatusSuccess
	if err != nil {
		status = common.StatusError
	}
	if entryID := a.scriptActivityIDs[processID]; entryID != "" {
		a.updateActivityEntry(entryID, func(entry *common.ActivityEntry) {
			entry.Status = status
			if err != nil {
				entry.Details = append(entry.Details, err.Error())
			}
		})
	}
	if rec := a.processRecords[processID]; rec != nil {
		rec.CompletedAt = time.Now()
		rec.Status = "done"
		if err != nil {
			rec.Status = "error"
		}
		code := exitCodeFromErr(err)
		rec.ExitCode = &code
	}
}

func (a *App) sortedProcessRecords() []*processRecord {
	records := make([]*processRecord, 0, len(a.processRecords))
	for _, rec := range a.processRecords {
		if rec != nil {
			records = append(records, rec)
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].StartedAt.After(records[j].StartedAt)
	})
	return records
}

func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if ok := errors.As(err, &exitErr); ok {
		return exitErr.ExitCode()
	}
	return 1
}
