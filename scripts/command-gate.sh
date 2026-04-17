#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit command-gate'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "command-gate.sh" 'cavekit command-gate' >&2
exec cavekit command-gate "$@"
