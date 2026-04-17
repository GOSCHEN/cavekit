package mux

import "strings"

// SessionPrefix is prepended to every cavekit-managed session name.
// Both the tmux and Windows Terminal back-ends honor this prefix so
// ListSessions can filter out unrelated sessions living on the same host.
const SessionPrefix = "bp_"

// SanitizeName turns an arbitrary raw name into a legal multiplexer session
// identifier by replacing characters tmux and wt treat as delimiters, and by
// prepending SessionPrefix.
//
// It is idempotent: passing an already-sanitized name returns it unchanged.
func SanitizeName(name string) string {
	s := strings.ReplaceAll(name, " ", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = strings.ReplaceAll(s, ":", "_")
	if !strings.HasPrefix(s, SessionPrefix) {
		s = SessionPrefix + s
	}
	return s
}

// SessionName is a readability alias for SanitizeName — emphasizes that the
// result is the canonical session identifier used on the multiplexer side.
func SessionName(name string) string {
	return SanitizeName(name)
}
