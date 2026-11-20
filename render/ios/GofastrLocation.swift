import Foundation

struct MobileLocationRequest: Codable {
    let operation: String
    var accuracy: String?
    var timeoutMillis: Int?
    var maxSamples: Int?
    var durationMillis: Int?
    var distanceFilterMeters: Double?
    var reason: String?

    func validate() throws {
        let reason = reason ?? "", distance = distanceFilterMeters ?? 0
        guard reason.utf8.count <= 200, !reason.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
              distance.isFinite, distance >= 0 else { throw MobileLocationFailure.invalid }
        switch operation {
        case "status", "authorize":
            guard (accuracy ?? "").isEmpty, (timeoutMillis ?? 0) == 0, (maxSamples ?? 0) == 0,
                  (durationMillis ?? 0) == 0, distance == 0, operation != "status" || reason.isEmpty else { throw MobileLocationFailure.invalid }
        case "current", "track":
            guard ["", "best", "balanced", "coarse"].contains(accuracy ?? "") else { throw MobileLocationFailure.invalid }
            if operation == "current" {
                guard (0...60_000).contains(timeoutMillis ?? 0), (maxSamples ?? 0) == 0,
                      (durationMillis ?? 0) == 0, distance == 0 else { throw MobileLocationFailure.invalid }
            } else {
                guard (timeoutMillis ?? 0) == 0, (1...100).contains(maxSamples ?? 0),
                      (1_000...120_000).contains(durationMillis ?? 0) else { throw MobileLocationFailure.invalid }
            }
        default: throw MobileLocationFailure.invalid
        }
    }
}

struct MobileLocationFix: Codable {
    let latitude: Double
    let longitude: Double
    let horizontalAccuracyMeters: Double
    let altitudeMeters: Double
    let verticalAccuracyMeters: Double
    let speedMetersPerSecond: Double
    let courseDegrees: Double
    let timestampMillis: Int64
}

struct MobileLocationResult: Codable {
    let permission: String
    let servicesEnabled: Bool
    let precise: Bool
    var fixes: [MobileLocationFix]?

    var wire: [String: Any] {
        var value: [String: Any] = ["permission": permission, "servicesEnabled": servicesEnabled, "precise": precise]
        if let fixes, !fixes.isEmpty, let data = try? JSONEncoder().encode(fixes),
           let encoded = try? JSONSerialization.jsonObject(with: data) { value["fixes"] = encoded }
        return value
    }
}

enum MobileLocationFailure: Error { case invalid }

#if os(iOS)
import CoreLocation

final class GofastrLocation: NSObject, CLLocationManagerDelegate {
    private let isForeground: () -> Bool
    private var manager: CLLocationManager?
    private var request: MobileLocationRequest?
    private var completion: ((MobileServiceResult) -> Void)?
    private var timer: Timer?
    private var started = false
    private var fixes: [MobileLocationFix] = []
    private var backgroundObserver: NSObjectProtocol?

    init(isForeground: @escaping () -> Bool) {
        self.isForeground = isForeground
        super.init()
        backgroundObserver = NotificationCenter.default.addObserver(forName: Notification.Name("UIApplicationDidEnterBackgroundNotification"), object: nil, queue: .main) { [weak self] _ in self?.cancel() }
    }

    deinit {
        if let backgroundObserver { NotificationCenter.default.removeObserver(backgroundObserver) }
        timer?.invalidate()
        manager?.stopUpdatingLocation()
        manager?.delegate = nil
    }

    static var enabled: Bool {
        Bundle.main.object(forInfoDictionaryKey: "GofastrLocation") as? Bool == true &&
        (Bundle.main.object(forInfoDictionaryKey: "NSLocationWhenInUseUsageDescription") as? String)?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false
    }

    func perform(_ request: MobileLocationRequest, completion: @escaping (MobileServiceResult) -> Void) {
        precondition(Thread.isMainThread)
        guard self.completion == nil else { completion(MobileServiceResult(status: "failed")); return }
        guard Self.enabled else { completion(MobileServiceResult(status: "unsupported")); return }
        guard isForeground() else { completion(MobileServiceResult(status: "cancelled")); return }
        do { try request.validate() } catch { completion(MobileServiceResult(status: "failed")); return }
        self.completion = completion
        self.request = request
        fixes = []
        started = false
        let manager = CLLocationManager()
        self.manager = manager
        manager.delegate = self
        if request.operation == "current" {
            deadline(manager, milliseconds: request.timeoutMillis == nil || request.timeoutMillis == 0 ? 15_000 : request.timeoutMillis!)
        }
        advance(manager, mayPrompt: true)
    }

    func cancel() {
        precondition(Thread.isMainThread)
        finish(MobileServiceResult(status: "cancelled"))
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        guard manager === self.manager else { return }
        advance(manager, mayPrompt: false)
    }

    private func advance(_ manager: CLLocationManager, mayPrompt: Bool) {
        guard manager === self.manager, let request else { return }
        guard isForeground() else { cancel(); return }
        let observed = snapshot(manager)
        guard observed.servicesEnabled else { finish(MobileServiceResult(status: "unsupported", permission: observed.permission)); return }
        if ["denied", "restricted"].contains(observed.permission) {
            finish(MobileServiceResult(status: "denied", permission: observed.permission)); return
        }
        if request.operation == "status" { complete(manager); return }
        if observed.permission == "not_determined" {
            if mayPrompt { manager.requestWhenInUseAuthorization() }
            return
        }
        if request.operation == "authorize" { complete(manager); return }
        guard !started else { return }
        started = true
        switch request.accuracy ?? "best" {
        case "balanced": manager.desiredAccuracy = kCLLocationAccuracyHundredMeters
        case "coarse": manager.desiredAccuracy = kCLLocationAccuracyKilometer
        default: manager.desiredAccuracy = kCLLocationAccuracyBest
        }
        manager.distanceFilter = request.operation == "track" ? request.distanceFilterMeters ?? kCLDistanceFilterNone : kCLDistanceFilterNone
        manager.allowsBackgroundLocationUpdates = false
        if request.operation == "track" { deadline(manager, milliseconds: request.durationMillis!) }
        manager.startUpdatingLocation()
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        guard manager === self.manager, started, let request else { return }
        guard isForeground() else { cancel(); return }
        let limit = request.operation == "current" ? 1 : request.maxSamples!
        for location in locations {
            let coordinate = location.coordinate
            guard CLLocationCoordinate2DIsValid(coordinate), location.horizontalAccuracy.isFinite, location.horizontalAccuracy >= 0,
                  location.altitude.isFinite, location.verticalAccuracy.isFinite, location.speed.isFinite, location.course.isFinite,
                  location.timestamp.timeIntervalSince1970.isFinite, location.timestamp.timeIntervalSince1970 >= 0,
                  location.timestamp.timeIntervalSince1970 < Double(Int64.max / 1000) else { continue }
            let fix = MobileLocationFix(latitude: coordinate.latitude, longitude: coordinate.longitude,
                horizontalAccuracyMeters: location.horizontalAccuracy, altitudeMeters: location.altitude,
                verticalAccuracyMeters: location.verticalAccuracy < 0 ? -1 : location.verticalAccuracy,
                speedMetersPerSecond: location.speed < 0 ? -1 : location.speed,
                courseDegrees: location.course < 0 || location.course >= 360 ? -1 : location.course,
                timestampMillis: Int64(location.timestamp.timeIntervalSince1970 * 1000))
            fixes.append(fix)
            if fixes.count == limit { complete(manager); return }
        }
    }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) {
        guard manager === self.manager else { return }
        if let error = error as? CLError {
            if error.code == .locationUnknown { return }
            if error.code == .denied { advance(manager, mayPrompt: false); return }
        }
        finish(MobileServiceResult(status: "failed", permission: snapshot(manager).permission))
    }

    private func snapshot(_ manager: CLLocationManager) -> MobileLocationResult {
        let permission: String
        switch manager.authorizationStatus {
        case .notDetermined: permission = "not_determined"
        case .denied: permission = "denied"
        case .restricted: permission = "restricted"
        case .authorizedWhenInUse: permission = "when_in_use"
        case .authorizedAlways: permission = "always"
        @unknown default: permission = "restricted"
        }
        return MobileLocationResult(permission: permission, servicesEnabled: CLLocationManager.locationServicesEnabled(), precise: manager.accuracyAuthorization == .fullAccuracy, fixes: fixes.isEmpty ? nil : fixes)
    }

    private func deadline(_ manager: CLLocationManager, milliseconds: Int) {
        timer?.invalidate()
        timer = Timer.scheduledTimer(withTimeInterval: Double(milliseconds) / 1000, repeats: false) { [weak self, weak manager] _ in
            guard let self, let manager, manager === self.manager else { return }
            if !self.isForeground() { self.cancel() }
            else if self.fixes.isEmpty { self.finish(MobileServiceResult(status: "failed", permission: self.snapshot(manager).permission)) }
            else { self.complete(manager) }
        }
    }

    private func complete(_ manager: CLLocationManager) {
        let observed = snapshot(manager)
        guard observed.servicesEnabled else { finish(MobileServiceResult(status: "unsupported", permission: observed.permission)); return }
        guard !["denied", "restricted"].contains(observed.permission) else { finish(MobileServiceResult(status: "denied", permission: observed.permission)); return }
        finish(MobileServiceResult(status: "completed", location: observed))
    }

    private func finish(_ result: MobileServiceResult) {
        guard let completion else { return }
        self.completion = nil
        timer?.invalidate()
        timer = nil
        manager?.delegate = nil
        manager?.stopUpdatingLocation()
        manager = nil
        request = nil
        fixes = []
        started = false
        completion(result)
    }
}
#endif
