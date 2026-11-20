import StoreKitTest
import XCTest

final class StorefrontTests: XCTestCase {
    @MainActor
    func testPurchaseAccessAndRestore() throws {
        let session = try store()
        let app = XCUIApplication(bundleIdentifier: "dev.gofastr.storefront")
        app.launch()

        run("products", in: app, expecting: "Products (2):")
        XCTAssertTrue(status(in: app).label.contains("Demo Unlock"))
        XCTAssertTrue(status(in: app).label.contains("Demo Monthly"))
        back(in: app)
        run("entitlements", in: app, expecting: "Entitlements (0):")
        back(in: app)
        run("unlock", in: app, expecting: "Purchased: dev.gofastr.storefront.unlock")
        XCTAssertEqual(session.allTransactions().count, 1)
        back(in: app)
        run("updates", in: app, expecting: "Updates (1): dev.gofastr.storefront.unlock")
        back(in: app)
        run("finish", in: app, expecting: "Finished transaction.")
        back(in: app)
        run("restore", in: app, expecting: "Purchases synced.")
        back(in: app)
        run("entitlements", in: app, expecting: "Entitlements (1): dev.gofastr.storefront.unlock")
    }

    @MainActor
    func testPendingSubscriptionBecomesActive() throws {
        let session = try store()
        session.askToBuyEnabled = true
        let app = XCUIApplication(bundleIdentifier: "dev.gofastr.storefront")
        app.launch()

        run("monthly", in: app, expecting: "Purchase pending.")
        let pending = try XCTUnwrap(session.allTransactions().first)
        try session.approveAskToBuyTransaction(identifier: pending.identifier)
        back(in: app)
        run("updates", in: app, expecting: "dev.gofastr.storefront.monthly")
        back(in: app)
        run("subscription", in: app, expecting: "Subscription statuses (1): subscribed")
        back(in: app)
        run("entitlements", in: app, expecting: "Entitlements (1): dev.gofastr.storefront.monthly")
    }

    private func store() throws -> SKTestSession {
        continueAfterFailure = false
        let file = try XCTUnwrap(Bundle(for: Self.self).url(forResource: "Local", withExtension: "storekit"))
        let session = try SKTestSession(contentsOf: file)
        session.resetToDefaultState()
        session.clearTransactions()
        session.disableDialogs = true
        addTeardownBlock {
            session.clearTransactions()
            session.resetToDefaultState()
        }
        return session
    }

    @MainActor
    private func run(_ route: String, in app: XCUIApplication, expecting text: String) {
        let row = app.cells["native.row.\(route)"]
        XCTAssertTrue(row.waitForExistence(timeout: 10), route)
        if !row.isHittable { app.tables.firstMatch.swipeUp() }
        row.tap()
        let action = app.buttons["native.action.perform"]
        XCTAssertTrue(action.waitForExistence(timeout: 10), route)
        action.tap()
        let outcome = status(in: app)
        let hasText = NSPredicate(format: "exists == true AND label CONTAINS %@", text)
        let expectation = XCTNSPredicateExpectation(predicate: hasText, object: outcome)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: 20), .completed,
                       "\(route) expected '\(text)', got '\(outcome.exists ? outcome.label : "(no status)")'")
    }

    @MainActor
    private func back(in app: XCUIApplication) {
        app.navigationBars.buttons["StoreKit Demo"].tap()
    }

    @MainActor
    private func status(in app: XCUIApplication) -> XCUIElement {
        app.staticTexts["native.status"]
    }
}
