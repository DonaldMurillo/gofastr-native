import Foundation

enum MobileRefreshFailure: Error { case invalid }

struct MobileRefreshRequest: Codable {
    static let minimumMinutes = 15
    static let maximumMinutes = 7 * 24 * 60

    let operation: String
    let intervalMinutes: Int?

    func validate() throws {
        switch operation {
        case "schedule":
            guard let intervalMinutes, (Self.minimumMinutes...Self.maximumMinutes).contains(intervalMinutes) else { throw MobileRefreshFailure.invalid }
        case "cancel", "status":
            guard (intervalMinutes ?? 0) == 0 else { throw MobileRefreshFailure.invalid }
        default:
            throw MobileRefreshFailure.invalid
        }
    }
}

struct MobileRefreshState: Codable {
    let scheduled: Bool
}

/// The interval survives relaunch so each background run can submit the next
/// request; the OS forgets a task once it has run.
struct MobileRefreshPreference {
    static let key = "gofastr.background-refresh.interval-minutes"
    let defaults: UserDefaults

    var intervalMinutes: Int? {
        let value = defaults.integer(forKey: Self.key)
        return (MobileRefreshRequest.minimumMinutes...MobileRefreshRequest.maximumMinutes).contains(value) ? value : nil
    }

    func save(_ minutes: Int?) {
        if let minutes { defaults.set(minutes, forKey: Self.key) } else { defaults.removeObject(forKey: Self.key) }
    }
}

/// Valid task identifiers are reverse-DNS names the app also lists in
/// BGTaskSchedulerPermittedIdentifiers.
enum MobileRefreshIdentifier {
    static func valid(_ value: String) -> Bool {
        !value.isEmpty && value.utf8.count <= 200 && !value.hasPrefix(".") && !value.hasSuffix(".") && !value.contains("..") &&
            value.utf8.allSatisfy { (48...57).contains($0) || (65...90).contains($0) || (97...122).contains($0) || [45, 46].contains($0) }
    }
}
