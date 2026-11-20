import Foundation

struct GofastrMapsPayload: Decodable {
    struct Region: Decodable {
        let latitude: Double
        let longitude: Double
        let latitudeDelta: Double
        let longitudeDelta: Double
    }
    struct Pin: Decodable {
        let id: String
        let title: String
        let subtitle: String
        let latitude: Double
        let longitude: Double
    }
    let region: Region?
    let pins: [Pin]?
    let showsUserLocation: Bool

    static func decode(_ payload: String) throws -> Self {
        guard !payload.isEmpty, payload.utf8.count <= MobileComponent.maximumPayloadBytes else {
            throw MobileComponentFailure.invalidPayload
        }
        let value = try JSONDecoder().decode(Self.self, from: Data(payload.utf8))
        let pins = value.pins ?? []
        guard pins.count <= 200, value.region != nil || !pins.isEmpty,
              Set(pins.map { $0.id }).count == pins.count,
              pins.allSatisfy({ !$0.id.isEmpty && $0.latitude.isFinite && $0.longitude.isFinite && abs($0.latitude) <= 90 && abs($0.longitude) <= 180 }) else {
            throw MobileComponentFailure.invalidPayload
        }
        if let r = value.region {
            guard r.latitude.isFinite, r.longitude.isFinite, abs(r.latitude) <= 90, abs(r.longitude) <= 180,
                  r.latitudeDelta > 0, r.latitudeDelta <= 180, r.longitudeDelta > 0, r.longitudeDelta <= 360 else {
                throw MobileComponentFailure.invalidPayload
            }
        }
        return value
    }
}

#if canImport(UIKit)
import UIKit
import MapKit
import CoreLocation

enum GofastrMapComponent: MobileComponentExtensionFactory {
    static let name = "gofastr.maps/v1"
    static func capabilities() -> [String: String] { [name: "available"] }
    static func make(payload: String, compact: Bool, emit: @escaping (String) -> Void) throws -> MobileComponentExtensionView {
        try GofastrMapView(payload: payload, emit: emit)
    }
}

final class GofastrMapAnnotation: MKPointAnnotation {
    let id: String
    init(id: String) { self.id = id; super.init() }
}

final class GofastrVisibleMap: MKMapView {
    var visibilityChanged: (() -> Void)?
    override func didMoveToWindow() { super.didMoveToWindow(); visibilityChanged?() }
}

final class GofastrMapView: NSObject, MobileComponentExtensionView, MKMapViewDelegate, CLLocationManagerDelegate {
    private let map = GofastrVisibleMap()
    private let location = CLLocationManager()
    private var annotations: [String: GofastrMapAnnotation] = [:]
    private var generation = UUID()
    private var emit: ((String) -> Void)?
    private var wantsLocation = false
    private var disposed = false
    var view: UIView { map }

    init(payload: String, emit: @escaping (String) -> Void) throws {
        super.init()
        map.accessibilityIdentifier = "native.map"
        map.heightAnchor.constraint(equalToConstant: 360).isActive = true
        map.delegate = self
        location.delegate = self
        map.visibilityChanged = { [weak self] in self?.updateLocation() }
        try update(payload: payload, emit: emit)
    }

    func update(payload: String, emit: @escaping (String) -> Void) throws {
        let content = try GofastrMapsPayload.decode(payload)
        generation = UUID()
        let current = generation
        self.emit = { [weak self] event in
            guard let self = self, !self.disposed, self.generation == current else { return }
            emit(event)
        }
        let pins = content.pins ?? []
        let ids = Set(pins.map { $0.id })
        for id in Array(annotations.keys) where !ids.contains(id) {
            if let annotation = annotations.removeValue(forKey: id) { map.removeAnnotation(annotation) }
        }
        for pin in pins {
            let annotation = annotations[pin.id] ?? GofastrMapAnnotation(id: pin.id)
            annotation.title = pin.title
            annotation.subtitle = pin.subtitle
            annotation.coordinate = CLLocationCoordinate2D(latitude: pin.latitude, longitude: pin.longitude)
            if annotations[pin.id] == nil { annotations[pin.id] = annotation; map.addAnnotation(annotation) }
            if let marker = map.view(for: annotation) { configure(marker, annotation) }
        }
        if let r = content.region {
            map.setRegion(MKCoordinateRegion(center: CLLocationCoordinate2D(latitude: r.latitude, longitude: r.longitude), span: MKCoordinateSpan(latitudeDelta: r.latitudeDelta, longitudeDelta: r.longitudeDelta)), animated: false)
        } else { map.showAnnotations(Array(annotations.values), animated: false) }
        wantsLocation = content.showsUserLocation
        updateLocation()
    }

    private func updateLocation() {
        precondition(Thread.isMainThread)
        let purpose = Bundle.main.object(forInfoDictionaryKey: "NSLocationWhenInUseUsageDescription") as? String ?? ""
        let enabled = !disposed && wantsLocation && map.window != nil && !purpose.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        if enabled && location.authorizationStatus == .notDetermined { location.requestWhenInUseAuthorization() }
        map.showsUserLocation = enabled && (location.authorizationStatus == .authorizedWhenInUse || location.authorizationStatus == .authorizedAlways)
    }

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) { updateLocation() }

    private func configure(_ marker: MKAnnotationView, _ annotation: GofastrMapAnnotation) {
        marker.accessibilityIdentifier = "native.map.pin.\(annotation.id)"
        marker.accessibilityLabel = annotation.title
        marker.isAccessibilityElement = true
        marker.canShowCallout = true
        marker.rightCalloutAccessoryView = UIButton(type: .detailDisclosure)
    }

    func mapView(_ mapView: MKMapView, viewFor annotation: MKAnnotation) -> MKAnnotationView? {
        guard let pin = annotation as? GofastrMapAnnotation else { return nil }
        let marker = mapView.dequeueReusableAnnotationView(withIdentifier: "pin") ?? MKMarkerAnnotationView(annotation: pin, reuseIdentifier: "pin")
        marker.annotation = pin
        configure(marker, pin)
        return marker
    }

    func mapView(_ mapView: MKMapView, annotationView view: MKAnnotationView, calloutAccessoryControlTapped control: UIControl) {
        guard !disposed, let pin = view.annotation as? GofastrMapAnnotation, annotations[pin.id] === pin else { return }
        emit?("pin.\(pin.id)")
    }

    func dispose() {
        guard !disposed else { return }
        disposed = true
        generation = UUID()
        emit = nil
        map.visibilityChanged = nil
        map.delegate = nil
        map.showsUserLocation = false
        location.stopUpdatingLocation()
        location.delegate = nil
        map.removeAnnotations(Array(annotations.values))
        annotations.removeAll()
    }
}
#endif
