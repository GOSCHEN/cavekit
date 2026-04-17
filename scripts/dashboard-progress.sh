#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit dashboard progress'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "dashboard-progress.sh" 'cavekit dashboard progress' >&2
exec cavekit dashboard progress "$@"
