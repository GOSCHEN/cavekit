#!/usr/bin/env bash
# Deprecated shim — forwards to 'cavekit launch'.
# Removed in cavekit 2.2.0. Update callers to invoke cavekit directly.

printf '[deprecation] %s is a shim for %s. Update callers.\n' "cavekit-launch-session.sh" 'cavekit launch' >&2
exec cavekit launch "$@"
