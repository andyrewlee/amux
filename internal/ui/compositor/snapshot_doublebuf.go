package compositor

import "github.com/andyrewlee/amux/internal/vterm"

// SnapshotDoubleBuffer alternates between two snapshot buffers so the buffer
// mutated by an incremental snapshot is never the one most recently handed to a
// layer. Each buffer carries rows dirtied while the other buffer was the target,
// because vterm dirty tracking is per snapshot call, not per buffer.
//
// It is not safe for concurrent use; callers must hold the same lock they hold
// when snapshotting the VTerm.
type SnapshotDoubleBuffer struct {
	bufs   [2]*VTermSnapshot
	stale  [2][]bool // nil means fully stale; force a full copy into that buffer.
	last   int
	inited bool
}

// Snapshot returns a current terminal snapshot without mutating the snapshot
// returned by the previous call.
func (d *SnapshotDoubleBuffer) Snapshot(term *vterm.VTerm, showCursor bool) *VTermSnapshot {
	target := 1 - d.last
	if !d.inited {
		target = 0
	}

	prev := d.bufs[target]
	extraDirty := d.stale[target]
	if prev != nil && extraDirty == nil {
		prev = nil
	}

	snap := newVTermSnapshot(term, showCursor, prev, extraDirty)
	if snap == nil {
		return nil
	}

	d.bufs[target] = snap
	d.stale[target] = resetStaleMask(d.stale[target], snap.Height)
	d.markOtherStale(1-target, snap)
	d.last = target
	d.inited = true
	return snap
}

// Reset discards both buffers and all stale-row tracking.
func (d *SnapshotDoubleBuffer) Reset() {
	d.bufs = [2]*VTermSnapshot{}
	d.stale = [2][]bool{}
	d.last = 0
	d.inited = false
}

func (d *SnapshotDoubleBuffer) markOtherStale(other int, snap *VTermSnapshot) {
	if snap == nil || snap.AllDirty || snap.DirtyLines == nil ||
		d.stale[other] == nil || len(d.stale[other]) != snap.Height {
		d.stale[other] = nil
		return
	}
	for y, dirty := range snap.DirtyLines {
		if dirty {
			d.stale[other][y] = true
		}
	}
}

func resetStaleMask(mask []bool, height int) []bool {
	if len(mask) != height {
		return make([]bool, height)
	}
	for i := range mask {
		mask[i] = false
	}
	return mask
}
