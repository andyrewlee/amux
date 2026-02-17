package common

// SelectionState tracks mouse selection state for copy/paste.
type SelectionState struct {
	Active    bool // Selection in progress (mouse button down)?
	StartX    int  // Start column (terminal coordinates)
	StartLine int  // Start row (absolute line number, 0 = first scrollback line)
	EndX      int  // End column
	EndLine   int  // End row (absolute line number)
}
