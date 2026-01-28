package linear

import "strings"

// MapStateToColumn maps a Linear state to a board column name.
func MapStateToColumn(state State, team Team, cfg BoardConfig) string {
	// Custom mapping by team name/key/id.
	if len(cfg.StateMapping) > 0 {
		keys := []string{team.Name, team.Key, team.ID}
		for _, key := range keys {
			if key == "" {
				continue
			}
			if mapping, ok := cfg.StateMapping[key]; ok {
				if col, ok := mapping[state.Name]; ok {
					return col
				}
			}
		}
	}

	switch strings.ToLower(state.Type) {
	case "backlog", "unstarted":
		return "Todo"
	case "started":
		return "In Progress"
	case "review":
		return "In Review"
	case "completed", "canceled":
		return "Done"
	default:
		return "Todo"
	}
}

// ColumnIndex returns the index of a column name within board columns.
func ColumnIndex(columns []string, name string) int {
	for i, col := range columns {
		if strings.EqualFold(col, name) {
			return i
		}
	}
	return -1
}
