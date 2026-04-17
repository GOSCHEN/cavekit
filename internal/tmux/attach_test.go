package tmux

import (
	"testing"

	"github.com/JuliusBrussee/cavekit/internal/exec"
)

func TestNewAttacher(t *testing.T) {
	mock := exec.NewMockExecutor()
	mgr := NewManager(mock)
	attacher := NewAttacher(mgr)
	if attacher == nil {
		t.Error("NewAttacher should not return nil")
	}
	if attacher.mgr != mgr {
		t.Error("attacher should reference the manager")
	}
}
