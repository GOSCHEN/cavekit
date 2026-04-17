//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

// binDir is the Windows default install location.
// Prefers %LOCALAPPDATA%\Programs\cavekit, falling back to
// %USERPROFILE%\.local\bin if LOCALAPPDATA is unset.
func binDir() string {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "Programs", "cavekit")
	}
	return filepath.Join(UserHome(), ".local", "bin")
}
