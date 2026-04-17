#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit poll'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "cavekit-status-poller.sh" 'cavekit poll' >&2
exec cavekit poll "$@"
