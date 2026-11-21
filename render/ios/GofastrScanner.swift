import Foundation

struct MobileScanRequest: Codable {
    let title: String
    let symbologies: [String]?
    static let names = ["qr", "ean13", "ean8", "upce", "code39", "code93", "code128", "itf14", "pdf417", "aztec", "datamatrix"]
    var selected: [String] { symbologies?.isEmpty == false ? symbologies! : Self.names }

    func validate() throws {
        guard !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, title.utf8.count <= 200,
              !title.unicodeScalars.contains(where: CharacterSet.controlCharacters.contains),
              Set(selected).count == selected.count, selected.allSatisfy(Self.names.contains) else { throw MobileServiceFailure.invalid }
    }
}

struct MobileScanResult: Codable {
    let value: String
    let symbology: String
    var wire: [String: Any] { ["value": value, "symbology": symbology] }
}

#if canImport(UIKit)
import UIKit
import AVFoundation
import Vision
import CoreImage

final class MobileScannerController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    private let request: MobileScanRequest
    private let completion: (MobileServiceResult) -> Void
    private let session = AVCaptureSession()
    private let queue = DispatchQueue(label: "gofastr.scanner.capture")
    private var preview: AVCaptureVideoPreviewLayer?
    private var settled = false
    private var started = false
    private static let types: [String: AVMetadataObject.ObjectType] = [
        "qr": .qr, "ean13": .ean13, "ean8": .ean8, "upce": .upce,
        "code39": .code39, "code93": .code93, "code128": .code128,
        "itf14": .itf14, "pdf417": .pdf417, "aztec": .aztec, "datamatrix": .dataMatrix
    ]

    static var available: Bool {
        #if targetEnvironment(simulator)
        if ProcessInfo.processInfo.environment["GOFASTR_SCANNER_FIXTURE"] != nil { return true }
        #endif
        let purpose = Bundle.main.object(forInfoDictionaryKey: "NSCameraUsageDescription") as? String
        return purpose?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false && AVCaptureDevice.default(for: .video) != nil
    }

    init(request: MobileScanRequest, completion: @escaping (MobileServiceResult) -> Void) {
        self.request = request
        self.completion = completion
        super.init(nibName: nil, bundle: nil)
        title = request.title
        isModalInPresentation = true
    }
    required init?(coder: NSCoder) { fatalError("init(coder:) has not been implemented") }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        view.accessibilityIdentifier = "gofastr.scanner.sheet"
        navigationItem.leftBarButtonItem = UIBarButtonItem(title: "Cancel", style: .plain, target: self, action: #selector(cancel))
        navigationItem.leftBarButtonItem?.accessibilityIdentifier = "gofastr.scanner.cancel"
        NotificationCenter.default.addObserver(self, selector: #selector(cancel), name: UIApplication.didEnterBackgroundNotification, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(cancel), name: AVCaptureSession.wasInterruptedNotification, object: session)
        NotificationCenter.default.addObserver(self, selector: #selector(captureFailed), name: AVCaptureSession.runtimeErrorNotification, object: session)
        #if targetEnvironment(simulator)
        if ProcessInfo.processInfo.environment["GOFASTR_SCANNER_FIXTURE"] != nil {
            let button = UIButton(type: .system)
            button.setTitle("Scan test code", for: .normal)
            button.accessibilityIdentifier = "gofastr.scanner.test"
            button.addTarget(self, action: #selector(scanFixture), for: .touchUpInside)
            button.translatesAutoresizingMaskIntoConstraints = false
            view.addSubview(button)
            NSLayoutConstraint.activate([button.centerXAnchor.constraint(equalTo: view.centerXAnchor), button.centerYAnchor.constraint(equalTo: view.centerYAnchor)])
            return
        }
        #endif
        let layer = AVCaptureVideoPreviewLayer(session: session)
        layer.videoGravity = .resizeAspectFill
        view.layer.insertSublayer(layer, at: 0)
        preview = layer
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        preview?.frame = view.bounds
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        guard !started else { return }
        started = true
        #if targetEnvironment(simulator)
        if ProcessInfo.processInfo.environment["GOFASTR_SCANNER_FIXTURE"] != nil { return }
        #endif
        guard Self.available else { finish(MobileServiceResult(status: "unsupported")); return }
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: startCapture()
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { [weak self] granted in
                DispatchQueue.main.async {
                    guard let self, !self.settled else { return }
                    if granted { self.startCapture() } else { self.finish(MobileServiceResult(status: "denied")) }
                }
            }
        default: finish(MobileServiceResult(status: "denied"))
        }
    }

    private func startCapture() {
        let selected = request.selected.compactMap { Self.types[$0] }
        queue.async { [self] in
            do {
                guard let device = AVCaptureDevice.default(for: .video) else {
                    DispatchQueue.main.async { self.finish(MobileServiceResult(status: "unsupported")) }; return
                }
                let input = try AVCaptureDeviceInput(device: device)
                let output = AVCaptureMetadataOutput()
                session.beginConfiguration()
                guard session.canAddInput(input), session.canAddOutput(output) else {
                    session.commitConfiguration()
                    DispatchQueue.main.async { self.finish(MobileServiceResult(status: "unsupported")) }; return
                }
                session.addInput(input)
                session.addOutput(output)
                output.setMetadataObjectsDelegate(self, queue: .main)
                let supported = selected.filter(output.availableMetadataObjectTypes.contains)
                guard !supported.isEmpty else {
                    session.commitConfiguration()
                    DispatchQueue.main.async { self.finish(MobileServiceResult(status: "unsupported")) }; return
                }
                output.metadataObjectTypes = supported
                session.commitConfiguration()
                session.startRunning()
            } catch {
                DispatchQueue.main.async { self.finish(MobileServiceResult(status: "failed")) }
            }
        }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput metadataObjects: [AVMetadataObject], from connection: AVCaptureConnection) {
        for case let code as AVMetadataMachineReadableCodeObject in metadataObjects {
            if let value = code.stringValue, let name = Self.types.first(where: { $0.value == code.type })?.key {
                accept(value, symbology: name)
                if settled { return }
            }
        }
    }

    private func accept(_ value: String, symbology: String) {
        guard !value.isEmpty, value.utf8.count <= 4096, request.selected.contains(symbology) else { return }
        finish(MobileServiceResult(status: "completed", scan: MobileScanResult(value: value, symbology: symbology)))
    }

    @objc func cancel() { finish(MobileServiceResult(status: "cancelled")) }
    @objc private func captureFailed() { finish(MobileServiceResult(status: "failed")) }

    private func finish(_ result: MobileServiceResult) {
        guard Thread.isMainThread else { DispatchQueue.main.async { self.finish(result) }; return }
        guard !settled else { return }
        settled = true
        NotificationCenter.default.removeObserver(self)
        queue.async { [self] in
            if session.isRunning { session.stopRunning() }
            DispatchQueue.main.async { self.completion(result) }
        }
    }

    #if targetEnvironment(simulator)
    @objc private func scanFixture() {
        guard let text = ProcessInfo.processInfo.environment["GOFASTR_SCANNER_FIXTURE"] else { return }
        queue.async { [self] in
            do {
                guard let generator = CIFilter(name: "CIQRCodeGenerator") else { throw MobileServiceFailure.invalid }
                generator.setValue(Data(text.utf8), forKey: "inputMessage")
                guard let code = generator.outputImage else { throw MobileServiceFailure.invalid }
                let background = CIImage(color: .white).cropped(to: code.extent.insetBy(dx: -4, dy: -4))
                let image = code.composited(over: background).transformed(by: CGAffineTransform(scaleX: 10, y: 10))
                guard let rendered = CIContext(options: [.useSoftwareRenderer: true]).createCGImage(image, from: image.extent) else { throw MobileServiceFailure.invalid }
                let detection = VNDetectBarcodesRequest()
                detection.revision = VNDetectBarcodesRequestRevision1
                detection.symbologies = [.qr]
                try VNImageRequestHandler(cgImage: rendered).perform([detection])
                guard let value = detection.results?.first?.payloadStringValue, request.selected.contains("qr"), !value.isEmpty, value.utf8.count <= 4096 else { throw MobileServiceFailure.invalid }
                DispatchQueue.main.async { self.accept(value, symbology: "qr") }
            } catch {
                DispatchQueue.main.async { self.finish(MobileServiceResult(status: "failed")) }
            }
        }
    }
    #endif

    deinit { NotificationCenter.default.removeObserver(self) }
}
#endif
