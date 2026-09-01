# Burrow (Go + Wails v2) — task runner.  Install `just`:  brew install just
# Run `just` with no args to list recipes.

set shell := ["bash", "-uc"]

# Apple notarization identity (NOT secret — the app-specific password lives in
# the login Keychain item BURROW_NOTARY_PWD, never here).
export APPLE_ID      := "bc.jakubgal@email.cz"
export APPLE_TEAM_ID := "9QY36KZ8JP"
profile := "BURROW_NOTARY"

# GitHub repo hosting releases + the updater manifest (latest.json).
# Must match updateRepo in src-wails/updater.go.
repo := "JBurdik/burrow-wails"

# Current app version, read live from wails.json (single source of truth;
# `just bump` rewrites package.json + src-wails/version.go to match).
version := `node -p "require('./src-wails/wails.json').info.productVersion" 2>/dev/null || echo 0.0.0`

app     := "src-wails/build/bin/Burrow.app"
tarball := "src-wails/build/bin/Burrow.app.tar.gz"
dmg     := "src-wails/build/bin/Burrow_" + version + "_aarch64.dmg"

# List recipes
default:
    @just --list

# Print the current version
version:
    @echo "{{version}}"

# Show where the built artifacts are
where:
    @echo "app: {{app}}"
    @echo "dmg: {{dmg}}"
    @echo "tar: {{tarball}}"

# ── dev ───────────────────────────────────────────────────────────────────────

# Native dev window (hot-reload)
dev:
    cd src-wails && wails dev

# Frontend only, in the browser (no Wails backend)
web:
    pnpm dev

# Type-check frontend + backend, run tests. No bundle.
check:
    pnpm vue-tsc --noEmit
    cd src-wails && go vet ./... && go build ./... && go test ./...

# ── build ─────────────────────────────────────────────────────────────────────

# Frontend production build, copied into src-wails/frontend/dist for the embed.
build-web:
    bash src-wails/build-frontend.sh

# Mobile remote/PWA bundle -> src-wails/dist-mobile/app, //go:embed-ed by
# httpserver.go and served at / when remote access is on.
build-mobile:
    pnpm build:mobile

# Full unsigned build: frontend + app bundle + the sidecar binaries inside it
# (burrow-daemon holds the PTYs; burrow-mcp serves the control verbs to agent
# clients that speak MCP).
# `wails build -s` skips Wails' own frontend step — build-web already ran it,
# and Wails runs frontend:build from a directory the script can't be found in.
build: build-web build-mobile
    #!/usr/bin/env bash
    set -euo pipefail
    cd src-wails
    wails build -platform darwin/arm64 -clean -s
    go build -o build/bin/Burrow.app/Contents/MacOS/burrow-daemon ./cmd/burrow-daemon
    go build -ldflags "-X main.buildVersion={{version}}" \
      -o build/bin/Burrow.app/Contents/MacOS/burrow-mcp ./cmd/burrow-mcp
    echo "built: {{app}}"

# Codesign the bundle with the Developer ID identity + hardened runtime.
# Inner binaries first, then the bundle (deep signing alone leaves the nested
# daemon unsigned in a way notarization rejects).
sign:
    #!/usr/bin/env bash
    set -euo pipefail
    ID="Developer ID Application: Jakub Gál ({{APPLE_TEAM_ID}})"
    ENT="src-wails/build/darwin/entitlements.plist"
    for bin in burrow-daemon burrow-mcp Burrow; do
      codesign --force --timestamp --options runtime --entitlements "$ENT" \
               --sign "$ID" "{{app}}/Contents/MacOS/$bin"
    done
    codesign --force --timestamp --options runtime --entitlements "$ENT" \
             --sign "$ID" "{{app}}"
    codesign --verify --deep --strict --verbose=2 "{{app}}"
    echo "signed: $ID"

# Fail loudly unless every Mach-O in the bundle carries a Developer ID
# signature with a hardened runtime. A running `wails dev` writes its own
# adhoc-signed binary over build/bin, so a bundle that passed `just sign`
# minutes ago can silently regress — Apple rejects it 5 minutes later with
# "not signed with a valid Developer ID certificate".
assert-signed:
    #!/usr/bin/env bash
    set -euo pipefail
    for bin in Burrow burrow-daemon burrow-mcp; do
      out="$(codesign -dv "{{app}}/Contents/MacOS/$bin" 2>&1)"
      grep -q "TeamIdentifier={{APPLE_TEAM_ID}}" <<<"$out" \
        || { echo "❌ $bin is not signed by team {{APPLE_TEAM_ID}} (is 'wails dev' running and overwriting build/bin?) — run 'just sign'"; exit 1; }
      grep -q "flags=.*runtime" <<<"$out" \
        || { echo "❌ $bin has no hardened runtime — run 'just sign'"; exit 1; }
    done
    codesign --verify --deep --strict "{{app}}"
    echo "signature check: ✓ Developer ID + hardened runtime"

# Notarize + staple the .app (submits a zip; the ticket is stapled to the app).
notarize-app: assert-signed
    #!/usr/bin/env bash
    set -euo pipefail
    ZIP="src-wails/build/bin/Burrow-notarize.zip"
    ditto -c -k --keepParent "{{app}}" "$ZIP"
    xcrun notarytool submit "$ZIP" --keychain-profile "{{profile}}" --wait
    xcrun stapler staple "{{app}}"
    rm -f "$ZIP"

# Build the .dmg from the (already signed + stapled) app.
dmg:
    #!/usr/bin/env bash
    set -euo pipefail
    STAGE="src-wails/build/bin/dmg-staging"
    rm -rf "$STAGE" "{{dmg}}"
    mkdir -p "$STAGE"
    cp -R "{{app}}" "$STAGE/"
    ln -s /Applications "$STAGE/Applications"
    hdiutil create -volname "Burrow" -srcfolder "$STAGE" -ov -format UDZO "{{dmg}}"
    rm -rf "$STAGE"
    # The dmg needs its own Developer ID signature, not just a notarization
    # ticket: Gatekeeper checks the disk image itself on download, and an
    # unsigned one is rejected with "no usable signature" even when stapled.
    codesign --force --timestamp --sign "Developer ID Application: Jakub Gál ({{APPLE_TEAM_ID}})" "{{dmg}}"
    echo "dmg: {{dmg}} (signed)"

# Notarize + staple the .dmg (Gatekeeper checks the dmg itself on download,
# so it needs its own ticket even though the app inside is already stapled).
notarize-dmg:
    xcrun notarytool submit "{{dmg}}" --keychain-profile "{{profile}}" --wait
    xcrun stapler staple "{{dmg}}"

# Pack the updater artifacts: Burrow.app.tar.gz + latest.json (with the sha256
# the in-app updater checks before installing — see src-wails/updater.go).
pack notes="": assert-signed
    #!/usr/bin/env bash
    set -euo pipefail
    [ -d "{{app}}" ] || { echo "❌ {{app}} missing — run 'just build' first"; exit 1; }
    rm -f "{{tarball}}"
    tar -czf "{{tarball}}" -C "$(dirname "{{app}}")" "$(basename "{{app}}")"
    VERIFY_DIR="$(mktemp -d)"
    trap 'rm -rf "$VERIFY_DIR"' EXIT
    tar -xzf "{{tarball}}" -C "$VERIFY_DIR"
    codesign --verify --deep --strict "$VERIFY_DIR/Burrow.app"
    SUM="$(shasum -a 256 "{{tarball}}" | awk '{print $1}')"
    NOTES="{{notes}}"
    [ -n "$NOTES" ] || NOTES="$(git log --pretty=format:'- %s' "$(git describe --tags --abbrev=0 2>/dev/null || git rev-list --max-parents=0 HEAD)..HEAD" 2>/dev/null | grep -v '^- release v' | head -30 || echo '- Maintenance release')"
    node -e '
      const fs = require("fs");
      const [version, sum, repo, notes] = process.argv.slice(1);
      fs.writeFileSync("src-wails/build/bin/latest.json", JSON.stringify({
        version,
        notes,
        pub_date: new Date().toISOString().replace(/\.\d+Z$/, "Z"),
        platforms: {
          "darwin-aarch64": {
            url: `https://github.com/${repo}/releases/download/v${version}/Burrow.app.tar.gz`,
            sha256: sum,
          },
        },
      }, null, 2) + "\n");
    ' "{{version}}" "$SUM" "{{repo}}" "$NOTES"
    echo "packed: {{tarball}} (sha256 $SUM) + latest.json"

# ── release ───────────────────────────────────────────────────────────────────

# Bump the version in lockstep across wails.json, package.json and
# src-wails/version.go.  Usage:  just bump [patch|minor|major]
# Prints the new version on stdout (consumed by `release`).
bump level="patch":
    #!/usr/bin/env bash
    set -euo pipefail
    node -e '
      const fs = require("fs");
      const lvl = process.argv[1] || "patch";
      const confPath = "src-wails/wails.json";
      const conf = JSON.parse(fs.readFileSync(confPath, "utf8"));
      let [a, b, c] = conf.info.productVersion.split(".").map(Number);
      if (lvl === "major") { a++; b = 0; c = 0; }
      else if (lvl === "minor") { b++; c = 0; }
      else { c++; }
      const v = `${a}.${b}.${c}`;
      conf.info.productVersion = v;
      fs.writeFileSync(confPath, JSON.stringify(conf, null, 2) + "\n");
      const pkgPath = "package.json";
      const pkg = JSON.parse(fs.readFileSync(pkgPath, "utf8"));
      pkg.version = v;
      fs.writeFileSync(pkgPath, JSON.stringify(pkg, null, 2) + "\n");
      const goPath = "src-wails/version.go";
      const go = fs.readFileSync(goPath, "utf8")
        .replace(/const appVersion = "[^"]*"/, `const appVersion = "${v}"`);
      fs.writeFileSync(goPath, go);
      console.error(`bumped ${lvl}: ${v}`);
      process.stdout.write(v);
    ' {{level}}

# Cut a full release: bump → build → sign → notarize app → dmg → pack updater
# artifacts → commit + tag + push → publish the GitHub release. The updater
# endpoint (releases/latest/download/latest.json) always serves the newest one.
# Usage:  just release           (patch bump)
#         just release minor
release level="patch":
    #!/usr/bin/env bash
    set -euo pipefail
    command -v gh >/dev/null || { echo "❌ gh CLI not found (brew install gh)"; exit 1; }
    security find-generic-password -s BURROW_NOTARY_PWD >/dev/null 2>&1 \
      || { echo "❌ BURROW_NOTARY_PWD missing from the Keychain — run 'just notary-creds <pwd>'"; exit 1; }
    git diff --quiet || { echo "❌ working tree dirty — commit or stash first"; exit 1; }

    NEW="$(just bump {{level}})"
    TAG="v$NEW"
    echo "▶ releasing $TAG"

    NOTES="$(git log --pretty=format:'- %s' "$(git describe --tags --abbrev=0 2>/dev/null || git rev-list --max-parents=0 HEAD)..HEAD" 2>/dev/null | grep -v '^- release v' | head -30 || echo '- Maintenance release')"

    just build
    just sign
    just notarize-app
    just dmg
    just notarize-dmg
    just pack "$NOTES"

    DMG="src-wails/build/bin/Burrow_${NEW}_aarch64.dmg"
    [ -f "$DMG" ] || { echo "❌ dmg not found: $DMG"; exit 1; }

    git add package.json src-wails/wails.json src-wails/version.go
    git commit -m "release $TAG"
    git tag "$TAG"
    git push origin HEAD
    git push origin "$TAG"

    gh release create "$TAG" \
        "$DMG" "{{tarball}}" src-wails/build/bin/latest.json \
        --repo "{{repo}}" --title "$TAG" --notes "$NOTES"

    echo "✅ released $TAG → https://github.com/{{repo}}/releases/tag/$TAG"
    echo "   in-app updater will pick it up on the next check."

# ── verification ──────────────────────────────────────────────────────────────

# Full signing/notarization/Gatekeeper check on the .app and .dmg.
verify:
    #!/usr/bin/env bash
    set -uo pipefail
    hr(){ printf '\n==== %s ====\n' "$1"; }
    hr "codesign --verify (deep, strict)"
    codesign --verify --deep --strict --verbose=2 "{{app}}" 2>&1 | tail -2
    hr "signature + hardened runtime"
    codesign -dvvv "{{app}}" 2>&1 | grep -E "Authority=|TeamIdentifier=|flags=|Runtime Version" | head
    hr "nested binaries"
    for b in Burrow burrow-daemon burrow-mcp; do
      printf '%-14s ' "$b"; codesign --verify --strict "{{app}}/Contents/MacOS/$b" && echo ok
    done
    hr "Gatekeeper — app (exec)"
    spctl -a -vvv -t exec "{{app}}" 2>&1 | head -3
    hr "Gatekeeper — dmg (install)"
    spctl -a -vvv -t install "{{dmg}}" 2>&1 | head -2
    hr "staple tickets"
    xcrun stapler validate "{{app}}" 2>&1 | tail -1
    xcrun stapler validate "{{dmg}}" 2>&1 | tail -1
    hr "quarantine-sim (a downloaded copy)"
    T=$(mktemp -d); cp -R "{{app}}" "$T/"; xattr -w com.apple.quarantine "0083;0;Safari;" "$T/Burrow.app"
    spctl -a -vvv -t exec "$T/Burrow.app" 2>&1 | head -2; rm -rf "$T"

# One-time: store the app-specific password in the Keychain + notarytool profile.
#   just notary-creds 'xxxx-xxxx-xxxx-xxxx'
notary-creds password:
    security add-generic-password -s BURROW_NOTARY_PWD -a "{{APPLE_ID}}" -w "{{password}}" -U
    xcrun notarytool store-credentials "{{profile}}" \
        --apple-id "{{APPLE_ID}}" --team-id "{{APPLE_TEAM_ID}}" --password "{{password}}"
