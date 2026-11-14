#!/usr/bin/env bash
# Builds the app and its share extension for the iOS simulator, unsigned (design D13). The CI does
# not build the extension's target, so this build is part of the definition of done.
#
#   HIPPO_API_TOKEN=<32 characters or more> make build-ios-sim
#
# HIPPO_API_URL defaults to the local backend (http://127.0.0.1:8080). Prebuild and build run in
# one command with the same environment, so the app and the extension never get different values
# (design D1). The token is checked, never printed, by plugins/with-share-config.js.
set -euo pipefail
cd "$(dirname "$0")/.."

# CocoaPods refuses a terminal that is not in UTF-8.
export LANG="${LANG:-en_US.UTF-8}"
# prebuild --clean only rewrites the git-ignored ios/ folder: no need to check the git status.
export EXPO_NO_GIT_STATUS=1

npx --no-install expo prebuild --clean --platform ios
xcodebuild -workspace ios/Hippo.xcworkspace -scheme Hippo -configuration Debug \
  -destination 'generic/platform=iOS Simulator' -derivedDataPath build/DerivedData \
  CODE_SIGNING_ALLOWED=NO -quiet build

echo "Built app/build/DerivedData/Build/Products/Debug-iphonesimulator/Hippo.app"
