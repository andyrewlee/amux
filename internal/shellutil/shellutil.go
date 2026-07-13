// Package shellutil holds small, security-sensitive shell helpers shared across
// packages so their behavior can't drift between copies.
package shellutil

import "strings"

// ShellQuote wraps value in single quotes for safe use as one POSIX shell word,
// escaping embedded single quotes using the standard close-quote, backslash-quote,
// open-quote idiom. An empty string becomes two adjacent single-quote characters.
func ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
