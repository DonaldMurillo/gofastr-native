import XCTest

final class CalendarUITests: XCTestCase {
    func testAddReadAndFullAuthorization() {
        continueAfterFailure = false
        let app = XCUIApplication(bundleIdentifier: "dev.gofastr.featuresnative")
        app.launch()
        let calendar = app.staticTexts["Calendar events"]
        XCTAssertTrue(calendar.waitForExistence(timeout: 15))
        calendar.tap()

        let add = app.buttons["native.action.calendar-add"]
        XCTAssertTrue(add.waitForExistence(timeout: 10))
        add.tap()
        let status = app.staticTexts["native.status"]
        expect(status, contains: "Sample event added.")

        action("Calendar status", app: app)
        expect(status, contains: "Calendar authorization: full")

        action("Read next 7 days", app: app)
        expect(status, contains: "GoFastr sample event — ")
        let screenshot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        screenshot.name = "calendar-read"
        screenshot.lifetime = .keepAlways
        self.add(screenshot)
    }

    private func action(_ label: String, app: XCUIApplication) {
        app.buttons["native.action.more"].tap()
        let action = app.buttons[label]
        XCTAssertTrue(action.waitForExistence(timeout: 5))
        action.tap()
    }

    private func expect(_ element: XCUIElement, contains text: String) {
        let predicate = NSPredicate(format: "label CONTAINS %@", text)
        let expectation = XCTNSPredicateExpectation(predicate: predicate, object: element)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 20), .completed,
                       "Expected \(text), got \(element.label)")
    }
}
