import StoreKit
import UIKit

@available(iOS 15.0, *)
@MainActor
final class GofastrStoreKit {
    static let shared = GofastrStoreKit()

    private var observer: Task<Void, Never>?
    private var recent: [Transaction] = []
    private var busy = false

    func start() {
        guard observer == nil else { return }
        observer = Task {
            for await update in Transaction.updates {
                guard case .verified(let transaction) = update else { continue }
                recent.removeAll { $0.id == transaction.id }
                recent.append(transaction)
                if recent.count > 100 { recent.removeFirst() }
            }
        }
    }

    func perform(_ request: MobileIAPRequest, presenter: UIViewController?) async -> MobileServiceResult {
        guard !busy else { return MobileServiceResult(status: "failed", message: "Another App Store operation is active.") }
        busy = true
        defer { busy = false }
        start()
        do {
            switch request.operation {
            case .products:
                let products = try await Product.products(for: request.productIds ?? []).map {
                    MobileIAPProduct(id: $0.id, type: Self.kind($0.type), displayName: $0.displayName,
                                     description: $0.description, displayPrice: $0.displayPrice)
                }
                return completed(MobileIAPResult(products: products))
            case .purchase:
                guard AppStore.canMakePayments else {
                    return MobileServiceResult(status: "denied", message: "In-app purchases are not allowed on this device.")
                }
                guard let product = try await Product.products(for: [request.productId ?? ""]).first else {
                    return MobileServiceResult(status: "failed", message: "The requested App Store product is unavailable.")
                }
                guard let window = Self.foregroundWindow(presenter) else { return noForeground }
                let outcome: Product.PurchaseResult
                if #available(iOS 17.0, *), let scene = window.windowScene { outcome = try await product.purchase(confirmIn: scene) }
                else { outcome = try await product.purchase() }
                switch outcome {
                case .success(let result):
                    guard case .verified(let transaction) = result else { return failedVerification }
                    return completed(MobileIAPResult(purchaseState: "purchased", transaction: Self.wire(transaction)))
                case .pending:
                    return completed(MobileIAPResult(purchaseState: "pending"))
                case .userCancelled:
                    return MobileServiceResult(status: "cancelled")
                @unknown default:
                    return MobileServiceResult(status: "failed", message: "The App Store returned an unknown purchase outcome.")
                }
            case .sync:
                guard Self.foregroundWindow(presenter) != nil else { return noForeground }
                try await AppStore.sync()
                return completed(MobileIAPResult())
            case .entitlements:
                var transactions: [MobileIAPTransaction] = []
                for await result in Transaction.currentEntitlements {
                    guard case .verified(let transaction) = result, transaction.revocationDate == nil,
                          !transaction.isUpgraded, transaction.productType != .consumable else { continue }
                    if transaction.productType == .autoRenewable {
                        guard let state = await Self.renewalState(of: transaction),
                              state == .subscribed || state == .inGracePeriod else { continue }
                    }
                    transactions.append(Self.wire(transaction))
                }
                return completed(MobileIAPResult(transactions: transactions))
            case .updates:
                var transactions: [UInt64: Transaction] = [:]
                for await result in Transaction.unfinished {
                    if case .verified(let transaction) = result { transactions[transaction.id] = transaction }
                }
                for transaction in recent { transactions[transaction.id] = transaction }
                var updates: [MobileIAPTransaction] = []
                for transaction in transactions.values.sorted(by: { $0.id < $1.id }) {
                    if transaction.productType == .autoRenewable,
                       let state = await Self.renewalState(of: transaction), Self.state(state) == "unknown" { continue }
                    updates.append(Self.wire(transaction))
                }
                return completed(MobileIAPResult(transactions: updates))
            case .finish:
                for await result in Transaction.unfinished {
                    if case .verified(let transaction) = result, String(transaction.id) == request.transactionId {
                        await transaction.finish()
                        break
                    }
                }
                return completed(MobileIAPResult())
            case .subscriptionStatus:
                guard let subscription = try await Product.products(for: [request.productId ?? ""]).first?.subscription else {
                    return MobileServiceResult(status: "failed", message: "The requested auto-renewable subscription is unavailable.")
                }
                let statuses = try await subscription.status.compactMap { status -> MobileIAPSubscription? in
                    guard case .verified(let transaction) = status.transaction, case .verified(let renewal) = status.renewalInfo else { return nil }
                    return MobileIAPSubscription(state: Self.state(status.state), autoRenews: renewal.willAutoRenew,
                                                 transaction: Self.wire(transaction))
                }
                return completed(MobileIAPResult(subscriptions: statuses))
            }
        } catch StoreKitError.userCancelled {
            return MobileServiceResult(status: "cancelled")
        } catch {
            return MobileServiceResult(status: "failed", message: "The App Store operation could not be completed.")
        }
    }

    private var failedVerification: MobileServiceResult {
        MobileServiceResult(status: "failed", message: "The App Store transaction could not be verified.")
    }

    private var noForeground: MobileServiceResult {
        MobileServiceResult(status: "failed", message: "The App Store operation requires an available foreground window.")
    }

    private func completed(_ result: MobileIAPResult) -> MobileServiceResult {
        MobileServiceResult(status: "completed", iap: result)
    }

    private static func foregroundWindow(_ presenter: UIViewController?) -> UIWindow? {
        guard UIApplication.shared.applicationState == .active, let presenter,
              presenter.presentedViewController == nil, let window = presenter.viewIfLoaded?.window else { return nil }
        if let scene = window.windowScene, scene.activationState != .foregroundActive { return nil }
        return window
    }

    private static func wire(_ transaction: Transaction) -> MobileIAPTransaction {
        MobileIAPTransaction(id: String(transaction.id), originalId: String(transaction.originalID), productId: transaction.productID,
                             type: kind(transaction.productType), purchasedAt: timestamp(transaction.purchaseDate),
                             expiresAt: transaction.expirationDate.map(timestamp), revokedAt: transaction.revocationDate.map(timestamp),
                             isUpgraded: transaction.isUpgraded)
    }

    private static func timestamp(_ date: Date) -> Int64 {
        Int64(date.timeIntervalSince1970 * 1000)
    }

    private static func renewalState(of transaction: Transaction) async -> Product.SubscriptionInfo.RenewalState? {
        guard let status = await transaction.subscriptionStatus,
              case .verified(let current) = status.transaction, current.id == transaction.id,
              current.productID == transaction.productID,
              case .verified = status.renewalInfo else { return nil }
        return status.state
    }

    private static func kind(_ type: Product.ProductType) -> String {
        switch type {
        case .consumable: return "consumable"
        case .nonConsumable: return "nonConsumable"
        case .autoRenewable: return "autoRenewable"
        case .nonRenewable: return "nonRenewing"
        default: return "unknown"
        }
    }

    private static func state(_ state: Product.SubscriptionInfo.RenewalState) -> String {
        switch state {
        case .subscribed: return "subscribed"
        case .expired: return "expired"
        case .inBillingRetryPeriod: return "billingRetry"
        case .inGracePeriod: return "gracePeriod"
        case .revoked: return "revoked"
        default: return "unknown"
        }
    }
}
