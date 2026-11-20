#!/bin/zsh
# Development-signed IPA for over-the-air install: the Burrow core serves it at
# https://<mac>.ts.net[:port]/ios (src-wails/iosinstall.go). Signed with team
# 9QY36KZ8JP's wildcard dev profile already on this Mac, so only its devices install it;
# a new device = add it at developer.apple.com → Devices and refresh that profile.
set -euo pipefail
cd "$(dirname "$0")"
TEAM=${TEAM:-9QY36KZ8JP}
# Jakub Gál's Apple Development cert + the team's wildcard profile (same pair `just ios` uses).
IDENTITY=${IDENTITY:-DF4CCBA4F69D30AC9FB6A092DB5AB32217DEFDF7}
PROFILE=${PROFILE:-"$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles/676ff5a2-9942-40d8-95ed-5dcfbdb5f1d4.mobileprovision"}
# Push (APNs): the wildcard profile cannot carry aps-environment. Set APP_PROFILE
# to a development profile for the explicit App ID com.agenticide.burrow.ios
# (Push Notifications capability on) and the app is signed with it + the
# entitlement; extensions stay on the wildcard. Unset = no push, as before.
APP_PROFILE=${APP_PROFILE:-}
OUT="$HOME/Library/Application Support/burrow-wails${BURROW_DATA_SUFFIX:+-$BURROW_DATA_SUFFIX}/ios"
BUILD=$(date +%Y%m%d%H%M)
TMP=build/release
rm -rf $TMP && mkdir -p $TMP "$OUT"
xcodegen generate -q
xcodebuild build -project Burrow.xcodeproj -scheme Burrow -configuration Release -destination 'generic/platform=iOS' \
	-derivedDataPath $TMP/dd CURRENT_PROJECT_VERSION=$BUILD CODE_SIGNING_ALLOWED=NO > $TMP/build.log 2>&1 \
	|| { grep -E 'error' $TMP/build.log | head -20; exit 1; }
APP=$TMP/ipa/Payload/Burrow.app
mkdir -p $TMP/ipa/Payload
cp -R $TMP/dd/Build/Products/Release-iphoneos/Burrow.app $APP
# Sign inside-out: extensions, then the app. Entitlements spelled out, since
# the unsigned build left $(AppIdentifierPrefix) unexpanded.
sign() {
	local bundle=$1 profile=${2:-$PROFILE} aps=${3:-} id=$(/usr/libexec/PlistBuddy -c 'Print CFBundleIdentifier' "$1/Info.plist")
	cat > $TMP/ent.plist <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>application-identifier</key><string>$TEAM.$id</string>
<key>com.apple.developer.team-identifier</key><string>$TEAM</string>
<key>get-task-allow</key><true/>
<key>keychain-access-groups</key><array><string>$TEAM.com.agenticide.burrow.shared</string></array>
${aps:+<key>aps-environment</key><string>development</string>}
</dict></plist>
PL
	cp "$profile" "$bundle/embedded.mobileprovision"
	codesign --force --timestamp=none --sign "$IDENTITY" --entitlements $TMP/ent.plist "$bundle"
}
for ext in $APP/PlugIns/*.appex; do sign "$ext"; done
if [[ -n $APP_PROFILE ]]; then sign $APP "$APP_PROFILE" 1; else sign $APP; fi
codesign --verify --deep --strict $APP
(cd $TMP/ipa && zip -qry Burrow.ipa Payload)
cp $TMP/ipa/Burrow.ipa "$OUT/Burrow.ipa"
echo $BUILD > "$OUT/version"
ls -lh "$OUT/Burrow.ipa"
echo "Install: open https://<this-mac>.ts.net/ios in Safari on the iPhone"
