package app

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type workspaceFileFingerprint struct {
	modTimeUnixNano int64
	size            int64
	digest          [32]byte
}

type localWorkspaceSaveMarker struct {
	at          time.Time
	fingerprint workspaceFileFingerprint
}

func (a *App) markLocalWorkspaceSaveForID(wsID string) {
	path := a.workspaceMetadataPath(wsID)
	if path == "" {
		return
	}
	a.markLocalWorkspaceSavePath(path)
}

func (a *App) workspaceMetadataPath(wsID string) string {
	if a == nil || a.config == nil || a.config.Paths == nil {
		return ""
	}
	root := strings.TrimSpace(a.config.Paths.MetadataRoot)
	id := strings.TrimSpace(wsID)
	if root == "" || id == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(root, id, "workspace.json"))
}

func (a *App) markLocalWorkspaceSavePath(path string) {
	if a == nil {
		return
	}
	normalized := filepath.Clean(strings.TrimSpace(path))
	if normalized == "" {
		return
	}
	fingerprint, ok := workspaceMetadataFingerprint(normalized)
	if !ok {
		return
	}
	now := time.Now()
	a.localWorkspaceSaveMu.Lock()
	if a.localWorkspaceSavesAt == nil {
		a.localWorkspaceSavesAt = make(map[string]localWorkspaceSaveMarker)
	}
	pruneOldLocalWorkspaceSavesLocked(a.localWorkspaceSavesAt, now)
	a.localWorkspaceSavesAt[normalized] = localWorkspaceSaveMarker{
		at:          now,
		fingerprint: fingerprint,
	}
	a.localWorkspaceSaveMu.Unlock()
}

func (a *App) shouldSuppressWorkspaceReload(paths []string, now time.Time) bool {
	if a == nil || len(paths) == 0 {
		return false
	}

	// Collect markers under the lock, then release before doing filesystem I/O
	// so that os.Stat/os.ReadFile calls in workspaceMetadataFingerprint don't
	// hold localWorkspaceSaveMu.
	type pathMarker struct {
		path   string
		marker localWorkspaceSaveMarker
	}
	var toCheck []pathMarker

	a.localWorkspaceSaveMu.Lock()
	if len(a.localWorkspaceSavesAt) == 0 {
		a.localWorkspaceSaveMu.Unlock()
		return false
	}
	pruneOldLocalWorkspaceSavesLocked(a.localWorkspaceSavesAt, now)
	if len(a.localWorkspaceSavesAt) == 0 {
		a.localWorkspaceSaveMu.Unlock()
		return false
	}
	for _, raw := range paths {
		path := filepath.Clean(strings.TrimSpace(raw))
		if path == "" {
			continue
		}
		marker, ok := a.localWorkspaceSavesAt[path]
		if !ok {
			a.localWorkspaceSaveMu.Unlock()
			return false
		}
		delta := now.Sub(marker.at)
		if delta < 0 || delta > localWorkspaceReloadSuppressWindow {
			a.localWorkspaceSaveMu.Unlock()
			return false
		}
		toCheck = append(toCheck, pathMarker{path: path, marker: marker})
	}
	a.localWorkspaceSaveMu.Unlock()

	// Perform filesystem I/O outside the critical section.
	for _, pm := range toCheck {
		fingerprint, ok := workspaceMetadataFingerprint(pm.path)
		if !ok {
			return false
		}
		if fingerprint != pm.marker.fingerprint {
			return false
		}
	}
	return len(toCheck) > 0
}

func pruneOldLocalWorkspaceSavesLocked(saves map[string]localWorkspaceSaveMarker, now time.Time) {
	for path, marker := range saves {
		if marker.at.IsZero() || now.Sub(marker.at) > localWorkspaceReloadSuppressWindow || now.Before(marker.at) {
			delete(saves, path)
		}
	}
}

// workspaceMetadataFingerprint returns a fingerprint for the file at path.
// Note: there is a TOCTOU gap between Stat and ReadFile — if the file changes
// between the two calls the fingerprint won't match the stored one, causing
// shouldSuppressWorkspaceReload to return false (not suppress). This is the
// safe/conservative direction so the race is benign.
func workspaceMetadataFingerprint(path string) (workspaceFileFingerprint, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return workspaceFileFingerprint{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return workspaceFileFingerprint{}, false
	}
	return workspaceFileFingerprint{
		modTimeUnixNano: info.ModTime().UnixNano(),
		size:            info.Size(),
		digest:          sha256.Sum256(data),
	}, true
}
