#if canImport(UIKit)
import UIKit

// The instrument is drawn on Android only so far. On iOS the component
// says so, so a build that includes it still compiles and renders.
enum ChordHarpComponent: MobileComponentExtensionFactory {
    static let name = "dev.gofastr.chordharp/v1"

    static func capabilities() -> [String: String] { [name: "available"] }

    static func make(payload: String, compact: Bool, emit: @escaping (String) -> Void) throws -> MobileComponentExtensionView {
        ChordHarpPlaceholder()
    }
}

final class ChordHarpPlaceholder: MobileComponentExtensionView {
    private let label = UILabel()
    var view: UIView { label }

    init() {
        label.text = "The chord harp plays on Android. The iOS view is not written yet."
        label.numberOfLines = 0
        label.font = .preferredFont(forTextStyle: .body)
        label.adjustsFontForContentSizeCategory = true
    }

    func update(payload: String, emit: @escaping (String) -> Void) throws {}
    func dispose() {}
}
#endif
