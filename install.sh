#!/bin/bash
#
# Cavekit installer (Unix). Builds the cavekit Go binary, places it on PATH,
# then delegates the marketplace / settings.json / Codex link work to
# `cavekit install all`.
#
# Usage:
#   git clone https://github.com/JuliusBrussee/cavekit.git ~/.cavekit && ~/.cavekit/install.sh

set -euo pipefail

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${CAVEKIT_BIN_DIR:-/usr/local/bin}"

R=$'\033[0m' B=$'\033[1m' GR=$'\033[32m' YL=$'\033[33m' BL=$'\033[34m' RD=$'\033[31m'

info()  { printf "${BL}▸${R} %s\n" "$1"; }
ok()    { printf "${GR}■${R} %s\n" "$1"; }
warn()  { printf "${YL}!${R} %s\n" "$1"; }
fail()  { printf "${RD}✗${R} %s\n" "$1" >&2; exit 1; }

printf "\n${B}${BL}  ┌──────────────────────────┐${R}\n"
printf "${B}${BL}  │  C A V E K I T           │${R}\n"
printf "${B}${BL}  └──────────────────────────┘${R}\n"
printf "${B}Installer${R}\n\n"

# ─── Preflight ──────────────────────────────────────────────────────────────

command -v git &>/dev/null || fail "git not found."
command -v go &>/dev/null || fail "go not found. Install Go 1.22+ (https://go.dev/dl/)."
command -v claude &>/dev/null || warn "claude CLI not found. Install Claude Code to use /ck:... commands."
command -v codex &>/dev/null || warn "codex CLI not found. Codex local plugin sync will be skipped."
command -v tmux &>/dev/null || warn "tmux not found. Install for the parallel launcher."

# ─── Build the Go binary ────────────────────────────────────────────────────

info "Building cavekit binary..."
cd "$INSTALL_DIR"
go build -o "$INSTALL_DIR/cavekit" ./cmd/cavekit
ok "Built $INSTALL_DIR/cavekit"

# ─── Place on PATH ─────────────────────────────────────────────────────────

info "Installing cavekit to $BIN_DIR..."
if [[ -w "$BIN_DIR" ]]; then
  ln -sf "$INSTALL_DIR/cavekit" "$BIN_DIR/cavekit"
else
  sudo ln -sf "$INSTALL_DIR/cavekit" "$BIN_DIR/cavekit"
fi
ok "Linked cavekit into $BIN_DIR"

# ─── Delegate to cavekit install all ────────────────────────────────────────

info "Running cavekit install all..."
"$INSTALL_DIR/cavekit" install all

# ─── Done ───────────────────────────────────────────────────────────────────

printf "\n${B}${GR}Installed!${R}\n\n"
printf "  Restart Claude Code and Codex to load the plugin changes.\n\n"
