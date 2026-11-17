// Package clipboard reads text from the system clipboard, for pasting into
// the game's text boxes.
package clipboard

import "strings"

// Text returns the clipboard's text. A folder or file copied in the file
// browser comes back as its path. It returns "" when the clipboard holds
// neither.
func Text() (string, error) { return read() }

// Line tidies pasted text for a one-line box: the first line that has
// anything on it, without control characters or the spaces around it.
func Line(s string) string {
	for _, l := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }) {
		l = strings.TrimSpace(strings.Map(func(r rune) rune {
			if r == '\t' {
				return ' '
			}
			if r < 32 || r == 127 {
				return -1
			}
			return r
		}, l))
		if l != "" {
			return l
		}
	}
	return ""
}
