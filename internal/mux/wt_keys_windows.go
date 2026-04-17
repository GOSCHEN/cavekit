//go:build windows

package mux

import "strings"

// translateKey converts a tmux-style key token into the raw bytes the child
// process expects on its ConPTY stdin. Unknown tokens are sent verbatim so
// ad-hoc text still reaches the prompt.
//
// Covers the subset used by cavekit (see internal/tui/app.go keyhandler and
// internal/session/autoyes.go):
//
//	Enter, BSpace, Tab, Up, Down, Left, Right, Space,
//	C-<letter> (e.g. C-c, C-d),
//	ESC, arbitrary literals.
func translateKey(token string) []byte {
	switch token {
	case "Enter":
		return []byte{'\r'}
	case "BSpace":
		return []byte{0x7f}
	case "Tab":
		return []byte{'\t'}
	case "ESC", "Escape":
		return []byte{0x1b}
	case "Space":
		return []byte{' '}
	case "Up":
		return []byte("\x1b[A")
	case "Down":
		return []byte("\x1b[B")
	case "Right":
		return []byte("\x1b[C")
	case "Left":
		return []byte("\x1b[D")
	}
	// Ctrl sequences — "C-c" → 0x03, "C-d" → 0x04, etc.
	if strings.HasPrefix(token, "C-") && len(token) == 3 {
		letter := token[2]
		if letter >= 'a' && letter <= 'z' {
			return []byte{letter - 'a' + 1}
		}
		if letter >= 'A' && letter <= 'Z' {
			return []byte{letter - 'A' + 1}
		}
	}
	// Fall through: treat as literal text to type.
	return []byte(token)
}

// translateKeys joins the bytes produced by every token in order.
func translateKeys(tokens []string) []byte {
	var out []byte
	for _, t := range tokens {
		out = append(out, translateKey(t)...)
	}
	return out
}
