import XCTest

final class LocationUI: XCTestCase {
    func testLocationActionsShowSimulatedCoordinates() {
        continueAfterFailure = false
        let app = XCUIApplication(bundleIdentifier: "dev.gofastr.locationdemo")
        app.activate()
        let location = app.cells["native.row.location"]
        XCTAssertTrue(location.waitForExistence(timeout: 15), app.debugDescription)
        location.tap()

        func action(_ label: String) {
            let more = app.buttons["native.action.more"]
            XCTAssertTrue(more.waitForExistence(timeout: 10))
            more.tap()
            let button = app.buttons[label]
            XCTAssertTrue(button.waitForExistence(timeout: 5), app.debugDescription)
            button.tap()
        }
        func result(_ operation: String, contains text: String) {
            let row = app.cells["native.row.location-" + operation]
            let predicate = NSPredicate(format: "exists == true AND label CONTAINS %@", text)
            expectation(for: predicate, evaluatedWith: row)
            waitForExpectations(timeout: 25)
            XCTAssertTrue(row.isHittable, app.debugDescription)
        }

        action("Location status")
        result("status", contains: "permission: when_in_use")
        action("Authorize location")
        result("authorize", contains: "completed")
        app.buttons["native.action.current"].tap()
        result("current", contains: "37.3349, -122.0090")
        action("Track 5 samples")
        result("track", contains: "37.3349, -122.0090")
        result("track", contains: "completed")

        let screenshot = XCTAttachment(screenshot: app.screenshot())
        screenshot.name = "Location coordinates"
        screenshot.lifetime = .keepAlways
        add(screenshot)
    }
}
