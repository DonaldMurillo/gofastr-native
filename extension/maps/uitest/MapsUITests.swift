import XCTest

final class MapsUITests: XCTestCase {
    private let app = XCUIApplication(bundleIdentifier: "dev.gofastr.gallerynative")

    override func setUpWithError() throws {
        continueAfterFailure = false
        app.terminate()
        app.launch()
        let controls = app.buttons["native.tab.controls"]
        XCTAssertTrue(controls.waitForExistence(timeout: 15))
        controls.tap()
        for _ in 0..<5 {
            let back = app.navigationBars.buttons["BackButton"].firstMatch
            if !back.exists { break }
            back.tap()
        }
        let row = app.staticTexts["Map"].firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        row.tap()
        XCTAssertTrue(app.descendants(matching: .any)["native.map"].firstMatch.waitForExistence(timeout: 15), app.debugDescription)
    }

    private func pin(_ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: "native.map.pin.\(id)").firstMatch
    }

    func testPinsAndLabels() {
        for (id, title) in [("ferry", "Ferry Building"), ("park", "Yerba Buena Gardens"), ("hall", "City Hall")] {
            XCTAssertTrue(pin(id).waitForExistence(timeout: 15))
            XCTAssertEqual(pin(id).label, title)
        }
    }

    func testPinNavigation() {
        let hall = pin("hall")
        XCTAssertTrue(hall.waitForExistence(timeout: 15))
        hall.tap()
        let detail = app.buttons["More Info"].firstMatch
        XCTAssertTrue(detail.waitForExistence(timeout: 5))
        detail.tap()
        XCTAssertTrue(app.staticTexts["Selected place: hall"].waitForExistence(timeout: 10))
    }

    func testUserLocation() {
        let user = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS[c] %@", "My Location")).firstMatch
        XCTAssertTrue(user.waitForExistence(timeout: 30), app.debugDescription)
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = "Map with pins and user location"
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
