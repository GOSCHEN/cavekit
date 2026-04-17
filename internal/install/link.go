package install

import (
	"io"
	"os"
	"path/filepath"
)

// linkDir creates a cross-platform link from link → target where target is a
// directory. On Unix this is a symlink; on Windows it is a directory
// junction (no admin privilege required, unlike NTFS symlinks). Pre-existing
// entries at link are replaced.
func linkDir(target, link string) error {
	if err := prepareLink(link); err != nil {
		return err
	}
	return linkDirPlatform(target, link)
}

// linkFile creates a link from link → target where target is a file. On
// Unix a symlink; on Windows a hard link (which does not require admin on
// file links). Falls back to copying when hard-link creation fails.
func linkFile(target, link string) error {
	if err := prepareLink(link); err != nil {
		return err
	}
	if err := linkFilePlatform(target, link); err == nil {
		return nil
	}
	return copyFile(target, link)
}

func prepareLink(link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	// Remove existing (symlink, junction, file, or dir).
	if _, err := os.Lstat(link); err == nil {
		// Try a plain Remove first; fall back to RemoveAll for directories
		// that no longer satisfy the symlink shape.
		if err := os.Remove(link); err != nil {
			if err := os.RemoveAll(link); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(target, link string) error {
	src, err := os.Open(target)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(link)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}
