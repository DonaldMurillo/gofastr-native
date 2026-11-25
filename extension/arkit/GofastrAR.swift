import Foundation

struct GofastrARPayload: Decodable, Equatable {
    struct Vector: Decodable, Equatable {
        let x: Double
        let y: Double
        let z: Double
    }
    struct Object: Decodable, Equatable {
        let id: String
        let title: String
        let shape: String
        let size: Double
        let color: String
        let position: Vector?
        let destination: String?
        var navigable: Bool { !(destination ?? "").isEmpty }
    }
    let planes: [String]?
    let showsPlanes: Bool
    let coaching: Bool
    let lightEstimation: Bool
    let objects: [Object]?

    static let maximumObjects = 64
    static let shapes: Set<String> = ["box", "sphere", "cylinder", "cone", "pyramid"]

    static func decode(_ payload: String) throws -> Self {
        guard !payload.isEmpty, payload.utf8.count <= MobileComponent.maximumPayloadBytes else {
            throw MobileComponentFailure.invalidPayload
        }
        let value = try JSONDecoder().decode(Self.self, from: Data(payload.utf8))
        let planes = value.planes ?? []
        let objects = value.objects ?? []
        guard Set(planes).count == planes.count, planes.allSatisfy({ $0 == "horizontal" || $0 == "vertical" }),
              !value.showsPlanes || !planes.isEmpty, objects.count <= maximumObjects,
              Set(objects.map { $0.id }).count == objects.count,
              objects.allSatisfy({ object in
                  !object.id.isEmpty && !object.title.isEmpty && shapes.contains(object.shape) && rgb(object.color) != nil &&
                      object.size.isFinite && object.size >= 0.01 && object.size <= 2 &&
                      (object.position.map { [$0.x, $0.y, $0.z].allSatisfy { $0.isFinite && abs($0) <= 50 } } ?? !planes.isEmpty)
              }) else {
            throw MobileComponentFailure.invalidPayload
        }
        return value
    }

    static func rgb(_ hex: String) -> (red: Double, green: Double, blue: Double)? {
        let digits = hex.dropFirst()
        guard hex.utf8.count == 7, hex.first == "#", digits.allSatisfy({ "0123456789abcdefABCDEF".contains($0) }),
              let value = UInt32(digits, radix: 16) else { return nil }
        return (Double((value >> 16) & 0xff) / 255, Double((value >> 8) & 0xff) / 255, Double(value & 0xff) / 255)
    }
}

enum GofastrARStatus {
    static func message(tracking: String?, surfaces: Int, detectsPlanes: Bool, unplaced: String?, navigable: Bool) -> String {
        if let tracking { return tracking }
        if let unplaced {
            return surfaces > 0 ? "Tap a surface to place \(unplaced)." : "Move the device slowly to find a surface."
        }
        if navigable { return "Tap an object to open it." }
        guard detectsPlanes else { return "Tracking." }
        switch surfaces {
        case 0: return "Move the device slowly to find a surface."
        case 1: return "Found 1 surface."
        default: return "Found \(surfaces) surfaces."
        }
    }
}

#if canImport(UIKit) && canImport(ARKit)
import UIKit
import ARKit
import SceneKit
import AVFoundation

enum GofastrARComponent: MobileComponentExtensionFactory {
    static let name = "gofastr.arkit/v1"
    static func capabilities() -> [String: String] { [name: "available"] }
    static func make(payload: String, compact: Bool, emit: @escaping (String) -> Void) throws -> MobileComponentExtensionView {
        try GofastrARView(payload: payload, emit: emit)
    }
}

final class GofastrARContainer: UIView {
    var visibilityChanged: (() -> Void)?
    var elementsProvider: (() -> [Any])?
    override func didMoveToWindow() { super.didMoveToWindow(); visibilityChanged?() }
    override var accessibilityElements: [Any]? {
        get { elementsProvider?() }
        set {}
    }
}

final class GofastrARObjectElement: UIAccessibilityElement {
    var activate: (() -> Bool)?
    override func accessibilityActivate() -> Bool { activate?() ?? false }
}

final class GofastrARView: NSObject, MobileComponentExtensionView, ARSCNViewDelegate {
    private enum Mode: Equatable { case tracking, fixture, unsupported(String) }
    private struct Placement { let point: SCNVector3; let normal: SCNVector3 }
    private static let objectPrefix = "gofastr.ar.object."

    private let container = GofastrARContainer()
    private let stage = UIView()
    private let status = UILabel()
    private let reset = UIButton(type: .system)
    private let mode: Mode
    private var sceneView: SCNView?
    private var arView: ARSCNView? { sceneView as? ARSCNView }
    private var coaching: ARCoachingOverlayView?
    private var floor: SCNNode?
    private var content: GofastrARPayload
    private var nodes: [String: SCNNode] = [:]
    private var placements: [String: Placement] = [:]
    private var planes: [UUID: SCNNode] = [:]
    private var elements: [String: GofastrARObjectElement] = [:]
    private var tracking: String?
    private var failure: String?
    private var running = false
    private var generation = UUID()
    private var emit: ((String) -> Void)?
    private var disposed = false
    var view: UIView { container }

    init(payload: String, emit: @escaping (String) -> Void) throws {
        content = try GofastrARPayload.decode(payload)
        mode = Self.detectMode()
        super.init()
        layout()
        switch mode {
        case .tracking: makeTrackingScene()
        case .fixture: makeFixtureScene()
        case .unsupported: break
        }
        container.visibilityChanged = { [weak self] in self?.visibilityChanged() }
        container.elementsProvider = { [weak self] in self?.accessibleElements() ?? [] }
        try update(payload: payload, emit: emit)
    }

    private static func detectMode() -> Mode {
        #if targetEnvironment(simulator)
        if ProcessInfo.processInfo.environment["GOFASTR_AR_FIXTURE"] != nil { return .fixture }
        #endif
        guard ARWorldTrackingConfiguration.isSupported else {
            return .unsupported("AR needs an iPhone or iPad that supports ARKit world tracking.")
        }
        let purpose = Bundle.main.object(forInfoDictionaryKey: "NSCameraUsageDescription") as? String ?? ""
        guard !purpose.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            return .unsupported("AR is off because this app has no camera purpose text.")
        }
        return .tracking
    }

    private func layout() {
        container.accessibilityIdentifier = "native.ar"
        stage.translatesAutoresizingMaskIntoConstraints = false
        stage.backgroundColor = .secondarySystemBackground
        stage.layer.cornerRadius = 12
        stage.clipsToBounds = true
        status.translatesAutoresizingMaskIntoConstraints = false
        status.numberOfLines = 0
        status.font = .preferredFont(forTextStyle: .footnote)
        status.adjustsFontForContentSizeCategory = true
        status.textColor = .secondaryLabel
        reset.translatesAutoresizingMaskIntoConstraints = false
        reset.setTitle("Reset", for: .normal)
        reset.accessibilityIdentifier = "native.ar.reset"
        reset.addTarget(self, action: #selector(resetTapped), for: .touchUpInside)
        reset.setContentHuggingPriority(.required, for: .horizontal)
        reset.setContentCompressionResistancePriority(.required, for: .horizontal)
        [stage, status, reset].forEach(container.addSubview)
        NSLayoutConstraint.activate([
            stage.topAnchor.constraint(equalTo: container.topAnchor),
            stage.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            stage.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            stage.heightAnchor.constraint(equalToConstant: 360),
            status.topAnchor.constraint(equalTo: stage.bottomAnchor, constant: 8),
            status.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            status.bottomAnchor.constraint(equalTo: container.bottomAnchor),
            reset.leadingAnchor.constraint(equalTo: status.trailingAnchor, constant: 8),
            reset.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            reset.centerYAnchor.constraint(equalTo: status.centerYAnchor),
            reset.topAnchor.constraint(greaterThanOrEqualTo: stage.bottomAnchor),
            reset.bottomAnchor.constraint(lessThanOrEqualTo: container.bottomAnchor),
        ])
    }

    private func install(_ scene: SCNView) {
        scene.translatesAutoresizingMaskIntoConstraints = false
        scene.autoenablesDefaultLighting = true
        scene.antialiasingMode = .multisampling4X
        stage.addSubview(scene)
        NSLayoutConstraint.activate([
            scene.topAnchor.constraint(equalTo: stage.topAnchor),
            scene.bottomAnchor.constraint(equalTo: stage.bottomAnchor),
            scene.leadingAnchor.constraint(equalTo: stage.leadingAnchor),
            scene.trailingAnchor.constraint(equalTo: stage.trailingAnchor),
        ])
        scene.addGestureRecognizer(UITapGestureRecognizer(target: self, action: #selector(tapped(_:))))
        sceneView = scene
    }

    private func makeTrackingScene() {
        let view = ARSCNView()
        view.delegate = self
        view.scene = SCNScene()
        install(view)
    }

    // The simulator has no world tracking. With GOFASTR_AR_FIXTURE set, a fixed
    // camera over one synthetic horizontal surface stands in for the session.
    private func makeFixtureScene() {
        let view = SCNView()
        let scene = SCNScene()
        scene.background.contents = UIColor(white: 0.18, alpha: 1)
        let camera = SCNNode()
        camera.camera = SCNCamera()
        camera.camera?.zNear = 0.01
        camera.eulerAngles.x = -0.35
        scene.rootNode.addChildNode(camera)
        let surface = SCNNode(geometry: SCNPlane(width: 8, height: 8))
        surface.name = "gofastr.ar.fixture.surface"
        surface.eulerAngles.x = -.pi / 2
        surface.position = SCNVector3(0, -0.6, 0)
        scene.rootNode.addChildNode(surface)
        view.scene = scene
        view.pointOfView = camera
        floor = surface
        install(view)
    }

    func update(payload: String, emit: @escaping (String) -> Void) throws {
        let next = try GofastrARPayload.decode(payload)
        generation = UUID()
        let current = generation
        self.emit = { [weak self] event in
            guard let self = self, !self.disposed, self.generation == current else { return }
            emit(event)
        }
        let configurationChanged = next.planes != content.planes || next.lightEstimation != content.lightEstimation
        content = next
        applyObjects()
        applyPlaneVisibility()
        applyCoaching()
        arView?.automaticallyUpdatesLighting = content.lightEstimation
        if running && configurationChanged, let arView { arView.session.run(configuration()) }
        refreshStatus()
    }

    private var objects: [GofastrARPayload.Object] { content.objects ?? [] }
    private var detectsPlanes: Bool { !(content.planes ?? []).isEmpty }
    private var nextUnplaced: GofastrARPayload.Object? {
        objects.first { $0.position == nil && placements[$0.id] == nil }
    }

    private func configuration() -> ARWorldTrackingConfiguration {
        let configuration = ARWorldTrackingConfiguration()
        var detection: ARWorldTrackingConfiguration.PlaneDetection = []
        for plane in content.planes ?? [] {
            detection.insert(plane == "vertical" ? .vertical : .horizontal)
        }
        configuration.planeDetection = detection
        configuration.isLightEstimationEnabled = content.lightEstimation
        return configuration
    }

    private func geometry(_ object: GofastrARPayload.Object) -> SCNGeometry {
        let size = CGFloat(object.size)
        let geometry: SCNGeometry
        switch object.shape {
        case "sphere": geometry = SCNSphere(radius: size / 2)
        case "cylinder": geometry = SCNCylinder(radius: size / 2, height: size)
        case "cone": geometry = SCNCone(topRadius: 0, bottomRadius: size / 2, height: size)
        case "pyramid": geometry = SCNPyramid(width: size, height: size, length: size)
        default: geometry = SCNBox(width: size, height: size, length: size, chamferRadius: size * 0.05)
        }
        let material = SCNMaterial()
        let rgb = GofastrARPayload.rgb(object.color) ?? (0.5, 0.5, 0.5)
        material.diffuse.contents = UIColor(red: CGFloat(rgb.red), green: CGFloat(rgb.green), blue: CGFloat(rgb.blue), alpha: 1)
        material.lightingModel = .physicallyBased
        material.roughness.contents = 0.6
        geometry.materials = [material]
        return geometry
    }

    private func applyObjects() {
        let ids = Set(objects.map { $0.id })
        for id in Array(nodes.keys) where !ids.contains(id) {
            nodes.removeValue(forKey: id)?.removeFromParentNode()
            placements[id] = nil
            elements[id] = nil
        }
        guard let root = sceneView?.scene?.rootNode else { return }
        for object in objects {
            let node = nodes[object.id] ?? SCNNode()
            node.name = Self.objectPrefix + object.id
            node.geometry = geometry(object)
            node.pivot = object.shape == "pyramid" ? SCNMatrix4MakeTranslation(0, Float(object.size / 2), 0) : SCNMatrix4Identity
            nodes[object.id] = node
            if let p = object.position {
                placements[object.id] = nil
                node.position = SCNVector3(Float(p.x), Float(p.y), Float(p.z))
            } else if let placed = placements[object.id] {
                let lift = Float(object.size / 2)
                node.position = SCNVector3(placed.point.x + placed.normal.x * lift, placed.point.y + placed.normal.y * lift, placed.point.z + placed.normal.z * lift)
            } else {
                node.removeFromParentNode()
                continue
            }
            if node.parent == nil { root.addChildNode(node) }
        }
    }

    private func applyPlaneVisibility() {
        for plane in planes.values { plane.isHidden = !content.showsPlanes }
        guard let material = floor?.geometry?.firstMaterial else { return }
        material.diffuse.contents = UIColor(white: 1, alpha: 0.25)
        material.isDoubleSided = true
        material.colorBufferWriteMask = content.showsPlanes ? .all : []
    }

    private func applyCoaching() {
        guard let arView else { return }
        guard content.coaching else {
            coaching?.removeFromSuperview()
            coaching = nil
            return
        }
        let overlay = coaching ?? ARCoachingOverlayView()
        let planes = Set(content.planes ?? [])
        overlay.goal = planes.count == 2 ? .anyPlane : planes.contains("horizontal") ? .horizontalPlane : planes.contains("vertical") ? .verticalPlane : .tracking
        if coaching == nil {
            overlay.session = arView.session
            overlay.activatesAutomatically = true
            overlay.frame = arView.bounds
            overlay.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            arView.addSubview(overlay)
            coaching = overlay
        }
    }

    private var surfaces: Int { mode == .fixture ? 1 : planes.count }

    private func refreshStatus() {
        precondition(Thread.isMainThread)
        var unavailable: String?
        if case .unsupported(let reason) = mode { unavailable = reason }
        unavailable = failure ?? unavailable
        if let unavailable {
            status.text = unavailable
            status.accessibilityIdentifier = "native.ar.unsupported"
        } else {
            status.text = GofastrARStatus.message(tracking: tracking, surfaces: surfaces, detectsPlanes: detectsPlanes, unplaced: nextUnplaced?.title, navigable: objects.contains { $0.navigable && nodes[$0.id]?.parent != nil })
            status.accessibilityIdentifier = "native.ar.status"
        }
        reset.isHidden = sceneView == nil
        UIAccessibility.post(notification: .layoutChanged, argument: nil)
    }

    private func visibilityChanged() {
        guard !disposed, let arView else { return }
        if container.window == nil {
            arView.session.pause()
            running = false
            return
        }
        guard !running else { return }
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .denied, .restricted:
            failure = "Camera access is off. Allow it in Settings to use AR."
        default:
            failure = nil
            arView.session.run(configuration())
            running = true
        }
        refreshStatus()
    }

    private func objectID(at point: CGPoint, in view: SCNView) -> String? {
        let hits = view.hitTest(point, options: [.searchMode: SCNHitTestSearchMode.all.rawValue, .ignoreHiddenNodes: true])
        for hit in hits {
            var node: SCNNode? = hit.node
            while let current = node {
                if let name = current.name, name.hasPrefix(Self.objectPrefix) {
                    let id = String(name.dropFirst(Self.objectPrefix.count))
                    if nodes[id] === current { return id }
                }
                node = current.parent
            }
        }
        return nil
    }

    @discardableResult private func activate(_ id: String) -> Bool {
        guard !disposed, let object = objects.first(where: { $0.id == id }), object.navigable, nodes[id]?.parent != nil else { return false }
        emit?("object.\(id)")
        return true
    }

    @objc private func tapped(_ gesture: UITapGestureRecognizer) {
        guard !disposed, let sceneView, gesture.state == .ended else { return }
        let point = gesture.location(in: sceneView)
        if let id = objectID(at: point, in: sceneView) {
            activate(id)
            return
        }
        guard let next = nextUnplaced, let placement = surface(at: point, in: sceneView) else { return }
        placements[next.id] = placement
        applyObjects()
        refreshStatus()
    }

    private func surface(at point: CGPoint, in view: SCNView) -> Placement? {
        if let floor {
            let hit = view.hitTest(point, options: [.searchMode: SCNHitTestSearchMode.all.rawValue]).first { $0.node === floor }
            return hit.map { Placement(point: $0.worldCoordinates, normal: SCNVector3(0, 1, 0)) }
        }
        guard let arView, running else { return nil }
        let planes = Set(content.planes ?? [])
        let alignment: ARRaycastQuery.TargetAlignment = planes.count == 2 ? .any : planes.contains("vertical") ? .vertical : .horizontal
        guard let query = arView.raycastQuery(from: point, allowing: .existingPlaneGeometry, alignment: alignment),
              let result = arView.session.raycast(query).first else { return nil }
        let transform = result.worldTransform
        return Placement(point: SCNVector3(transform.columns.3.x, transform.columns.3.y, transform.columns.3.z),
                         normal: SCNVector3(transform.columns.1.x, transform.columns.1.y, transform.columns.1.z))
    }

    @objc private func resetTapped() {
        guard !disposed else { return }
        placements.removeAll()
        planes.values.forEach { $0.removeFromParentNode() }
        planes.removeAll()
        tracking = nil
        if let arView, running {
            arView.session.run(configuration(), options: [.resetTracking, .removeExistingAnchors])
        }
        applyObjects()
        refreshStatus()
    }

    private func accessibleElements() -> [Any] {
        var result: [Any] = [status]
        if let sceneView {
            for object in objects {
                guard let node = nodes[object.id], node.parent != nil, let frame = projectedFrame(node, in: sceneView) else { continue }
                let element = elements[object.id] ?? GofastrARObjectElement(accessibilityContainer: container)
                let id = object.id
                element.activate = { [weak self] in self?.activate(id) ?? false }
                element.accessibilityIdentifier = "native.ar.object.\(object.id)"
                element.accessibilityLabel = object.title
                element.accessibilityTraits = object.navigable ? .button : .image
                element.accessibilityFrameInContainerSpace = sceneView.convert(frame, to: container)
                elements[object.id] = element
                result.append(element)
            }
        }
        if !reset.isHidden { result.append(reset) }
        return result
    }

    private func projectedFrame(_ node: SCNNode, in view: SCNView) -> CGRect? {
        let (low, high) = node.boundingBox
        var rect = CGRect.null
        for x in [low.x, high.x] {
            for y in [low.y, high.y] {
                for z in [low.z, high.z] {
                    let projected = view.projectPoint(node.convertPosition(SCNVector3(x, y, z), to: nil))
                    guard projected.z > 0, projected.z < 1 else { return nil }
                    rect = rect.union(CGRect(x: CGFloat(projected.x), y: CGFloat(projected.y), width: 0, height: 0))
                }
            }
        }
        let visible = rect.intersection(view.bounds)
        return visible.isNull || visible.width < 1 || visible.height < 1 ? nil : visible
    }

    private func onMain(_ work: @escaping () -> Void) {
        if Thread.isMainThread { work() } else { DispatchQueue.main.async(execute: work) }
    }

    func renderer(_ renderer: SCNSceneRenderer, didAdd node: SCNNode, for anchor: ARAnchor) {
        guard let anchor = anchor as? ARPlaneAnchor else { return }
        let plane = SCNNode(geometry: SCNPlane(width: CGFloat(anchor.planeExtent.width), height: CGFloat(anchor.planeExtent.height)))
        plane.name = "gofastr.ar.plane"
        plane.eulerAngles.x = -.pi / 2
        plane.simdPosition = anchor.center
        plane.isHidden = true
        plane.geometry?.firstMaterial?.diffuse.contents = UIColor(white: 1, alpha: 0.25)
        plane.geometry?.firstMaterial?.isDoubleSided = true
        node.addChildNode(plane)
        let identifier = anchor.identifier
        onMain { [weak self] in
            guard let self, !self.disposed else { return }
            self.planes[identifier] = plane
            plane.isHidden = !self.content.showsPlanes
            self.refreshStatus()
        }
    }

    func renderer(_ renderer: SCNSceneRenderer, didUpdate node: SCNNode, for anchor: ARAnchor) {
        guard let anchor = anchor as? ARPlaneAnchor, let plane = node.childNode(withName: "gofastr.ar.plane", recursively: false),
              let geometry = plane.geometry as? SCNPlane else { return }
        geometry.width = CGFloat(anchor.planeExtent.width)
        geometry.height = CGFloat(anchor.planeExtent.height)
        plane.simdPosition = anchor.center
    }

    func renderer(_ renderer: SCNSceneRenderer, didRemove node: SCNNode, for anchor: ARAnchor) {
        guard anchor is ARPlaneAnchor else { return }
        let identifier = anchor.identifier
        onMain { [weak self] in
            guard let self, !self.disposed, self.planes.removeValue(forKey: identifier) != nil else { return }
            self.refreshStatus()
        }
    }

    func session(_ session: ARSession, cameraDidChangeTrackingState camera: ARCamera) {
        let message: String?
        switch camera.trackingState {
        case .normal: message = nil
        case .notAvailable: message = "Tracking is unavailable."
        case .limited(.initializing): message = "Starting AR. Move the device slowly."
        case .limited(.excessiveMotion): message = "Slow down: the device is moving too fast."
        case .limited(.insufficientFeatures): message = "Point at a well-lit surface with more detail."
        case .limited(.relocalizing): message = "Resuming AR. Point back where you were."
        case .limited: message = "Tracking is limited."
        }
        onMain { [weak self] in
            guard let self, !self.disposed else { return }
            self.tracking = message
            self.refreshStatus()
        }
    }

    func session(_ session: ARSession, didFailWithError error: Error) {
        let message = (error as? ARError)?.code == .cameraUnauthorized
            ? "Camera access is off. Allow it in Settings to use AR."
            : "AR stopped: \(error.localizedDescription)"
        onMain { [weak self] in
            guard let self, !self.disposed else { return }
            self.failure = message
            self.running = false
            self.refreshStatus()
        }
    }

    func sessionWasInterrupted(_ session: ARSession) {
        onMain { [weak self] in
            guard let self, !self.disposed else { return }
            self.tracking = "AR is paused while the camera is unavailable."
            self.refreshStatus()
        }
    }

    func sessionInterruptionEnded(_ session: ARSession) {
        onMain { [weak self] in
            guard let self, !self.disposed else { return }
            self.tracking = nil
            self.refreshStatus()
        }
    }

    func dispose() {
        guard !disposed else { return }
        disposed = true
        generation = UUID()
        emit = nil
        container.visibilityChanged = nil
        container.elementsProvider = nil
        arView?.session.pause()
        arView?.delegate = nil
        running = false
        coaching?.session = nil
        coaching?.removeFromSuperview()
        coaching = nil
        nodes.values.forEach { $0.removeFromParentNode() }
        nodes.removeAll()
        planes.values.forEach { $0.removeFromParentNode() }
        planes.removeAll()
        placements.removeAll()
        elements.removeAll()
        sceneView?.gestureRecognizers?.forEach { sceneView?.removeGestureRecognizer($0) }
    }
}
#endif
