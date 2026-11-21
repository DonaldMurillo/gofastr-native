import Foundation

struct MobileIAPRequest: Codable {
    enum Operation: String, Codable {
        case products, purchase, sync, entitlements, updates, finish, subscriptionStatus
    }

    let operation: Operation
    let productIds: [String]?
    let productId: String?
    let transactionId: String?

    func validate() throws {
        let ids = productIds ?? []
        let valid: Bool
        switch operation {
        case .products:
            valid = productId == nil && transactionId == nil && (1...100).contains(ids.count) &&
                Set(ids).count == ids.count && ids.allSatisfy(Self.validProduct)
        case .purchase, .subscriptionStatus:
            valid = ids.isEmpty && transactionId == nil && productId.map(Self.validProduct) == true
        case .finish:
            valid = ids.isEmpty && productId == nil && transactionId.flatMap { id in
                UInt64(id).map { String($0) == id }
            } == true
        case .sync, .entitlements, .updates:
            valid = ids.isEmpty && productId == nil && transactionId == nil
        }
        guard valid else { throw MobileServiceFailure.invalid }
    }

    private static func validProduct(_ id: String) -> Bool {
        (1...100).contains(id.utf8.count) && id.utf8.allSatisfy {
            (65...90).contains($0) || (97...122).contains($0) || (48...57).contains($0) || $0 == 46 || $0 == 95
        }
    }
}

struct MobileIAPProduct: Codable {
    let id: String
    let type: String
    let displayName: String
    let description: String
    let displayPrice: String
}

struct MobileIAPTransaction: Codable {
    let id: String
    let originalId: String
    let productId: String
    let type: String
    let purchasedAt: Int64
    let expiresAt: Int64?
    let revokedAt: Int64?
    let isUpgraded: Bool
}

struct MobileIAPSubscription: Codable {
    let state: String
    let autoRenews: Bool
    let transaction: MobileIAPTransaction
}

struct MobileIAPResult: Codable {
    var products: [MobileIAPProduct]?
    var purchaseState: String?
    var transaction: MobileIAPTransaction?
    var transactions: [MobileIAPTransaction]?
    var subscriptions: [MobileIAPSubscription]?
}
