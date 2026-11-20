import Foundation

enum MobileCalendarFailure: Error { case invalid }

struct MobileCalendarEvent: Codable {
    var identifier: String?
    var title: String
    var start: String
    var end: String
    var allDay: Bool
    var location: String?
    var notes: String?

    static func date(_ value: String) -> Date? {
        guard value.range(of: #"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$"#, options: .regularExpression) != nil,
              !value.hasPrefix("0000-"), !value.hasPrefix("0001-01-01T00:00:00") else { return nil }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.date(from: value) ?? ISO8601DateFormatter().date(from: value)
    }
    static func plain(_ value: String, _ maximum: Int) -> Bool {
        value.utf8.count <= maximum && !value.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains)
    }
    func validate(result: Bool = false) throws {
        guard Self.plain(title, 200), !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              Self.plain(location ?? "", 200), Self.plain(notes ?? "", 2000),
              let start = Self.date(start), let end = Self.date(end), end >= start,
              result ? Self.validID(identifier ?? "") : (identifier ?? "").isEmpty else { throw MobileCalendarFailure.invalid }
    }
    static func validID(_ value: String) -> Bool {
        plain(value, 512) && !value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

struct MobileCalendarRequest: Codable {
    var operation: String
    var event: MobileCalendarEvent?
    var from: String?
    var to: String?
    var limit: Int?
    var effectiveLimit: Int { (limit ?? 0) == 0 ? 20 : limit! }

    func validate() throws {
        if operation == "status" || operation == "add" {
            guard from == nil, to == nil, (limit ?? 0) == 0 else { throw MobileCalendarFailure.invalid }
            if operation == "status" { guard event == nil else { throw MobileCalendarFailure.invalid }; return }
            guard let event else { throw MobileCalendarFailure.invalid }
            try event.validate()
        } else {
            guard operation == "read", event == nil, let from, let to,
                  let start = MobileCalendarEvent.date(from), let end = MobileCalendarEvent.date(to),
                  end > start, end.timeIntervalSince(start) <= 366 * 86400,
                  (0...100).contains(limit ?? 0) else { throw MobileCalendarFailure.invalid }
        }
    }

    func accept(_ result: MobileCalendarResult) throws {
        try validate()
        switch operation {
        case "status":
            guard ["full", "writeOnly", "denied", "restricted", "notDetermined"].contains(result.authorization ?? ""),
                  result.identifier == nil, result.events == nil else { throw MobileCalendarFailure.invalid }
        case "add":
            guard MobileCalendarEvent.validID(result.identifier ?? ""), result.authorization == nil, result.events == nil else { throw MobileCalendarFailure.invalid }
        default:
            guard result.authorization == nil, result.identifier == nil, (result.events?.count ?? 0) <= effectiveLimit,
                  let from = from.flatMap(MobileCalendarEvent.date), let to = to.flatMap(MobileCalendarEvent.date) else { throw MobileCalendarFailure.invalid }
            for event in result.events ?? [] {
                try event.validate(result: true)
                guard let start = MobileCalendarEvent.date(event.start), let end = MobileCalendarEvent.date(event.end),
                      start < to, end >= from else { throw MobileCalendarFailure.invalid }
            }
        }
    }
}

struct MobileCalendarResult: Codable {
    var authorization: String?
    var identifier: String?
    var events: [MobileCalendarEvent]?
    var wire: [String: Any] {
        guard let data = try? JSONEncoder().encode(self),
              let value = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [:] }
        return value
    }
}

#if canImport(UIKit)
import UIKit
import EventKit

final class GofastrCalendar {
    private let queue = DispatchQueue(label: "gofastr.native.calendar", qos: .userInitiated)
    private var completion: ((MobileServiceResult) -> Void)?
    private var cancelled = false
    static var enabled: Bool {
        Bundle.main.object(forInfoDictionaryKey: "GofastrCalendar") as? Bool == true &&
        ["NSCalendarsFullAccessUsageDescription", "NSCalendarsWriteOnlyAccessUsageDescription", "NSCalendarsUsageDescription"].allSatisfy {
            !(Bundle.main.object(forInfoDictionaryKey: $0) as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        }
    }
    func cancel() { cancelled = true }
    private static func authorization() -> String {
        let status = EKEventStore.authorizationStatus(for: .event)
        if #available(iOS 17.0, *) {
            if status == .fullAccess { return "full" }
            if status == .writeOnly { return "writeOnly" }
        }
        switch status {
        case .authorized: return "full"
        case .denied: return "denied"
        case .restricted: return "restricted"
        default: return "notDetermined"
        }
    }
    func perform(_ request: MobileCalendarRequest, completion: @escaping (MobileServiceResult) -> Void) {
        precondition(Thread.isMainThread)
        guard self.completion == nil else { completion(MobileServiceResult(status: "failed")); return }
        guard Self.enabled else { completion(MobileServiceResult(status: "unsupported")); return }
        do { try request.validate() } catch { completion(MobileServiceResult(status: "failed")); return }
        self.completion = completion
        cancelled = false
        queue.async {
            let store = EKEventStore()
            if request.operation == "status" {
                self.finish(MobileServiceResult(status: "completed", calendar: MobileCalendarResult(authorization: Self.authorization())))
                return
            }
            let granted: (Bool, Error?) -> Void = { allowed, _ in
                self.queue.async {
                    guard allowed else { self.finish(MobileServiceResult(status: "denied")); return }
                    DispatchQueue.main.async {
                        guard !self.cancelled, UIApplication.shared.applicationState != .background else {
                            self.finish(MobileServiceResult(status: "cancelled")); return
                        }
                        self.queue.async {
                            do {
                                let result = try Self.execute(request, store: store)
                                guard let calendar = result.calendar else { throw MobileCalendarFailure.invalid }
                                try request.accept(calendar)
                                self.finish(result)
                            }
                            catch { self.finish(MobileServiceResult(status: "failed")) }
                        }
                    }
                }
            }
            if #available(iOS 17.0, *) {
                if request.operation == "add" { store.requestWriteOnlyAccessToEvents(completion: granted) }
                else { store.requestFullAccessToEvents(completion: granted) }
            } else { store.requestAccess(to: .event, completion: granted) }
        }
    }
    private static func execute(_ request: MobileCalendarRequest, store: EKEventStore) throws -> MobileServiceResult {
        if let input = request.event {
            guard let calendar = store.defaultCalendarForNewEvents else { throw MobileCalendarFailure.invalid }
            let event = EKEvent(eventStore: store)
            event.calendar = calendar
            event.title = input.title
            event.startDate = MobileCalendarEvent.date(input.start)
            event.endDate = MobileCalendarEvent.date(input.end)
            event.isAllDay = input.allDay
            event.location = input.location
            event.notes = input.notes
            try store.save(event, span: .thisEvent, commit: true)
            guard let identifier = event.eventIdentifier, MobileCalendarEvent.validID(identifier) else { throw MobileCalendarFailure.invalid }
            return MobileServiceResult(status: "completed", calendar: MobileCalendarResult(identifier: identifier))
        }
        guard let from = request.from.flatMap(MobileCalendarEvent.date), let to = request.to.flatMap(MobileCalendarEvent.date) else { throw MobileCalendarFailure.invalid }
        let predicate = store.predicateForEvents(withStart: from, end: to, calendars: nil)
        let formatter = ISO8601DateFormatter()
        var events: [MobileCalendarEvent] = []
        store.enumerateEvents(matching: predicate) { event, stop in
            guard let identifier = event.eventIdentifier, let start = event.startDate, let end = event.endDate else { return }
            let value = MobileCalendarEvent(identifier: identifier, title: event.title ?? "", start: formatter.string(from: start), end: formatter.string(from: end), allDay: event.isAllDay, location: event.location, notes: event.notes)
            guard (try? value.validate(result: true)) != nil, start < to, end >= from else { return }
            events.append(value)
            if events.count >= request.effectiveLimit { stop.pointee = true }
        }
        return MobileServiceResult(status: "completed", calendar: MobileCalendarResult(events: events))
    }
    private func finish(_ result: MobileServiceResult) {
        DispatchQueue.main.async {
            guard let completion = self.completion else { return }
            self.completion = nil
            completion(result)
        }
    }
}
#endif
