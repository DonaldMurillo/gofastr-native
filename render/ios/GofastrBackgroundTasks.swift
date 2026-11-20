import BackgroundTasks
import UIKit

/// Periodic background refresh through BGAppRefreshTask. The app registers once,
/// before launch finishes; Go code schedules, cancels and inspects it through
/// the `refresh` device service and supplies the work that runs.
public enum GofastrBackgroundRefresh {
    static let infoKey = "GofastrBackgroundRefresh"
    /// Keep the run inside the roughly 30 seconds iOS grants an app refresh task.
    static let runMillis: Int64 = 25_000
    private static let queue = DispatchQueue(label: "gofastr.native.background-refresh", qos: .utility)
    private static var registered = false

    static var identifier: String? {
        guard let value = Bundle.main.object(forInfoDictionaryKey: infoKey) as? String, MobileRefreshIdentifier.valid(value),
              (Bundle.main.object(forInfoDictionaryKey: "BGTaskSchedulerPermittedIdentifiers") as? [String])?.contains(value) == true else { return nil }
        return value
    }

    static var enabled: Bool { identifier != nil }

    /// Call from `application(_:didFinishLaunchingWithOptions:)`. `run` performs
    /// one refresh within the given milliseconds and must return when `cancel`
    /// is called; `ready` delays the run until the app's runtime has started.
    public static func register(ready: @escaping (@escaping () -> Void) -> Void = { $0() },
                                run: @escaping (Int64) throws -> Void, cancel: @escaping () -> Void) {
        guard Thread.isMainThread, !registered, let identifier else { return }
        registered = BGTaskScheduler.shared.register(forTaskWithIdentifier: identifier, using: nil) { task in
            handle(task, ready: ready, run: run, cancel: cancel)
        }
    }

    private static func handle(_ task: BGTask, ready: @escaping (@escaping () -> Void) -> Void,
                               run: @escaping (Int64) throws -> Void, cancel: @escaping () -> Void) {
        let completion = MobileRefreshCompletion(task)
        task.expirationHandler = {
            cancel()
            completion.finish(false)
        }
        // Submit the next run first so a crash or expiry still leaves one pending.
        // `perform` runs on the main thread too, so a cancel is never undone here.
        // The run starts only after that, so the task never completes before its replacement exists.
        DispatchQueue.main.async {
            if let minutes = MobileRefreshPreference(defaults: .standard).intervalMinutes { _ = submit(minutes) }
            ready {
                queue.async {
                    guard completion.begin() else { return }
                    let succeeded = (try? run(runMillis)) != nil
                    completion.finish(succeeded)
                }
            }
        }
    }

    static func perform(_ request: MobileRefreshRequest, completion: @escaping (MobileServiceResult) -> Void) {
        guard identifier != nil else { completion(MobileServiceResult(status: "unsupported")); return }
        switch UIApplication.shared.backgroundRefreshStatus {
        case .denied:
            completion(MobileServiceResult(status: "denied", message: "Background App Refresh is off for this app in Settings.", permission: "denied")); return
        case .restricted:
            completion(MobileServiceResult(status: "denied", message: "Background App Refresh is restricted on this device.", permission: "restricted")); return
        default: break
        }
        let preference = MobileRefreshPreference(defaults: .standard)
        switch request.operation {
        case "schedule":
            guard let minutes = request.intervalMinutes else { completion(MobileServiceResult(status: "failed")); return }
            if let failure = submit(minutes) { completion(failure); return }
            preference.save(minutes)
            completion(result(scheduled: true))
        case "cancel":
            preference.save(nil)
            if let identifier { BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: identifier) }
            completion(result(scheduled: false))
        default:
            BGTaskScheduler.shared.getPendingTaskRequests { requests in
                let pending = requests.contains { $0.identifier == identifier }
                DispatchQueue.main.async { completion(result(scheduled: pending)) }
            }
        }
    }

    private static func result(scheduled: Bool) -> MobileServiceResult {
        var result = MobileServiceResult(status: "completed")
        result.refresh = MobileRefreshState(scheduled: scheduled)
        return result
    }

    private static func submit(_ minutes: Int) -> MobileServiceResult? {
        guard let identifier else { return MobileServiceResult(status: "unsupported") }
        let request = BGAppRefreshTaskRequest(identifier: identifier)
        request.earliestBeginDate = Date(timeIntervalSinceNow: TimeInterval(minutes * 60))
        do {
            try BGTaskScheduler.shared.submit(request)
            return nil
        } catch BGTaskScheduler.Error.unavailable {
            return MobileServiceResult(status: "unsupported", message: "Background refresh is unavailable on this device.")
        } catch BGTaskScheduler.Error.notPermitted {
            return MobileServiceResult(status: "unsupported", message: "The app is not configured for background refresh.")
        } catch {
            return MobileServiceResult(status: "failed", message: "Couldn’t schedule background refresh.")
        }
    }
}

/// Reports a task's completion exactly once, whichever of expiry or the run ends first.
private final class MobileRefreshCompletion {
    private let lock = NSLock()
    private var task: BGTask?

    init(_ task: BGTask) { self.task = task }

    /// False once the task has expired, so expired work never starts.
    func begin() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return task != nil
    }

    func finish(_ success: Bool) {
        lock.lock()
        let task = self.task
        self.task = nil
        lock.unlock()
        task?.setTaskCompleted(success: success)
    }
}
