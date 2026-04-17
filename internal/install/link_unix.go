//go:build !windows

package install

import "os"

func linkDirPlatform(target, link string) error {
	return os.Symlink(target, link)
}

func linkFilePlatform(target, link string) error {
	return os.Symlink(target, link)
}
