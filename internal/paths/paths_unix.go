//go:build !windows

package paths

// binDir is the Unix default install location.
func binDir() string {
	return "/usr/local/bin"
}
