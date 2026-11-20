package ios

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBrowserUI uses an installed native Starter app on an explicitly selected simulator.
func TestBrowserUI(t *testing.T) {
	device := os.Getenv("GOFASTR_BROWSER_UI_DEVICE")
	if device == "" {
		t.Skip("set GOFASTR_BROWSER_UI_DEVICE to a simulator with the native Starter app installed")
	}
	requireSwift(t)
	directory := t.TempDir()
	project := filepath.Join(directory, "Browser.xcodeproj")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{filepath.Join(project, "project.pbxproj"): browserUIProject, filepath.Join(directory, "BrowserUITests.swift"): browserUIChecks} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	arguments := []string{"test", "-project", project, "-scheme", "BrowserUI", "-destination", "platform=iOS Simulator,id=" + device, "-derivedDataPath", filepath.Join(directory, "build"), "-collect-test-diagnostics", "never", "CODE_SIGNING_ALLOWED=NO"}
	if results := os.Getenv("GOFASTR_BROWSER_UI_RESULTS"); results != "" {
		arguments = append(arguments, "-resultBundlePath", results)
	}
	command := exec.Command("xcodebuild", arguments...)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
}

const browserUIChecks = `import XCTest
final class BrowserUITests: XCTestCase {
    func testOpenAndDismissDocs() {
        continueAfterFailure = false
        let app = XCUIApplication(bundleIdentifier: "dev.gofastr.starter")
        app.launch()
        let about = app.buttons["About"]
        let more = app.buttons["native.action.more"]
        XCTAssertTrue(more.waitForExistence(timeout: 15))
        more.tap()
        XCTAssertTrue(about.waitForExistence(timeout: 10))
        about.tap()
        let docs = app.buttons["Open docs"]
        XCTAssertTrue(docs.waitForExistence(timeout: 10))
        docs.tap()
        XCTAssertTrue(app.otherElements["gofastr.browser"].waitForExistence(timeout: 15))
        let safari = XCTAttachment(screenshot: app.screenshot())
        safari.name = "In-app Safari"
        safari.lifetime = .keepAlways
        add(safari)
        let done = app.buttons["Done"]
        XCTAssertTrue(done.waitForExistence(timeout: 10))
        let hittable = expectation(for: NSPredicate(format: "isHittable == true"), evaluatedWith: done)
        wait(for: [hittable], timeout: 10)
        RunLoop.current.run(until: Date().addingTimeInterval(1))
        done.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap()
        XCTAssertTrue(app.navigationBars["Browser completed"].waitForExistence(timeout: 15))
        XCTAssertFalse(app.otherElements["gofastr.browser"].exists)
        let completed = XCTAttachment(screenshot: app.screenshot())
        completed.name = "Browser completed"
        completed.lifetime = .keepAlways
        add(completed)
    }
}
`

const browserUIProject = `// !$*UTF8*$!
{
 archiveVersion = 1; objectVersion = 56; classes = {};
 objects = {
  A00000000000000000000001 = {isa = PBXProject; buildConfigurationList = A00000000000000000000002; compatibilityVersion = "Xcode 14.0"; mainGroup = A00000000000000000000003; targets = (A00000000000000000000004,); };
  A00000000000000000000002 = {isa = XCConfigurationList; buildConfigurations = (A00000000000000000000005,); defaultConfigurationName = Debug; defaultConfigurationIsVisible = 0; };
  A00000000000000000000003 = {isa = PBXGroup; children = (A00000000000000000000006,A00000000000000000000007,); sourceTree = "<group>"; };
  A00000000000000000000004 = {isa = PBXNativeTarget; name = BrowserUI; productName = BrowserUI; productReference = A00000000000000000000007; productType = "com.apple.product-type.bundle.ui-testing"; buildConfigurationList = A00000000000000000000008; buildPhases = (A00000000000000000000009,); dependencies = (); buildRules = (); };
  A00000000000000000000005 = {isa = XCBuildConfiguration; name = Debug; buildSettings = {SDKROOT = iphonesimulator; IPHONEOS_DEPLOYMENT_TARGET = 16.0; }; };
  A00000000000000000000006 = {isa = PBXFileReference; path = BrowserUITests.swift; sourceTree = "<group>"; lastKnownFileType = sourcecode.swift; };
  A00000000000000000000007 = {isa = PBXFileReference; path = BrowserUI.xctest; sourceTree = BUILT_PRODUCTS_DIR; explicitFileType = wrapper.cfbundle; };
  A00000000000000000000008 = {isa = XCConfigurationList; buildConfigurations = (A00000000000000000000010,); defaultConfigurationName = Debug; defaultConfigurationIsVisible = 0; };
  A00000000000000000000009 = {isa = PBXSourcesBuildPhase; buildActionMask = 2147483647; files = (A00000000000000000000011,); runOnlyForDeploymentPostprocessing = 0; };
  A00000000000000000000010 = {isa = XCBuildConfiguration; name = Debug; buildSettings = {PRODUCT_BUNDLE_IDENTIFIER = dev.gofastr.browseruitests; PRODUCT_NAME = "$(TARGET_NAME)"; GENERATE_INFOPLIST_FILE = YES; SWIFT_VERSION = 5.0; TARGETED_DEVICE_FAMILY = "1,2"; }; };
  A00000000000000000000011 = {isa = PBXBuildFile; fileRef = A00000000000000000000006; };
 };
 rootObject = A00000000000000000000001;
}
`
