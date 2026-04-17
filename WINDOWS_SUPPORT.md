# Windows Support Plan

Status: **M1 + M2 landed** (commit `3bf54e9`). M3+ pending.

## Decisions

| # | Decision |
|---|----------|
| Target runtime | Rewrite bash scripts as Go subcommands of `cavekit` — single cross-platform binary |
| Multiplexer | Replace tmux with `wt.exe` (Windows Terminal 1.18+) backed by a cavekit-owned ConPTY daemon |
| Claude Code target | Native Windows (no WSL assumption) |
| Scope | Full parity: monitor TUI + parallel launcher + all slash commands |
| CI | Deferred — manual Windows 11/Win 10 1809+ smoke tests for now |
| Install | Add `install.ps1` alongside `install.sh`; both call a cross-platform `cavekit install` subcommand |
| Back-compat | Preserve Unix behavior via build tags and thin adapter layers |
| wt minimum | 1.18 |
| Windows minimum | Win 10 1809+ (ConPTY requirement) |

## Architecture

```
┌─ cmd/cavekit/main.go ─────────────────────────────────┐
│  monitor status kill version debug reset              │
│  (Windows only) mux-daemon mux-attach                 │
│  (future)  config codex-review setup-build detect     │
│            findings gate speculative command-gate     │
│            picker launch-session analytics poller     │
│            sync-codex install                         │
└────────┬──────────────────────────────────────────────┘
         │
┌────────▼─────────────────────────────────────────────┐
│  internal/mux   Multiplexer interface                │
│    ├── factory_unix.go      → NewTmuxAdapter         │
│    ├── factory_windows.go   → newWTMultiplexer       │
│    ├── tmux_adapter.go      (Unix production +       │
│    │                         test wiring on any OS)  │
│    └── wt_*_windows.go      (daemon + pipe + ringbuf │
│                              + keys + session meta)  │
│                                                       │
│  internal/tmux                                        │
│    (Unix production; also compiles on Windows as a   │
│     test target; attach/terminal are tagged !windows) │
│                                                       │
│  internal/paths   ClaudeDir / CavekitDir / TempDir /  │
│                   BinDir / SessionsDir / Marketplace  │
│                                                       │
│  internal/session   AutoYes + lifecycle               │
│  internal/site      build-site parsing                │
│  internal/worktree  git worktree ops                  │
│  internal/tui       Bubbletea UI                      │
└───────────────────────────────────────────────────────┘
```

### Windows multiplexer runtime

```
Parent (cavekit monitor)
  │
  │ CreateSession(name, dir, prog)
  ▼
wtMultiplexer
  ├── spawn detached: cavekit.exe mux-daemon --name <n> --dir <d> --prog <p>
  │       └── Daemon owns: ConPTY child, ring buffer, named pipe listener
  │           Metadata: %USERPROFILE%\.cavekit\sessions\<name>.json
  │
  ├── waitForPipe  (winio.DialPipe ping loop, 5s timeout)
  │
  └── spawn: wt.exe new-tab --title <n> cavekit.exe mux-attach --name <n>
          └── attach client dials pipe, opAttach frame, then io.Copy both ways

SendKeys / Capture / Kill
  └── short-lived pipe connection, 5-byte framed request, single reply
```

## Milestones

| Milestone | Tasks | Gate | Status |
|-----------|-------|------|--------|
| **M1** Cross-compile green | T-001, T-002, T-003 | `GOOS=windows go build ./...` passes, tests green | ✅ shipped |
| **M2** Windows TUI + multiplexer | T-005a, T-005b, T-005c, T-007 | E2E test green on Windows 11 | ✅ shipped |
| **M3** Go-native plugin backend | T-004, T-008..T-019 | Golden-file parity tests vs shell | ✅ shipped |
| **M4** Plugin cut-over | T-020..T-022 | Slash commands call `cavekit` not `.sh` | ✅ shipped |
| **M5** Installers | T-023..T-025 | Clean-VM installs both OSes | ✅ shipped |
| **M6** Polish | T-026..T-030 | Docs + 2.1.0 release | ✅ shipped |

## Task index

### Tier 0 — Foundations (done)

- **T-001** ✅ Mux interface + tmux split (`internal/tmux` + `internal/mux/tmux_adapter.go`)
- **T-002** ✅ PTY abstraction (merged into T-001 via `golang.org/x/term`)
- **T-003** ✅ `internal/paths` package

### Tier 1 — Multiplexer impls (done)

- **T-005a** ✅ Extract `Multiplexer` interface + factory
- **T-005b** ✅ Windows ConPTY daemon + named pipe + wt.exe spawn
- **T-005c** ✅ Migrate all callers to the interface
- **T-006** ✅ tmux parity preserved (Unix tests still green)
- **T-007** ✅ Cross-platform `StatusDetector` working over `Multiplexer`

### Tier 2 — Port scripts to Go (pending)

- **T-004** ✅ Config package (`scripts/bp-config.sh` 498 LOC → `internal/config` + `cavekit config`)
- **T-008** ✅ `codex-detect.sh` → `cavekit codex detect`
- **T-009** ✅ `codex-findings.sh` → `internal/codex/findings.go`
- **T-010** ✅ `codex-review.sh` → `cavekit codex review`
- **T-011** ✅ `codex-gate.sh` → `cavekit codex gate`
- **T-012** ✅ `codex-speculative.sh` → `cavekit codex speculative`
- **T-013** ✅ `codex-design-challenge.sh` (553 LOC) → `cavekit codex design`
- **T-014** ✅ `command-gate.sh` → `cavekit command-gate` (PreToolUse hook)
- **T-015** ✅ `setup-build.sh` (579 LOC) → `cavekit setup-build`
- **T-016** ✅ `sync-codex-plugin.sh` → `cavekit install sync-codex` (junctions on Windows)
- **T-017** ✅ `cavekit-analytics.sh`, `dashboard-*.sh`, `cavekit-status-poller.sh` → `cavekit analytics|dashboard|poll`
- **T-018** ✅ Replace Node.js `cavekit-picker.ts` with Bubbletea picker inside `cavekit`
- **T-019** ✅ `cavekit-launch-session.sh` → `cavekit launch`

Per task: golden-file parity tests — feed same inputs to shell and Go impl, diff output modulo whitespace.

### Tier 3 — Plugin wiring (pending)

- **T-020** ✅ Rewrite `commands/*.md` — replace `${CLAUDE_PLUGIN_ROOT}/scripts/xxx.sh` with `cavekit xxx`
- **T-021** ✅ Plugin hooks → `cavekit command-gate` (via `hooks.json`)
- **T-022** ✅ Thin `scripts/*.sh` shims that `exec cavekit ...` (deprecation window, deletable after a release)

### Tier 4 — Installers (pending)

- **T-023** ✅ `install.ps1`: verify wt.exe + git + claude, install binary to `%LOCALAPPDATA%\Programs\cavekit`, directory junctions for marketplace, settings.json merge via `ConvertFrom-Json`
- **T-024** ✅ `install.sh`: trim to bootstrap, delegate to `cavekit install`
- **T-025** ✅ `cavekit install` subcommand — cross-platform orchestrator

### Tier 5 — Polish (pending)

- **T-026** ✅ Process tree kill (`taskkill /T /F` on Windows, tmux kill-session on Unix)
- **T-027** ✅ Console-size polling → `opResize` frame (replaces SIGWINCH dependency)
- **T-028** ✅ Line endings — config/site/build/stats/gate readers tolerate CRLF
- **T-029** ✅ Case-folded worktree name comparison on Windows/macOS
- **T-030** ✅ README Windows quickstart + limitations table

## Risk register

| Risk | Mitigation |
|------|-----------|
| `wt.exe` lacks programmatic SendKeys | Daemon owns PTY; wt is display only. Implemented. |
| ConPTY quirks on older Windows | Require Win 10 1809+. Daemon gates on `conpty.IsConPtyAvailable()`. |
| Symlink privilege on Windows | Use junctions for dirs (no admin), hard links for files, copy fallback. |
| 4700 LOC of bash to port | Per-script golden-file parity tests + shim layer during transition. |
| Existing users break during cut-over | `scripts/*.sh` shims re-exec `cavekit <subcommand>` for one release. |
| `go 1.26.1` bleeding edge | Bump only if required; most deps happy on 1.22+. |

## Deliverables

- `cavekit.exe` (windows/amd64) + `cavekit` (linux/amd64, darwin/amd64, darwin/arm64)
- `install.ps1` + slimmed `install.sh`
- Updated `plugin.json`, `commands/*.md`, `.claude/` hook config
- README Windows quickstart + limitations section
- Migration notes for users (script paths → subcommands)
