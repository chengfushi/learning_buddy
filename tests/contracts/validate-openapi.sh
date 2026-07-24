#!/usr/bin/env bash
set -euo pipefail

spec="docs/openapi.yaml"
test -s "$spec"
for path in /health /api/auth/login /api/auth/refresh /api/agent/chat /api/agent/plan /api/agent/quiz; do
  grep -q "^  ${path//\//\\/}:" "$spec"
done
grep -q "text/event-stream" "$spec"
grep -q "httpOnly cookie" "$spec"
