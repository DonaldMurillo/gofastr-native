import XCTest

final class ARKitUITests: XCTestCase {
    private let app = XCUIApplication(bundleIdentifier: "dev.gofastr.gallerynative")

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    private func launch(fixture: Bool) {
        app.terminate()
        app.launchEnvironment = fixture ? ["GOFASTR_AR_FIXTURE": "1"] : [:]
        app.launch()
        let controls = app.buttons["native.tab.controls"]
        XCTAssertTrue(controls.waitForExistence(timeout: 15))
        controls.tap()
        for _ in 0..<5 {
            let back = app.navigationBars.buttons["BackButton"].firstMatch
            if !back.exists { break }
            back.tap()
        }
        let row = app.staticTexts["AR scene"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        row.tap()
    }

    private func element(_ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    private func capture(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    func testUnsupportedWithoutWorldTracking() {
        launch(fixture: false)
        let unsupported = element("native.ar.unsupported")
        XCTAssertTrue(unsupported.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertTrue(unsupported.label.contains("ARKit world tracking"), unsupported.label)
        XCTAssertFalse(element("native.ar.reset").exists)
        XCTAssertFalse(element("native.ar.object.cube").exists)
        capture("AR unsupported on simulator")
    }

    func testFixedObjectNavigation() {
        launch(fixture: true)
        let cube = element("native.ar.object.cube")
        XCTAssertTrue(cube.waitForExistence(timeout: 15), app.debugDescription)
        XCTAssertEqual(cube.label, "Blue cube")
        XCTAssertFalse(element("native.ar.object.ball").exists)
        XCTAssertEqual(element("native.ar.status").label, "Tap a surface to place Orange ball.")
        cube.tap()
        XCTAssertTrue(app.staticTexts["Selected object: cube"].waitForExistence(timeout: 10))
    }

    func testTapToPlaceAndReset() {
        launch(fixture: true)
        let status = element("native.ar.status")
        XCTAssertTrue(status.waitForExistence(timeout: 15), app.debugDescription)
        let ar = element("native.ar")
        XCTAssertTrue(ar.exists, app.debugDescription)
        ar.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: ar.frame.width * 0.3, dy: 320)).tap()
        let ball = element("native.ar.object.ball")
        XCTAssertTrue(ball.waitForExistence(timeout: 10), app.debugDescription)
        XCTAssertEqual(ball.label, "Orange ball")
        XCTAssertEqual(status.label, "Tap an object to open it.")
        capture("AR fixture with placed objects")
        ball.tap()
        XCTAssertTrue(app.staticTexts["Selected object: ball"].waitForExistence(timeout: 10))
        app.navigationBars.buttons["BackButton"].firstMatch.tap()
        let reset = element("native.ar.reset")
        XCTAssertTrue(reset.waitForExistence(timeout: 10))
        reset.tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: element("native.ar.object.ball"))
        wait(for: [gone], timeout: 10)
        XCTAssertEqual(element("native.ar.status").label, "Tap a surface to place Orange ball.")
        XCTAssertTrue(element("native.ar.object.cube").exists)
    }
}
