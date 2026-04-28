#!/bin/bash
# Stub orchestrated entrypoint — mirrors cm-orchestrated:test's
# behaviour: bootstrap workspace + MCP config (if needed), then sleep
# forever so the runner can spawn claude via Docker exec.
set -euo pipefail

# /workspace is bind-mounted by the runner; nothing else to bootstrap
# in stub mode (no real git ops).

echo "stub-orchestrated worker ready: card=${CM_CARD_ID:-?} project=${CM_PROJECT:-?}"

# sleep infinity. The runner kills us when the FSM finishes.
exec sleep infinity
