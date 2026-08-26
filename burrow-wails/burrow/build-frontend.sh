#!/usr/bin/env bash
# Wails invokes frontend:build without shell interpretation (no && chaining),
# so the build + copy-into-frontend/dist steps live here instead.
set -euo pipefail
cd "$(dirname "$0")/../.."
pnpm build
rm -rf burrow-wails/burrow/frontend/dist
cp -R dist burrow-wails/burrow/frontend/dist
