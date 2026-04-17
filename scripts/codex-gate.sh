#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit codex gate'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "codex-gate.sh" 'cavekit codex gate' >&2
exec cavekit codex gate "$@"
