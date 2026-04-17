#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit dashboard activity'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "dashboard-activity.sh" 'cavekit dashboard activity' >&2
exec cavekit dashboard activity "$@"
