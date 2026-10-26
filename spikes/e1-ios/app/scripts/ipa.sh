#!/bin/sh
# Builds an unsigned Release .ipa for SideStore, which re-signs the app and its extension with
# the free Apple account (design D7). Run `npm run configure` with the https URL first.
set -eu
cd "$(dirname "$0")/.."

npx expo prebuild -p ios
xcodebuild -workspace ios/HippoE1.xcworkspace -scheme HippoE1 -configuration Release \
  -sdk iphoneos -destination 'generic/platform=iOS' -archivePath build/HippoE1.xcarchive \
  CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO CODE_SIGN_IDENTITY="" archive

rm -rf build/Payload build/HippoE1.ipa
mkdir -p build/Payload
cp -R build/HippoE1.xcarchive/Products/Applications/HippoE1.app build/Payload/
(cd build && zip -qry HippoE1.ipa Payload)
unzip -l build/HippoE1.ipa | grep -E 'HippoE1.app/Info.plist|HippoShare.appex/Info.plist'
echo "Built build/HippoE1.ipa"
