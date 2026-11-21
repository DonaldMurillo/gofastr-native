import XCTest

final class AuthUITests: XCTestCase {
    private let app = XCUIApplication(bundleIdentifier: "dev.gofastr.features")

    override func setUpWithError() throws {
        continueAfterFailure = false
        app.launch()
        let demo = app.staticTexts["Native sign-in"]
        XCTAssertTrue(demo.waitForExistence(timeout: 15))
        demo.tap()
        XCTAssertTrue(app.buttons["native.action.apple"].waitForExistence(timeout: 5))
    }

    func testOAuthPresentsAndCancels() {
        app.buttons["native.action.more"].tap()
        let oauth = app.buttons["Open OAuth login"]
        XCTAssertTrue(oauth.waitForExistence(timeout: 5))
        oauth.tap()

        let continueButton = app.buttons["Continue"]
        if continueButton.waitForExistence(timeout: 3) { continueButton.tap() }
        let cancel = app.buttons.matching(NSPredicate(format: "label IN %@", ["Cancel", "Done", "Close"])).firstMatch
        XCTAssertTrue(cancel.waitForExistence(timeout: 15), "OAuth system UI did not present a dismissal control")
        cancel.tap()
        XCTAssertTrue(app.staticTexts["Sign-in result: cancelled. No app account was created."].waitForExistence(timeout: 10))
    }

    func testAppleWithoutAnAccountSettlesGracefully() {
        app.buttons["native.action.apple"].tap()
        let outcome = app.staticTexts.matching(NSPredicate(format: "label IN %@", [
            "Sign-in result: failed. No app account was created.",
            "Sign-in result: cancelled. No app account was created."
        ])).firstMatch
        let cancel = app.buttons.matching(NSPredicate(format: "label IN %@", ["Cancel", "Not Now", "Close"])).firstMatch
        let observed = NSPredicate { _, _ in outcome.exists || cancel.exists }
        XCTAssertEqual(XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: observed, object: nil)], timeout: 15), .completed)
        if cancel.exists { cancel.tap() }
        XCTAssertTrue(outcome.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["native.action.apple"].isEnabled)
    }
}
