#!/bin/bash
set -euo pipefail

if [[ $# != 3 ]]; then
  echo "Usage: bash $0 <simulator-udid> <Storefrontnative.app> <derived-data-dir>" >&2
  exit 2
fi

test_dir="$(cd "$(dirname "$0")" && pwd)"
device=$1
app="$(cd "$2" && pwd)"
data=$3
id="$(plutil -extract CFBundleIdentifier raw "$app/Info.plist")"
if [[ $id != dev.gofastr.storefront ]]; then
  echo "The UI suite expects bundle ID dev.gofastr.storefront, got $id." >&2
  exit 2
fi

destination="platform=iOS Simulator,id=$device"
xcrun simctl install "$device" "$app"
xcodebuild build-for-testing -project "$test_dir/StorefrontTests.xcodeproj" \
  -scheme StorefrontTests -destination "$destination" -parallel-testing-enabled NO \
  -derivedDataPath "$data"

products="$data/Build/Products"
set -- "$products"/StorefrontTests_*.xctestrun
if [[ $# != 1 || ! -f $1 ]]; then
  echo "Expected one generated .xctestrun; choose a fresh derived-data directory." >&2
  exit 1
fi

# Give XCTest an app-under-test so SKTestSession targets its bundle, not the runner.
run="$products/StorefrontLocal.xctestrun"
cp "$1" "$run"
plutil -replace StorefrontTests.UseUITargetAppProvidedByTests -bool NO "$run"
plutil -insert StorefrontTests.UITargetAppPath -string "$app" "$run"
plutil -insert StorefrontTests.UITargetAppBundleIdentifier -string "$id" "$run"
xcodebuild test-without-building -xctestrun "$run" -destination "$destination" \
  -parallel-testing-enabled NO
