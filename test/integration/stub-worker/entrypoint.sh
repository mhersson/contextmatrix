#!/bin/sh
# Stub-worker entrypoint. Mirrors the production worker's secrets-file
# discipline so CM_MCP_API_KEY (etc.) reach the binary as env vars.
set -e
if [ -f /run/cm-secrets/env ]; then
    set -a
    # shellcheck disable=SC1091
    . /run/cm-secrets/env
    set +a
fi
exec /usr/local/bin/claude "$@"
