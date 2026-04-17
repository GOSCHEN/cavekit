# Windows Support — Punchlist

Full plan: [`WINDOWS_SUPPORT.md`](WINDOWS_SUPPORT.md)

## Done

- [x] **M1** Cross-compile green (windows/linux/darwin)
- [x] **M2** Multiplexer interface + Windows ConPTY daemon + wt.exe frontend
- [x] E2E integration test (`CAVEKIT_WINDOWS_E2E=1`) green

## M3: Port bash scripts to Go — ✅ shipped

Each task: golden-file parity tests vs existing `.sh`.

- [x] **T-004** `internal/config` — port `scripts/bp-config.sh` (498 LOC); expose as `cavekit config` (parity test via `CAVEKIT_PARITY=1`)
- [x] **T-008** `cavekit codex detect` ← `scripts/codex-detect.sh`
- [x] **T-009** `internal/codex` findings parser ← `scripts/codex-findings.sh` (exposed via `cavekit codex findings`)
- [x] **T-010** `cavekit codex review` ← `scripts/codex-review.sh`
- [x] **T-011** `cavekit codex gate` ← `scripts/codex-gate.sh`
- [x] **T-012** `cavekit codex speculative` ← `scripts/codex-speculative.sh`
- [x] **T-013** `cavekit codex design` ← `scripts/codex-design-challenge.sh` (553 LOC — biggest)
- [x] **T-014** `cavekit command-gate` ← `scripts/command-gate.sh` (PreToolUse hook)
- [x] **T-015** `cavekit setup-build` ← `scripts/setup-build.sh` (579 LOC)
- [x] **T-016** `cavekit install sync-codex` ← `scripts/sync-codex-plugin.sh` (junctions on Windows)
- [x] **T-017** `cavekit analytics|dashboard|poll` ← `scripts/cavekit-analytics.sh` + `dashboard-*.sh` + `cavekit-status-poller.sh`
- [x] **T-018** Bubbletea picker inside `cavekit picker` ← `scripts/cavekit-picker.ts` (drops Node/tsx dep)
- [x] **T-019** `cavekit launch` ← `scripts/cavekit-launch-session.sh`

## M4 — Plugin cut-over

- [ ] **T-020** Rewrite `commands/*.md` — `${CLAUDE_PLUGIN_ROOT}/scripts/x.sh` → `cavekit x`
- [ ] **T-021** Wire PreToolUse hook to `cavekit command-gate`
- [ ] **T-022** `scripts/*.sh` shims exec-ing `cavekit <subcommand>` (deprecation window)

## M5 — Installers

- [ ] **T-023** `install.ps1` (junctions, settings.json merge via ConvertFrom-Json)
- [ ] **T-024** Trim `install.sh` to bootstrap only
- [ ] **T-025** `cavekit install` cross-platform orchestrator

## M6 — Polish

- [ ] **T-026** Process tree kill (`taskkill /T /F` on Windows)
- [ ] **T-027** Replace SIGWINCH with Windows `ReadConsoleInput` resize events
- [ ] **T-028** CRLF tolerance in config readers
- [ ] **T-029** Lowercase-normalize worktree names on Windows compare
- [ ] **T-030** README Windows quickstart + limitations

## Open questions

- [ ] Exact wt.exe version floor to enforce via preflight (currently checks presence only)
- [ ] Whether to ship signed Windows binary in GitHub Releases (code-sign cert required)
- [ ] CI matrix timing — suspended; revisit before 2.1.0 tag
