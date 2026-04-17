//go:build windows

package mux

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// sessionMeta describes a live daemon. One JSON file per session lives in
// sessionStateDir(). Daemons create the file at startup and delete it on
// exit. ListSessions scans the directory; Kill reads a single entry.
type sessionMeta struct {
	Name     string `json:"name"`      // sanitized session name (with SessionPrefix)
	PID      int    `json:"pid"`       // daemon process ID
	PipeName string `json:"pipe_name"` // full \\.\pipe\... path
	Program  string `json:"program"`   // program spawned inside the ConPTY
	WorkDir  string `json:"work_dir"`  // working directory of the child
}

// writeSessionMeta persists a metadata record for the current daemon.
func writeSessionMeta(m sessionMeta) error {
	dir := sessionStateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, m.Name+".json")
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// removeSessionMeta deletes the daemon's metadata file. Used on clean exit.
func removeSessionMeta(name string) {
	_ = os.Remove(filepath.Join(sessionStateDir(), SanitizeName(name)+".json"))
}

// readSessionMeta loads one daemon's metadata by sanitized name.
func readSessionMeta(name string) (sessionMeta, error) {
	var m sessionMeta
	data, err := os.ReadFile(filepath.Join(sessionStateDir(), SanitizeName(name)+".json"))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(data, &m)
}

// listSessionMetas returns every recorded daemon. Stale files (daemon died
// without cleanup) are returned as-is; callers should validate via a Ping
// before trusting the entry.
func listSessionMetas() ([]sessionMeta, error) {
	dir := sessionStateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []sessionMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var m sessionMeta
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if strings.HasPrefix(m.Name, SessionPrefix) {
			out = append(out, m)
		}
	}
	return out, nil
}
