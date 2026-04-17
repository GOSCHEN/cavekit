//go:build windows

package install

import (
	"os"
	"os/exec"
)

// linkDirPlatform creates a directory junction via `cmd /c mklink /J`.
// Junctions work without Developer Mode or Administrator; true symlinks
// would require one of those on Windows.
func linkDirPlatform(target, link string) error {
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	return cmd.Run()
}

// linkFilePlatform creates a hard link, which does not require elevated
// privileges when both files are on the same volume. Falls back to caller's
// copyFile if this fails.
func linkFilePlatform(target, link string) error {
	return os.Link(target, link)
}
