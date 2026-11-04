package agentstatus

import "strings"

// Record is one stored record: exactly the keys of the last report that
// addressed its id (the protocol replaces records completely). RootID is
// the root record's id ("").
type Record struct {
	ID        string
	State     string
	App       string
	Kind      string
	Message   string
	Title     string
	Progress  int
	UpdatedAt uint64 // internal monotonic counter, larger = newer
}

// RootID is the id of the root record (a report with no id).
const RootID = ""

// RecordSet is the terminal-side store the protocol asks a consumer to
// hold: one record per id, replaced wholesale by each report that uses the
// id, removed by clear, capped with least-recently-updated eviction. It is
// the state of an explicit machine driven by three events: apply a report,
// observe a shell prompt (OSC 133 A), and observe process exit.
type RecordSet struct {
	records map[string]*Record
	clock   uint64
}

// NewRecordSet returns an empty record set.
func NewRecordSet() *RecordSet {
	return &RecordSet{records: make(map[string]*Record)}
}

// Apply folds one parsed report into the set. A clear removes the
// addressed record and every record beneath it (with no id, every record).
// A state report replaces the addressed record completely. The return
// value reports whether anything changed (a clear of an unknown id or an
// identical re-report did not).
func (rs *RecordSet) Apply(r Report) bool {
	rs.clock++
	if r.IsClear() {
		return rs.clear(r.ID)
	}
	if existing, ok := rs.records[r.ID]; ok &&
		existing.State == r.State && existing.App == r.App &&
		existing.Kind == r.Kind && existing.Message == r.Message &&
		existing.Title == r.Title && existing.Progress == r.Progress {
		existing.UpdatedAt = rs.clock // re-observed: refresh recency, no change
		return false
	}
	rec := &Record{
		ID:        r.ID,
		State:     r.State,
		App:       r.App,
		Kind:      r.Kind,
		Message:   r.Message,
		Title:     r.Title,
		Progress:  r.Progress,
		UpdatedAt: rs.clock,
	}
	rs.records[r.ID] = rec
	rs.evict()
	return true
}

// clear removes the record for id and every record beneath it (ids that
// begin "id/"). The empty id clears everything.
func (rs *RecordSet) clear(id string) bool {
	if id == RootID {
		n := len(rs.records)
		rs.records = make(map[string]*Record)
		return n > 0
	}
	prefix := id + "/"
	changed := false
	if _, ok := rs.records[id]; ok {
		delete(rs.records, id)
		changed = true
	}
	for k := range rs.records {
		if strings.HasPrefix(k, prefix) {
			delete(rs.records, k)
			changed = true
		}
	}
	return changed
}

// evict enforces the record cap by dropping the least recently updated
// record, per the protocol's limits table.
func (rs *RecordSet) evict() {
	for len(rs.records) > MaxRecordsPerPane {
		var oldestID string
		var oldest uint64
		first := true
		for id, rec := range rs.records {
			if first || rec.UpdatedAt < oldest {
				oldestID, oldest, first = id, rec.UpdatedAt, false
			}
		}
		delete(rs.records, oldestID)
	}
}

// SeverityOf ranks states by how urgently they need the user's attention;
// the protocol leaves presentation to the consumer, and amux presents one
// badge per pane, so the most severe record is the pane's headline.
// Unknown states score below everything (the parser discards them, so this
// only guards stored legacy values).
func SeverityOf(state string) int {
	switch state {
	case StateBlocked:
		return 4
	case StateError:
		return 3
	case StateWorking:
		return 2
	case StateDone:
		return 1
	case StateIdle:
		return 0
	}
	return -1
}

// Fold reduces the record set to the single record amux presents for the
// pane: the most severe state wins; ties prefer the root record (the
// program's own state over a worker's), then the most recently updated.
// With no records the bool is false and the Record is zero.
//
// This is a presentation choice, not protocol behavior: the protocol
// explicitly allows root and child records to disagree ("both are true"),
// and a richer consumer can serialize the whole set instead.
func (rs *RecordSet) Fold() (Record, bool) {
	var best Record
	bestSeverity := -1
	found := false
	for _, rec := range rs.records {
		sev := SeverityOf(rec.State)
		if sev < 0 {
			continue
		}
		better := false
		switch {
		case sev > bestSeverity:
			better = true
		case sev == bestSeverity:
			if rec.ID == RootID && best.ID != RootID {
				better = true // prefer the program's own record
			} else if !(best.ID == RootID && rec.ID != RootID) {
				// Same record class (both root or both children): the
				// newest report is the pane's current truth.
				better = rec.UpdatedAt > best.UpdatedAt
			}
		}
		if better {
			best, bestSeverity, found = *rec, sev, true
		}
	}
	return best, found
}

// DropTransient implements the protocol's record lifetime: working and
// blocked records MUST drop when the process exits or a new shell prompt
// begins (OSC 133 A); done and error survive both, and idle MAY survive.
// amux keeps idle — a pane that reported idle and went quiet is still
// truthfully idle, and the alternative (heuristic guessing) is what this
// protocol exists to replace.
func (rs *RecordSet) DropTransient() bool {
	rs.clock++
	changed := false
	for id, rec := range rs.records {
		if rec.State == StateWorking || rec.State == StateBlocked {
			delete(rs.records, id)
			changed = true
		}
	}
	return changed
}
