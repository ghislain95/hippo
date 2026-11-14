#!/usr/bin/env bash
# Builds the unsigned Release .ipa of the app and its share extension, for SideStore, which signs
# both with the free Apple account (design D13, docs/runbook-ios.md).
#
#   HIPPO_API_URL=https://<server> HIPPO_API_TOKEN=<token> make ipa
#
# Outputs app/build/Hippo.ipa, and app/build/Hippo.ipa.zip to send by AirDrop (an .ipa sent alone
# is opened by iOS as an app to install). Prebuild and build run in one command with the same
# environment (design D1); plugins/with-share-config.js refuses a missing token or an http URL.
set -euo pipefail
cd "$(dirname "$0")/.."

case "${HIPPO_API_URL:-}" in
  https://*) ;;
  *)
    echo "HIPPO_API_URL must be the https URL of the server for a device build." >&2
    exit 1
    ;;
esac

# CocoaPods refuses a terminal that is not in UTF-8.
export LANG="${LANG:-en_US.UTF-8}"
# prebuild --clean only rewrites the git-ignored ios/ folder: no need to check the git status.
export EXPO_NO_GIT_STATUS=1

npx --no-install expo prebuild --clean --platform ios
rm -rf build/Hippo.xcarchive build/Payload build/Hippo.ipa build/Hippo.ipa.zip
xcodebuild -workspace ios/Hippo.xcworkspace -scheme Hippo -configuration Release \
  -destination 'generic/platform=iOS' -archivePath build/Hippo.xcarchive \
  -derivedDataPath build/DerivedData \
  CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO CODE_SIGN_IDENTITY="" -quiet archive

mkdir -p build/Payload
cp -R build/Hippo.xcarchive/Products/Applications/Hippo.app build/Payload/
if [ ! -d build/Payload/Hippo.app/PlugIns/HippoShare.appex ]; then
  echo "The share extension is missing from Hippo.app (PlugIns/HippoShare.appex)." >&2
  exit 1
fi
(cd build && zip -qry Hippo.ipa Payload && zip -q Hippo.ipa.zip Hippo.ipa)
rm -rf build/Payload

echo "Built app/build/Hippo.ipa and app/build/Hippo.ipa.zip (AirDrop)"
