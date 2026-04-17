package session

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/JuliusBrussee/cavekit/internal/paths"
)

// Store handles saving and loading instance state.
type Store struct {
	path string
}

// NewStore creates a persistence store at the given path.
// If empty, uses the canonical ~/.cavekit/state.json location.
func NewStore(path string) *Store {
	if path == "" {
		path = paths.StateFile()
	}
	return &Store{path: path}
}

// savedState is the JSON structure for persistence.
type savedState struct {
	Instances []*Instance `json:"instances"`
}

// Save persists the current instances to disk.
func (s *Store) Save(instances []*Instance) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	state := savedState{Instances: instances}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0644)
}

// Load restores instances from disk.
// Returns empty slice if file doesn't exist.
func (s *Store) Load() ([]*Instance, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state savedState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return state.Instances, nil
}

// Path returns the store's file path.
func (s *Store) Path() string {
	return s.path
}
