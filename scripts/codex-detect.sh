#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit codex detect'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "codex-detect.sh" 'cavekit codex detect' >&2
exec cavekit codex detect "$@"
