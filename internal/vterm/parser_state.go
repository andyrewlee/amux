package vterm

// ParserStateSnapshot captures the parser's exact in-flight state so callers
// can preserve stream continuity across non-PTY screen restores.
type ParserStateSnapshot struct {
	valid           bool
	state           parseState
	params          []int
	paramBuf        string
	intermediate    byte
	csiIntermediate byte
	oscBuf          string
	utf8Buf         [4]byte
	utf8Len         int
	utf8Pos         int
}

// SnapshotParserState returns an exact snapshot of the current parser state.
func (v *VTerm) SnapshotParserState() ParserStateSnapshot {
	if v == nil || v.parser == nil {
		return ParserStateSnapshot{}
	}
	snapshot := ParserStateSnapshot{
		valid:           true,
		state:           v.parser.state,
		paramBuf:        v.parser.paramBuf.String(),
		intermediate:    v.parser.intermediate,
		csiIntermediate: v.parser.csiIntermediate,
		oscBuf:          v.parser.oscBuf.String(),
		utf8Buf:         v.parser.utf8Buf,
		utf8Len:         v.parser.utf8Len,
		utf8Pos:         v.parser.utf8Pos,
	}
	if len(v.parser.params) > 0 {
		snapshot.params = append([]int(nil), v.parser.params...)
	}
	return snapshot
}

// RestoreParserState reapplies a previously captured parser snapshot.
func (v *VTerm) RestoreParserState(snapshot ParserStateSnapshot) {
	if v == nil || v.parser == nil || !snapshot.valid {
		return
	}
	v.parser.state = snapshot.state
	if len(snapshot.params) == 0 {
		v.parser.params = v.parser.params[:0]
	} else {
		v.parser.params = append(v.parser.params[:0], snapshot.params...)
	}
	v.parser.paramBuf.Reset()
	if snapshot.paramBuf != "" {
		v.parser.paramBuf.WriteString(snapshot.paramBuf)
	}
	v.parser.intermediate = snapshot.intermediate
	v.parser.csiIntermediate = snapshot.csiIntermediate
	v.parser.oscBuf.Reset()
	if snapshot.oscBuf != "" {
		v.parser.oscBuf.WriteString(snapshot.oscBuf)
	}
	v.parser.utf8Buf = snapshot.utf8Buf
	v.parser.utf8Len = snapshot.utf8Len
	v.parser.utf8Pos = snapshot.utf8Pos
}
