#!/usr/bin/env bash
# Wails invokes frontend:build without shell interpretation (no && chaining),
# so the build + copy-into-frontend/dist steps live here instead.
set -euo pipefail
cd "$(dirname "$0")/.."
pnpm build
rm -rf src-wails/frontend/dist
cp -R dist src-wails/frontend/dist
