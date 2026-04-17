// Package paths centralizes filesystem path resolution so the rest of the
// codebase never hardcodes Unix-only constants like /tmp, $HOME, or
// /usr/local. All helpers work on Linux, macOS, and Windows.
package paths

import (
	"os"
	"path/filepath"
)

// UserHome returns the current user's home directory.
// Falls back to empty string if the lookup fails; callers should treat
// an empty result as a hard error before joining paths against it.
func UserHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// ClaudeDir returns the Claude Code configuration directory.
// Both Windows and Unix Claude Code use ~/.claude.
func ClaudeDir() string {
	return filepath.Join(UserHome(), ".claude")
}

// CavekitDir returns the Cavekit user-level data directory (~/.cavekit).
// Holds the default config and persisted session state.
func CavekitDir() string {
	return filepath.Join(UserHome(), ".cavekit")
}

// StateFile returns the canonical session state file path.
// Default: ~/.cavekit/state.json.
func StateFile() string {
	return filepath.Join(CavekitDir(), "state.json")
}

// UserConfigFile returns the user-level config path (~/.cavekit/config).
func UserConfigFile() string {
	return filepath.Join(CavekitDir(), "config")
}

// ProjectConfigFile returns the project-level config path relative to the
// given project root (.cavekit/config).
func ProjectConfigFile(projectRoot string) string {
	return filepath.Join(projectRoot, ".cavekit", "config")
}

// TempDir returns the OS temp directory (os.TempDir wrapper kept for
// consistency and future override via env var).
func TempDir() string {
	if v := os.Getenv("CAVEKIT_TMPDIR"); v != "" {
		return v
	}
	return os.TempDir()
}

// TempFile creates a new temp file with the given prefix in TempDir().
// Returns the open file; the caller is responsible for closing and removing it.
func TempFile(prefix string) (*os.File, error) {
	return os.CreateTemp(TempDir(), prefix+"-*")
}

// BinDir returns the preferred install location for the cavekit binary.
// Installers may still place it elsewhere; this is the recommended default.
//   - Windows: %LOCALAPPDATA%\Programs\cavekit
//   - Unix:    /usr/local/bin  (or $HOME/.local/bin if LocalAppData-style
//              fallback is desired)
func BinDir() string {
	return binDir() // OS-specific
}

// MarketplaceDir returns the Claude Code local plugin marketplace directory
// Cavekit installs itself into.
func MarketplaceDir() string {
	return filepath.Join(ClaudeDir(), "plugins", "local", "cavekit-marketplace")
}

// SessionsDir returns the directory holding one metadata file per live
// cavekit-managed multiplexer session. Used by the Windows daemon for
// session discovery (ListSessions, Kill by name, reattach after reboot).
func SessionsDir() string {
	return filepath.Join(CavekitDir(), "sessions")
}
