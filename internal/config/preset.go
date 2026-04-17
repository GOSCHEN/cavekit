package config

import (
	"fmt"
	"strings"
)

// Preset names the model-tier bundles callers resolve via Model. Mirrors the
// preset column in `bp_config_presets`.
type Preset string

const (
	PresetExpensive Preset = "expensive"
	PresetQuality   Preset = "quality"
	PresetBalanced  Preset = "balanced"
	PresetFast      Preset = "fast"
)

// TaskType names the axis along which a preset resolves to a concrete model.
type TaskType string

const (
	TaskReasoning   TaskType = "reasoning"
	TaskExecution   TaskType = "execution"
	TaskExploration TaskType = "exploration"
)

var presetTable = map[Preset]map[TaskType]string{
	PresetExpensive: {TaskReasoning: "opus", TaskExecution: "opus", TaskExploration: "opus"},
	PresetQuality:   {TaskReasoning: "opus", TaskExecution: "opus", TaskExploration: "sonnet"},
	PresetBalanced:  {TaskReasoning: "opus", TaskExecution: "sonnet", TaskExploration: "haiku"},
	PresetFast:      {TaskReasoning: "sonnet", TaskExecution: "sonnet", TaskExploration: "haiku"},
}

// EffectivePreset resolves bp_model_preset and validates it.
func (s *Store) EffectivePreset() (Preset, error) {
	raw := s.GetWithDefault("bp_model_preset", string(PresetQuality))
	switch Preset(raw) {
	case PresetExpensive, PresetQuality, PresetBalanced, PresetFast:
		return Preset(raw), nil
	}
	return "", fmt.Errorf("invalid preset %q (allowed: expensive quality balanced fast)", raw)
}

// Model returns the resolved model for the given task type under the
// currently effective preset.
func (s *Store) Model(task TaskType) (string, error) {
	preset, err := s.EffectivePreset()
	if err != nil {
		return "", err
	}
	return ResolveModel(preset, task)
}

// ResolveModel returns the model string for (preset, task) directly.
func ResolveModel(preset Preset, task TaskType) (string, error) {
	tasks, ok := presetTable[preset]
	if !ok {
		return "", fmt.Errorf("unknown preset %q", preset)
	}
	model, ok := tasks[task]
	if !ok {
		return "", fmt.Errorf("unknown task type %q (allowed: reasoning execution exploration)", task)
	}
	return model, nil
}

// SummaryLine formats the one-line preset summary used by shell prompts.
func (s *Store) SummaryLine() (string, error) {
	preset, err := s.EffectivePreset()
	if err != nil {
		return "", err
	}
	reasoning, err := s.Model(TaskReasoning)
	if err != nil {
		return "", err
	}
	execution, err := s.Model(TaskExecution)
	if err != nil {
		return "", err
	}
	exploration, err := s.Model(TaskExploration)
	if err != nil {
		return "", err
	}
	caveman := s.GetWithDefault("caveman_mode", "on")
	return fmt.Sprintf(
		"Cavekit preset: %s (reasoning=%s, execution=%s, exploration=%s, caveman=%s)",
		preset, reasoning, execution, exploration, caveman,
	), nil
}

// CavemanActive reports whether caveman mode applies to the given phase.
func (s *Store) CavemanActive(phase string) (bool, error) {
	if !isKnownPhase(phase) {
		return false, fmt.Errorf("invalid phase %q (allowed: build,inspect,draft,architect)", phase)
	}
	if s.GetWithDefault("caveman_mode", "on") != "on" {
		return false, nil
	}
	phases := s.GetWithDefault("caveman_phases", "build,inspect")
	for _, p := range strings.Split(phases, ",") {
		if p == phase {
			return true, nil
		}
	}
	return false, nil
}

// PresetTable renders the built-in preset table as Markdown. Matches the
// output of `bp_config_presets`.
func PresetTable() string {
	return strings.Join([]string{
		"| Preset | Reasoning | Execution | Exploration |",
		"|---|---|---|---|",
		"| expensive | opus | opus | opus |",
		"| quality | opus | opus | sonnet |",
		"| balanced | opus | sonnet | haiku |",
		"| fast | sonnet | sonnet | haiku |",
		"",
	}, "\n")
}
