#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit install sync-codex'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "sync-codex-plugin.sh" 'cavekit install sync-codex' >&2
exec cavekit install sync-codex "$@"
