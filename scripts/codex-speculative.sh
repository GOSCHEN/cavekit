#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit codex speculative'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "codex-speculative.sh" 'cavekit codex speculative' >&2
exec cavekit codex speculative "$@"
