import XCTest

final class ScannerTests: XCTestCase {
    private let fixture = "gofastr-scanner-fixture"
    private func launch() -> XCUIApplication {
        continueAfterFailure = false
        let app = XCUIApplication(bundleIdentifier: "dev.example.devicefeatures")
        app.launchEnvironment["GOFASTR_SCANNER_FIXTURE"] = fixture
        app.launch()
        return app
    }

    func testScanFixture() {
        let app = launch()
        let scan = app.buttons["Scan code"]
        XCTAssertTrue(scan.waitForExistence(timeout: 20))
        scan.tap()
        let testCode = app.buttons["gofastr.scanner.test"]
        XCTAssertTrue(testCode.waitForExistence(timeout: 10))
        testCode.tap()
        XCTAssertTrue(app.staticTexts["Scanned qr: \(fixture)"].waitForExistence(timeout: 15))
        let screenshot = XCTAttachment(screenshot: app.screenshot())
        screenshot.name = "Scanner result"
        screenshot.lifetime = .keepAlways
        add(screenshot)
    }

    func testCancel() {
        let app = launch()
        let scan = app.buttons["Scan code"]
        XCTAssertTrue(scan.waitForExistence(timeout: 20))
        scan.tap()
        let cancel = app.buttons["gofastr.scanner.cancel"]
        XCTAssertTrue(cancel.waitForExistence(timeout: 10))
        cancel.tap()
        XCTAssertTrue(app.staticTexts["Device result: cancelled"].waitForExistence(timeout: 10))
    }
}
