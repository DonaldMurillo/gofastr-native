import UIKit
import Contacts
import ContactsUI
import UserNotifications
import AVFoundation
import SafariServices

public final class GofastrServiceRunner: NSObject, CNContactPickerDelegate, SFSafariViewControllerDelegate {
    private var contactsRequest: MobileContactsRequest?
    private weak var presenter: UIViewController?
    private let openRoute: (String) -> Bool
    private let queue = DispatchQueue(label: "gofastr.native.services", qos: .userInitiated)
    private var settlement = MobileServiceSettlement()
    private var completion: ((String) -> Void)?
    private var controller: UIViewController?
    private var directory: URL?
    private var dismissing = false
    private var scheduling = false
    private var cancellingSchedule = false
    private let calendar = GofastrCalendar()
    private var calendarActive = false
    private let secureStorage = GofastrSecureStorage()
    private var secureStorageActive = false
    private let location = GofastrLocation(isForeground: { UIApplication.shared.applicationState != .background })
    private var locationActive = false
    private var durableServiceActive = false
    private var authentication: MobileAuthentication?
    private lazy var dismissalDelegate = MobileServiceDismissalDelegate { [weak self] in self?.cancel() }

    public init(presenter: UIViewController, openRoute: @escaping (String) -> Bool) {
        self.presenter = presenter
        self.openRoute = openRoute
        super.init()
        MobileNotificationRouter.shared.install(openRoute)
    }

    public static func capabilities() -> [String: String] {
        let purpose = Bundle.main.object(forInfoDictionaryKey: "NSMicrophoneUsageDescription") as? String
        let iap: String
        if #available(iOS 15.0, *) { iap = "available" } else { iap = "unavailable" }
        return ["browser/v1": "available", "contacts/v1": "available", "notifications/v1": "available", "sharing/v1": "available", "clipboard/v1": "available",
                "iap/v1": iap,
                "scanner/v1": MobileScannerController.available ? "available" : "unavailable",
                "calendar/v1": GofastrCalendar.enabled ? "available" : "unavailable",
                "push/v1": Bundle.main.object(forInfoDictionaryKey: "GofastrPush") as? Bool == true ? "available" : "unavailable",
                "secure-storage/v1": GofastrSecureStorage.enabled ? "available" : "unavailable",
                "location/v1": GofastrLocation.enabled ? "available" : "unavailable",
                "incoming-shares/v1": Bundle.main.object(forInfoDictionaryKey: "GofastrReceiveShares") as? Bool == true ? "available" : "unavailable",
                "widgets/v1": Bundle.main.object(forInfoDictionaryKey: "GofastrWidgets") as? Bool == true ? "available" : "unavailable",
                "sign-in-with-apple/v1": MobileAuthentication.appleEnabled ? "available" : "unavailable",
                "oauth-web-login/v1": "available",
                "background-refresh/v1": GofastrBackgroundRefresh.enabled ? "available" : "unavailable",
                "audio/v1": purpose?.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty == false ? "available" : "unavailable"]
    }

    public func perform(requestJSON: String, completion: @escaping (String) -> Void) {
        guard Thread.isMainThread else {
            DispatchQueue.main.async { self.perform(requestJSON: requestJSON, completion: completion) }
            return
        }
        guard !dismissing, let token = settlement.begin() else {
            completion(MobileServiceResult(status: "failed", message: "Another device service is active.").json)
            return
        }
        self.completion = completion
        guard requestJSON.utf8.count <= MobileServiceBounds.maximumJSON else {
            finish(token, MobileServiceResult(status: "failed", message: MobileServiceFailure.tooLarge.localizedDescription))
            return
        }
        MobileNotificationRouter.shared.flush()
        queue.async {
            let decoded = Result { () throws -> MobileServiceRequest in
                let request = try JSONDecoder().decode(MobileServiceRequest.self, from: Data(requestJSON.utf8))
                try request.validate()
                return request
            }
            DispatchQueue.main.async {
                guard self.settlement.active == token else { return }
                switch decoded {
                case .failure(let error): self.finish(token, MobileServiceResult(status: "failed", message: error.localizedDescription))
                case .success(let request): self.execute(request, token: token)
                }
            }
        }
    }

    public func cancel() {
        guard Thread.isMainThread else { DispatchQueue.main.async { self.cancel() }; return }
        guard let token = settlement.active else { return }
        if let scanner = (controller as? UINavigationController)?.viewControllers.first as? MobileScannerController { scanner.cancel(); return }
        if calendarActive { calendar.cancel(); return }
        if secureStorageActive { secureStorage.cancel(); return }
        if locationActive { location.cancel(); return }
        if let authentication { authentication.cancel(); return }
        if durableServiceActive { return }
        if scheduling { cancellingSchedule = true; return }
        finish(token, MobileServiceResult(status: "cancelled", message: "Device service cancelled."))
    }

    private func execute(_ request: MobileServiceRequest, token: UUID) {
        if request.deviceInfo != nil {
            finish(token, MobileServiceResult(status: "unsupported", message: "Device snapshots are unavailable on this renderer."))
            return
        }
        if let locationRequest = request.location {
            locationActive = true
            location.perform(locationRequest) { [weak self] result in
                self?.locationActive = false
                self?.finish(token, result)
            }
            return
        }
        if let request = request.calendar {
            calendarActive = true
            calendar.perform(request) { [weak self] result in
                self?.calendarActive = false
                self?.finish(token, result)
            }
            return
        }
        if let push = request.push {
            guard Self.capabilities()["push/v1"] == "available" else { finish(token, MobileServiceResult(status: "unsupported")); return }
            guard !MobilePushRequest.inboxOperations.contains(push.operation) else {
                finish(token, MobileServiceResult(status: "unsupported", message: "iOS has no push inbox; it opens a tapped notification's route directly."))
                return
            }
            durableServiceActive = true
            GofastrPush.perform(push) { [weak self] result in
                self?.durableServiceActive = false
                self?.finish(token, result)
            }
            return
        }
        if let iap = request.iap {
            guard #available(iOS 15.0, *) else {
                finish(token, MobileServiceResult(status: "unsupported", message: "In-app purchases require iOS 15 or later."))
                return
            }
            durableServiceActive = true
            Task { @MainActor in
                let result = await GofastrStoreKit.shared.perform(iap, presenter: self.presenter)
                self.durableServiceActive = false
                self.finish(token, result)
            }
            return
        }
        if let secure = request.secureStorage {
            secureStorageActive = true
            secureStorage.perform(secure) { [weak self] result in
                self?.secureStorageActive = false
                self?.finish(token, result)
            }
            return
        }
        if let incoming = request.incomingShare {
            guard Self.capabilities()["incoming-shares/v1"] == "available" else { finish(token, MobileServiceResult(status: "unsupported")); return }
            durableServiceActive = true
            MobileIncomingShares(groupIdentifier: Bundle.main.object(forInfoDictionaryKey: "GofastrAppGroup") as? String ?? "").perform(incoming) { [weak self] result in self?.finishDictionary(token, result) }
            return
        }
        if let widget = request.widget {
            guard Self.capabilities()["widgets/v1"] == "available" else { finish(token, MobileServiceResult(status: "unsupported")); return }
            durableServiceActive = true
            MobileWidgetStore(appGroup: Bundle.main.object(forInfoDictionaryKey: "GofastrAppGroup") as? String ?? "").perform(widget) { [weak self] result in self?.finishDictionary(token, result) }
            return
        }
        if let refresh = request.refresh {
            durableServiceActive = true
            GofastrBackgroundRefresh.perform(refresh) { [weak self] result in
                guard let self, self.settlement.active == token else { return }
                self.durableServiceActive = false
                self.finish(token, result)
            }
            return
        }
        if let notification = request.notification { performNotification(notification, token: token); return }
        if let clipboard = request.clipboard {
            UIPasteboard.general.string = clipboard.text
            finish(token, MobileServiceResult(status: "completed"))
            return
        }
        guard let presenter, let window = presenter.viewIfLoaded?.window, presenter.presentedViewController == nil else {
            finish(token, MobileServiceResult(status: "failed", message: "The service has no available foreground presenter."))
            return
        }
        if let contacts = request.contacts { performContacts(contacts, token: token, presenter: presenter) }
        else if let share = request.share { performShare(share, token: token, presenter: presenter) }
        else if let browser = request.browser { performBrowser(browser, presenter: presenter) }
        else if let audio = request.audio { performAudio(audio, token: token, presenter: presenter) }
        else if let scan = request.scan {
            guard MobileScannerController.available else { finish(token, MobileServiceResult(status: "unsupported")); return }
            let scanner = MobileScannerController(request: scan) { [weak self] result in self?.finish(token, result) }
            let navigation = UINavigationController(rootViewController: scanner)
            navigation.isModalInPresentation = true
            controller = navigation
            presenter.present(navigation, animated: true)
            navigation.presentationController?.delegate = dismissalDelegate
        }
        else if let signIn = request.authentication {
            let task = MobileAuthentication(anchor: window) { [weak self] result in
                guard let self, self.settlement.active == token else { return }
                self.authentication = nil
                self.finish(token, result)
            }
            authentication = task
            task.start(signIn)
        }
    }

    private func performBrowser(_ request: MobileBrowserRequest, presenter: UIViewController) {
        precondition(Thread.isMainThread)
        guard let url = URL(string: request.url) else { return }
        let configuration = SFSafariViewController.Configuration()
        configuration.entersReaderIfAvailable = request.readerMode ?? false
        let browser = SFSafariViewController(url: url, configuration: configuration)
        browser.dismissButtonStyle = request.dismissLabel == "close" ? .close : request.dismissLabel == "cancel" ? .cancel : .done
        browser.delegate = self
        browser.view.accessibilityIdentifier = "gofastr.browser"
        controller = browser
        presenter.present(browser, animated: true)
    }

    public func safariViewControllerDidFinish(_ controller: SFSafariViewController) {
        precondition(Thread.isMainThread)
        guard self.controller === controller, let token = settlement.active else { return }
        finish(token, MobileServiceResult(status: "completed"))
    }

    private func finishDictionary(_ token: UUID, _ value: [String: Any]) {
        guard settlement.active == token else { return }
        durableServiceActive = false
        guard let data = try? JSONSerialization.data(withJSONObject: value),
              let result = try? JSONDecoder().decode(MobileServiceResult.self, from: data) else {
            finish(token, MobileServiceResult(status: "failed")); return
        }
        finish(token, result)
    }

    private func performContacts(_ request: MobileContactsRequest, token: UUID, presenter: UIViewController) {
        contactsRequest = request
        let picker = CNContactPickerViewController()
        let keys = ["phone": CNContactPhoneNumbersKey, "email": CNContactEmailAddressesKey, "postal": CNContactPostalAddressesKey, "organization": CNContactOrganizationNameKey]
        picker.displayedPropertyKeys = (request.fields ?? []).compactMap { keys[$0] }
        picker.delegate = self
        controller = picker
        presenter.present(picker, animated: true)
        picker.presentationController?.delegate = dismissalDelegate
    }

    public override func responds(to selector: Selector!) -> Bool {
        if selector == NSSelectorFromString("contactPicker:didSelectContacts:") {
            return contactsRequest?.multiple == true
        }
        if selector == NSSelectorFromString("contactPicker:didSelectContact:") { return contactsRequest?.multiple != true }
        return super.responds(to: selector)
    }

    public func contactPickerDidCancel(_ picker: CNContactPickerViewController) {
        guard controller === picker else { return }
        cancel()
    }
    public func contactPicker(_ picker: CNContactPickerViewController, didSelect contact: CNContact) { selectedContacts([contact], picker: picker) }
    public func contactPicker(_ picker: CNContactPickerViewController, didSelect contacts: [CNContact]) { selectedContacts(contacts, picker: picker) }

    private func selectedContacts(_ contacts: [CNContact], picker: CNContactPickerViewController) {
        guard controller === picker, let request = contactsRequest, let token = settlement.active else { return }
        let fields = Set(request.fields ?? [])
        func bounded(_ value: String, maximum: Int = 512, multiline: Bool = false) -> String {
            var result = ""
            for scalar in value.unicodeScalars {
                if CharacterSet.controlCharacters.contains(scalar) && !(multiline && (scalar == "\n" || scalar == "\t")) { continue }
                guard result.utf8.count + scalar.utf8.count <= maximum else { break }
                result.unicodeScalars.append(scalar)
            }
            return result
        }
        let selection = MobileContactSelection(contacts: contacts.prefix(request.multiple ? 50 : 1).map { contact in
            var value = MobileContact(id: bounded(contact.identifier), givenName: bounded(contact.givenName), familyName: bounded(contact.familyName))
            if fields.contains("organization"), contact.isKeyAvailable(CNContactOrganizationNameKey) { value.organization = bounded(contact.organizationName) }
            if fields.contains("phone"), contact.isKeyAvailable(CNContactPhoneNumbersKey) {
                value.phones = contact.phoneNumbers.prefix(20).map { MobileContactLabeled(label: bounded(CNLabeledValue<NSString>.localizedString(forLabel: $0.label ?? "")), value: bounded($0.value.stringValue)) }
            }
            if fields.contains("email"), contact.isKeyAvailable(CNContactEmailAddressesKey) {
                value.emails = contact.emailAddresses.prefix(20).map { MobileContactLabeled(label: bounded(CNLabeledValue<NSString>.localizedString(forLabel: $0.label ?? "")), value: bounded($0.value as String)) }
            }
            if fields.contains("postal"), contact.isKeyAvailable(CNContactPostalAddressesKey) {
                value.postal = contact.postalAddresses.prefix(20).map { bounded(CNPostalAddressFormatter.string(from: $0.value, style: .mailingAddress), maximum: 2048, multiline: true) }
            }
            return value
        })
        do {
            try request.accept(selection)
            finish(token, MobileServiceResult(status: "completed", contacts: selection))
        } catch {
            finish(token, MobileServiceResult(status: "failed", message: "The contact selection is invalid."))
        }
    }

    private func performShare(_ request: MobileShareRequest, token: UUID, presenter: UIViewController) {
        queue.async {
            let prepared = Result { try request.prepare() }
            DispatchQueue.main.async {
                guard self.settlement.active == token else {
                    if case .success(let result) = prepared { self.remove(result.0) }
                    return
                }
                switch prepared {
                case .failure(let error): self.finish(token, MobileServiceResult(status: "failed", message: error.localizedDescription))
                case .success(let result):
                    self.directory = result.0
                    var items: [Any] = []
                    if let text = request.text, !text.isEmpty { items.append(text) }
                    items.append(contentsOf: result.1)
                    let sheet = UIActivityViewController(activityItems: items, applicationActivities: nil)
                    sheet.title = request.title
                    sheet.popoverPresentationController?.sourceView = presenter.view
                    sheet.popoverPresentationController?.sourceRect = CGRect(x: presenter.view.bounds.midX, y: presenter.view.bounds.midY, width: 1, height: 1)
                    sheet.completionWithItemsHandler = { [weak self] _, completed, _, error in
                        DispatchQueue.main.async {
                            self?.finish(token, MobileServiceResult(status: error != nil ? "failed" : completed ? "completed" : "cancelled", message: error?.localizedDescription))
                        }
                    }
                    self.controller = sheet
                    presenter.present(sheet, animated: true)
                    sheet.presentationController?.delegate = self.dismissalDelegate
                }
            }
        }
    }

    private func performAudio(_ request: MobileAudioRequest, token: UUID, presenter: UIViewController) {
        guard Self.capabilities()["audio/v1"] == "available" else {
            finish(token, MobileServiceResult(status: "unsupported", message: "Microphone purpose text is missing."))
            return
        }
        let audio = MobileAudioRecorderController(request: request, queue: queue) { [weak self] result in
            self?.finish(token, result)
        }
        let navigation = UINavigationController(rootViewController: audio)
        navigation.isModalInPresentation = true
        controller = navigation
        presenter.present(navigation, animated: true)
        navigation.presentationController?.delegate = dismissalDelegate
    }

    private func finish(_ token: UUID, _ result: MobileServiceResult) {
        precondition(Thread.isMainThread)
        guard settlement.finish(token) else { return }
        scheduling = false
        cancellingSchedule = false
        let completion = self.completion
        self.completion = nil
        let presented = controller
        controller = nil
        let recording = (presented as? UINavigationController)?.viewControllers.first as? MobileAudioRecorderController
        recording?.shutdown()
        let temporary = directory
        directory = nil
        dismissing = true
        MobileForegroundDismissal.dismiss(presented) {
            self.queue.async {
                if let temporary { try? FileManager.default.removeItem(at: temporary) }
                let encoded = result.json
                DispatchQueue.main.async {
                    self.dismissing = false
                    completion?(encoded)
                }
            }
        }
    }

    private func remove(_ directory: URL) {
        queue.async { try? FileManager.default.removeItem(at: directory) }
    }

    private func performNotification(_ request: MobileNotificationRequest, token: UUID) {
        let center = UNUserNotificationCenter.current()
        let identifier = MobileNotificationRouter.prefix + request.identifier
        if request.operation == "cancel" {
            center.removePendingNotificationRequests(withIdentifiers: [identifier])
            center.removeDeliveredNotifications(withIdentifiers: [identifier])
            finish(token, MobileServiceResult(status: "completed", identifier: request.identifier))
            return
        }
        if request.operation == "status" {
            center.getNotificationSettings { settings in
                center.getPendingNotificationRequests { requests in
                    let scheduled = requests.contains { $0.identifier == identifier }
                    DispatchQueue.main.async {
                        self.finish(token, MobileServiceResult(status: "completed", message: scheduled ? "scheduled" : "not scheduled",
                                                              permission: Self.permission(settings.authorizationStatus), identifier: request.identifier))
                    }
                }
            }
            return
        }
        do { _ = try request.scheduledDate() }
        catch { finish(token, MobileServiceResult(status: "failed", message: "Choose a valid future local date and time.")); return }
        center.requestAuthorization(options: [.alert, .sound]) { granted, error in
            DispatchQueue.main.async {
                guard self.settlement.active == token else { return }
                guard error == nil, granted else {
                    self.finish(token, MobileServiceResult(status: error == nil ? "denied" : "failed", message: error?.localizedDescription,
                                                          permission: granted ? "granted" : "denied", identifier: request.identifier))
                    return
                }
                do {
                    let date = try request.scheduledDate()
                    let content = UNMutableNotificationContent()
                    content.title = request.title ?? ""
                    content.body = request.body ?? ""
                    content.sound = .default
                    content.userInfo = [MobileNotificationRouter.routeKey: request.route ?? ""]
                    var calendar = Calendar(identifier: .gregorian)
                    calendar.timeZone = .current
                    var components = calendar.dateComponents([.year, .month, .day, .hour, .minute], from: date)
                    components.calendar = calendar
                    components.timeZone = calendar.timeZone
                    let notification = UNNotificationRequest(identifier: identifier, content: content,
                        trigger: UNCalendarNotificationTrigger(dateMatching: components, repeats: false))
                    self.scheduling = true
                    center.add(notification) { error in
                        DispatchQueue.main.async {
                            guard self.settlement.active == token else { return }
                            if self.cancellingSchedule {
                                center.removePendingNotificationRequests(withIdentifiers: [identifier])
                                center.removeDeliveredNotifications(withIdentifiers: [identifier])
                                self.finish(token, MobileServiceResult(status: "cancelled", message: "Reminder scheduling cancelled.", identifier: request.identifier))
                                return
                            }
                            self.finish(token, MobileServiceResult(status: error == nil ? "completed" : "failed", message: error?.localizedDescription,
                                                                  permission: "granted", identifier: request.identifier))
                        }
                    }
                } catch {
                    self.finish(token, MobileServiceResult(status: "failed", message: "The chosen local time is no longer in the future."))
                }
            }
        }
    }

    private static func permission(_ status: UNAuthorizationStatus) -> String {
        switch status {
        case .authorized, .provisional, .ephemeral: return "granted"
        case .denied: return "denied"
        case .notDetermined: return "prompt"
        @unknown default: return "unsupported"
        }
    }

    deinit {
        if let directory { queue.async { try? FileManager.default.removeItem(at: directory) } }
        let presented = controller
        let authentication = authentication
        let recording = (presented as? UINavigationController)?.viewControllers.first as? MobileAudioRecorderController
        if Thread.isMainThread {
            ((presented as? UINavigationController)?.viewControllers.first as? MobileScannerController)?.cancel()
            authentication?.cancel()
            recording?.shutdown()
            MobileForegroundDismissal.dismiss(presented) {}
        } else {
            DispatchQueue.main.async {
                ((presented as? UINavigationController)?.viewControllers.first as? MobileScannerController)?.cancel()
                authentication?.cancel()
                recording?.shutdown()
                MobileForegroundDismissal.dismiss(presented) {}
            }
        }
    }
}

private final class MobileServiceDismissalDelegate: NSObject, UIAdaptivePresentationControllerDelegate {
    private let cancel: () -> Void
    init(cancel: @escaping () -> Void) { self.cancel = cancel }
    func presentationControllerDidDismiss(_ presentationController: UIPresentationController) { cancel() }
}

public enum GofastrPush {
    private static var token = ""
    private static var active: UUID?
    private static var completion: ((MobileServiceResult) -> Void)?

    public static func prepare() {
        MobileNotificationRouter.shared.installDelegate()
    }

    public static func didRegister(deviceToken: Data) {
        DispatchQueue.main.async {
            guard !deviceToken.isEmpty, deviceToken.count <= 256 else {
                settle(MobileServiceResult(status: "failed", message: "Invalid APNs token."))
                return
            }
            token = deviceToken.map { String(format: "%02x", $0) }.joined()
            guard let generation = active else { return }
            observe { state in
                guard active == generation else { return }
                settle(MobileServiceResult(status: "completed", push: state))
            }
        }
    }

    public static func didFailToRegister(error: Error) {
        DispatchQueue.main.async {
            token = ""
            settle(MobileServiceResult(status: "failed", message: "APNs registration failed."))
        }
    }

    static func perform(_ request: MobilePushRequest, completion: @escaping (MobileServiceResult) -> Void) {
        precondition(Thread.isMainThread)
        guard active == nil else { completion(MobileServiceResult(status: "failed", message: "APNs registration is active.")); return }
        if request.operation == "unregister" {
            UIApplication.shared.unregisterForRemoteNotifications()
            token = ""
        }
        guard request.operation == "register" else {
            observe { completion(MobileServiceResult(status: "completed", push: $0)) }
            return
        }
        let generation = UUID()
        active = generation
        self.completion = completion
        DispatchQueue.main.asyncAfter(deadline: .now() + 45) {
            guard active == generation else { return }
            settle(MobileServiceResult(status: "failed", message: "APNs registration timed out."))
        }
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge]) { allowed, error in
            DispatchQueue.main.async {
                guard active == generation else { return }
                if error != nil { settle(MobileServiceResult(status: "failed", message: "Notification authorization failed.")); return }
                guard allowed else { settle(MobileServiceResult(status: "denied")); return }
                UIApplication.shared.registerForRemoteNotifications()
            }
        }
    }

    private static func settle(_ result: MobileServiceResult) {
        let callback = completion
        completion = nil
        active = nil
        callback?(result)
    }

    private static func observe(_ completion: @escaping (MobilePushState) -> Void) {
        UNUserNotificationCenter.current().getNotificationSettings { settings in
            let profile = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision")
            let environment = profile.flatMap { try? Data(contentsOf: $0, options: .mappedIfSafe) }.flatMap { MobilePushState.environment(profile: $0) }
            DispatchQueue.main.async {
                let permission: String
                switch settings.authorizationStatus {
                case .notDetermined: permission = "notDetermined"
                case .denied: permission = "denied"
                case .authorized: permission = "authorized"
                case .provisional: permission = "provisional"
                case .ephemeral: permission = "ephemeral"
                @unknown default: permission = "notDetermined"
                }
                let registered = UIApplication.shared.isRegisteredForRemoteNotifications
                completion(MobilePushState(permission: permission, registered: registered, token: registered && !token.isEmpty ? token : nil, environment: environment))
            }
        }
    }
}

final class MobileNotificationRouter: NSObject, UNUserNotificationCenterDelegate {
    static let shared = MobileNotificationRouter()
    static let prefix = "gofastr.notification."
    static let routeKey = "gofastr.route"
    private weak var previous: UNUserNotificationCenterDelegate?
    private var openRoute: ((String) -> Bool)?
    private let routes = MobileNotificationRouteQueue()
    private var observer: NSObjectProtocol?

    func install(_ openRoute: @escaping (String) -> Bool) {
        precondition(Thread.isMainThread)
        self.openRoute = openRoute
        installDelegate()
        flush()
    }

    func installDelegate() {
        precondition(Thread.isMainThread)
        let center = UNUserNotificationCenter.current()
        if center.delegate !== self { previous = center.delegate; center.delegate = self }
        if observer == nil {
            observer = NotificationCenter.default.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in self?.flush() }
        }
        flush()
    }

    func flush() {
        routes.flush(openRoute)
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                withCompletionHandler completionHandler: @escaping () -> Void) {
        guard response.actionIdentifier == UNNotificationDefaultActionIdentifier,
              let route = MobilePushRoute.route(remote: response.notification.request.trigger is UNPushNotificationTrigger,
                  identifier: response.notification.request.identifier, userInfo: response.notification.request.content.userInfo) else {
            if let previous, previous.responds(to: #selector(userNotificationCenter(_:didReceive:withCompletionHandler:))) {
                previous.userNotificationCenter?(center, didReceive: response, withCompletionHandler: completionHandler)
            } else { completionHandler() }
            return
        }
        DispatchQueue.main.async {
            self.routes.pending = route
            self.flush()
            completionHandler()
        }
    }

    func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        if MobilePushRoute.shouldPresent(remote: notification.request.trigger is UNPushNotificationTrigger, identifier: notification.request.identifier, userInfo: notification.request.content.userInfo) { completionHandler([.banner, .sound]) }
        else if let previous, previous.responds(to: #selector(userNotificationCenter(_:willPresent:withCompletionHandler:))) {
            previous.userNotificationCenter?(center, willPresent: notification, withCompletionHandler: completionHandler)
        } else { completionHandler([]) }
    }
}
