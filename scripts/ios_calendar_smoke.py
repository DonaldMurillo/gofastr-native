"""Opt-in calendar acceptance against an installed native simulator build.

GOFASTR_IOS_CALENDAR_UI=1 python3 scripts/ios_calendar_smoke.py \
    --app /path/to/Featuresnative.app --device <booted-UDID> --out /path/to/results

Uses a standalone XCUITest runner, without changing the delivered app. Grants
calendar permission and leaves the sample event in the simulator's calendar.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys


def run(*args: str, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--app", type=Path, required=True)
    parser.add_argument("--device", required=True, help="explicit booted simulator UDID")
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    if os.environ.get("GOFASTR_IOS_CALENDAR_UI") != "1":
        print("SKIP: set GOFASTR_IOS_CALENDAR_UI=1 to run simulator calendar acceptance")
        return 0
    if sys.platform != "darwin" or not shutil.which("xcrun"):
        print("SKIP: Xcode and an iOS simulator are required")
        return 0
    tools = subprocess.run(["xcrun", "--find", "xcodebuild"], capture_output=True)
    if tools.returncode:
        print("SKIP: Xcode is unavailable")
        return 0
    devices = json.loads(subprocess.check_output(["xcrun", "simctl", "list", "devices", "booted", "--json"]))
    if not any(device["udid"] == args.device and device["isAvailable"]
               for group in devices["devices"].values() for device in group):
        print("SKIP: the selected iOS simulator is not booted and available")
        return 0
    app = args.app.resolve()
    with (app / "Info.plist").open("rb") as file:
        info = plistlib.load(file)
    bundle = "dev.gofastr.featuresnative"
    if info["CFBundleIdentifier"] != bundle or not info.get("GofastrCalendar"):
        raise ValueError("Build featuresnative with --calendar and its default bundle identifier")
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=True)
    project = out / "CalendarUITests.xcodeproj"
    project.mkdir(exist_ok=True)
    (project / "project.pbxproj").write_text(PROJECT)
    shutil.copyfile(Path(__file__).with_name("ios-calendar-ui") / "CalendarUITests.swift", out / "CalendarUITests.swift")
    schemes = project / "xcshareddata" / "xcschemes"
    schemes.mkdir(parents=True, exist_ok=True)
    (schemes / "CalendarUITests.xcscheme").write_text(SCHEME)
    run("xcrun", "simctl", "install", args.device, str(app))
    run("xcrun", "simctl", "privacy", args.device, "grant", "calendar", bundle)
    # Each invocation gets a separate result bundle; no prior evidence is erased.
    command = ["xcrun", "xcodebuild", "test", "-project", str(project),
               "-scheme", "CalendarUITests", "-destination", "platform=iOS Simulator,id=" + args.device,
               "-derivedDataPath", str(out / "DerivedData"),
               "-parallel-testing-enabled", "NO", "CODE_SIGNING_ALLOWED=NO",
               "ALWAYS_SEARCH_USER_PATHS=NO", "ONLY_ACTIVE_ARCH=YES"]
    run(*command)
    print("PASS: calendar add/read found GoFastr sample event; status reports full", flush=True)
    return 0


PROJECT = """
// !$*UTF8*$!
{
 archiveVersion = 1;
 classes = {};
 objectVersion = 56;
 objects = {
  A00000000000000000000001 = {isa = PBXProject; buildConfigurationList = A00000000000000000000002; compatibilityVersion = "Xcode 14.0"; developmentRegion = en; hasScannedForEncodings = 0; knownRegions = (en, Base); mainGroup = A00000000000000000000003; productRefGroup = A00000000000000000000004; projectDirPath = ""; projectRoot = ""; targets = (A00000000000000000000005); };
  A00000000000000000000002 = {isa = XCConfigurationList; buildConfigurations = (A00000000000000000000006); defaultConfigurationIsVisible = 0; defaultConfigurationName = Debug; };
  A00000000000000000000003 = {isa = PBXGroup; children = (A00000000000000000000007, A00000000000000000000004); sourceTree = "<group>"; };
  A00000000000000000000004 = {isa = PBXGroup; children = (A00000000000000000000008); name = Products; sourceTree = "<group>"; };
  A00000000000000000000005 = {isa = PBXNativeTarget; buildConfigurationList = A00000000000000000000009; buildPhases = (A0000000000000000000000A, A0000000000000000000000B); buildRules = (); dependencies = (); name = CalendarUITests; productName = CalendarUITests; productReference = A00000000000000000000008; productType = "com.apple.product-type.bundle.ui-testing"; };
  A00000000000000000000006 = {isa = XCBuildConfiguration; buildSettings = {SDKROOT = iphonesimulator; IPHONEOS_DEPLOYMENT_TARGET = 16.0; }; name = Debug; };
  A00000000000000000000007 = {isa = PBXFileReference; lastKnownFileType = sourcecode.swift; path = CalendarUITests.swift; sourceTree = "<group>"; };
  A00000000000000000000008 = {isa = PBXFileReference; explicitFileType = wrapper.cfbundle; includeInIndex = 0; path = CalendarUITests.xctest; sourceTree = BUILT_PRODUCTS_DIR; };
  A00000000000000000000009 = {isa = XCConfigurationList; buildConfigurations = (A0000000000000000000000C); defaultConfigurationIsVisible = 0; defaultConfigurationName = Debug; };
  A0000000000000000000000A = {isa = PBXSourcesBuildPhase; buildActionMask = 2147483647; files = (A0000000000000000000000D); runOnlyForDeploymentPostprocessing = 0; };
  A0000000000000000000000B = {isa = PBXFrameworksBuildPhase; buildActionMask = 2147483647; files = (); runOnlyForDeploymentPostprocessing = 0; };
  A0000000000000000000000C = {isa = XCBuildConfiguration; buildSettings = {PRODUCT_BUNDLE_IDENTIFIER = dev.gofastr.calendar-ui-tests; PRODUCT_NAME = "$(TARGET_NAME)"; SWIFT_VERSION = 5.0; GENERATE_INFOPLIST_FILE = YES; TARGETED_DEVICE_FAMILY = "1,2"; }; name = Debug; };
  A0000000000000000000000D = {isa = PBXBuildFile; fileRef = A00000000000000000000007; };
 };
 rootObject = A00000000000000000000001;
}
"""

SCHEME = """<?xml version="1.0" encoding="UTF-8"?>
<Scheme LastUpgradeVersion="1600" version="1.3">
 <BuildAction parallelizeBuildables="NO" buildImplicitDependencies="YES"><BuildActionEntries><BuildActionEntry buildForTesting="YES" buildForRunning="NO" buildForProfiling="NO" buildForArchiving="NO" buildForAnalyzing="YES"><BuildableReference BuildableIdentifier="primary" BlueprintIdentifier="A00000000000000000000005" BuildableName="CalendarUITests.xctest" BlueprintName="CalendarUITests" ReferencedContainer="container:CalendarUITests.xcodeproj"/></BuildActionEntry></BuildActionEntries></BuildAction>
 <TestAction buildConfiguration="Debug" shouldUseLaunchSchemeArgsEnv="YES"><Testables><TestableReference skipped="NO"><BuildableReference BuildableIdentifier="primary" BlueprintIdentifier="A00000000000000000000005" BuildableName="CalendarUITests.xctest" BlueprintName="CalendarUITests" ReferencedContainer="container:CalendarUITests.xcodeproj"/></TestableReference></Testables></TestAction>
</Scheme>
"""

if __name__ == "__main__":
    raise SystemExit(main())
