#!/bin/sh
# Serve the dashboard against a throwaway copy of the synthetic demo brain.
# Used by Playwright's webServer; the fixture itself is never modified.
set -eu
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PORT=${KAI_E2E_PORT:-9234}
WORK=$(mktemp -d "${TMPDIR:-/tmp}/kai-e2e-XXXXXX")
trap 'rm -rf "$WORK"' EXIT INT TERM

cp -R "$ROOT/tests/fixtures/demo-brain" "$WORK/brain"
cat > "$WORK/config.yaml" <<YAML
agent:
  name: Demo
brain:
  path: $WORK/brain
  embeddings:
    provider: lsi
sessions_path: $WORK/sessions
server:
  listen_addr: 127.0.0.1:$PORT
YAML

"$ROOT/kai" --config "$WORK/config.yaml" reindex
"$ROOT/kai" --config "$WORK/config.yaml" dashboard --port "$PORT" --no-open
