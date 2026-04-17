package worktree

import (
	"runtime"
	"strings"
)

// foldEqual reports whether a and b are equal under the filesystem's
// case-sensitivity rules: byte-for-byte on Unix, case-insensitive on
// Windows (and macOS HFS+/APFS default). Keeps name comparisons consistent
// with how the filesystem treats worktree directories.
func foldEqual(a, b string) bool {
	if caseInsensitiveFS() {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// hasFoldedPrefix mirrors strings.HasPrefix under the same folding rule.
func hasFoldedPrefix(s, prefix string) bool {
	if caseInsensitiveFS() {
		if len(s) < len(prefix) {
			return false
		}
		return strings.EqualFold(s[:len(prefix)], prefix)
	}
	return strings.HasPrefix(s, prefix)
}

// trimFoldedPrefix strips prefix from s when hasFoldedPrefix reports true.
// Returns s unchanged when the prefix does not match.
func trimFoldedPrefix(s, prefix string) string {
	if !hasFoldedPrefix(s, prefix) {
		return s
	}
	return s[len(prefix):]
}

func caseInsensitiveFS() bool {
	switch runtime.GOOS {
	case "windows", "darwin":
		return true
	}
	return false
}
