#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit codex findings'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "codex-findings.sh" 'cavekit codex findings' >&2
exec cavekit codex findings "$@"
