package worktree

import (
	"runtime"
	"testing"
)

func TestFoldEqualMatchesFilesystem(t *testing.T) {
	a, b := "MyProject", "myproject"
	want := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	if got := foldEqual(a, b); got != want {
		t.Errorf("foldEqual(%q,%q) = %v, want %v on %s", a, b, got, want, runtime.GOOS)
	}
	if !foldEqual("same", "same") {
		t.Error("identical strings should match regardless of OS")
	}
}

func TestHasFoldedPrefix(t *testing.T) {
	if !hasFoldedPrefix("myproject-cavekit-foo", "myproject-cavekit-") {
		t.Error("exact prefix should match")
	}
	got := hasFoldedPrefix("MyProject-cavekit-foo", "myproject-cavekit-")
	want := runtime.GOOS == "windows" || runtime.GOOS == "darwin"
	if got != want {
		t.Errorf("mixed-case prefix = %v, want %v on %s", got, want, runtime.GOOS)
	}
}

func TestTrimFoldedPrefixPreservesCaseOfRemainder(t *testing.T) {
	got := trimFoldedPrefix("MyProject-cavekit-FooBar", "myproject-cavekit-")
	if caseInsensitiveFS() {
		if got != "FooBar" {
			t.Errorf("got %q, want FooBar", got)
		}
	} else {
		if got != "MyProject-cavekit-FooBar" {
			t.Errorf("on case-sensitive FS, got %q, want unchanged input", got)
		}
	}
}
