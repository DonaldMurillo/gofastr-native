#!/bin/bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
: "${SIMULATOR_UDID:?Set SIMULATOR_UDID to an installed iOS simulator UUID}"
OUT="${OUT:-$ROOT/dist/mobile/scanner-ui}"
export GOWORK=off
go run ./cmd/gofastr-native build --platform ios-sim --renderer native \
  --pkg ./examples/devicefeatures/featuresnative --id dev.example.devicefeatures \
  --name GofastrDeviceFeatures --camera-usage 'Scan a code when you choose Scan code.' --out "$OUT/app"
if ! xcrun simctl list devices booted | rg -Fq "$SIMULATOR_UDID"; then
  xcrun simctl boot "$SIMULATOR_UDID"
fi
xcrun simctl bootstatus "$SIMULATOR_UDID" -b
xcrun simctl install "$SIMULATOR_UDID" "$OUT/app/Featuresnative.app"
xcodebuild test -project examples/devicefeatures/uitests/ScannerTests.xcodeproj \
  -scheme ScannerTests -destination "platform=iOS Simulator,id=$SIMULATOR_UDID" \
  -derivedDataPath "$OUT/DerivedData" CODE_SIGNING_ALLOWED=NO
