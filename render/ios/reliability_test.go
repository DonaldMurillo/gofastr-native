package ios

import (
	"strings"
	"testing"
)

func TestReliabilitySwiftCheckpointOrderingAndRestoration(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	sourcePath := swiftSources(t, directory, Sources, "GofastrReliability.swift")[0]
	compileRunChecks(t, directory, "reliability-checks", reliabilityChecks,
		"reliability checks do not compile", "checkpoint, expiration, or retained restoration regression",
		[]string{"-O", sourcePath}, nil)
}

func TestReliabilityUIKitLifecycleContracts(t *testing.T) {
	native := readSource(t, Sources, "GofastrNative.swift")
	requireTokens(t, native, "native reliability contract missing",
		"public func checkpoint(completion: @escaping (Bool) -> Void)", "public func suspend()", "public func resume()",
		"UIApplication.willResignActiveNotification", "UIApplication.didEnterBackgroundNotification", "UIApplication.didBecomeActiveNotification",
		"beginBackgroundTask(withName:", "UIApplication.shared.endBackgroundTask(task)", "batch.expire()", "expireCheckpoints()",
		"retainedScreens.forEach { $0.suspendReads() }", "retainedScreens.forEach { $0.resumeVisibleReads() }",
		"view.window != nil, navigationController?.topViewController === self", "guard liveVisible, host.active",
		"commandSequencer.prepare(operation", "checkpointFence.invalidate()", "checkpointFence.accepts(ticket",
		"checkpointDeferral.hold(completion)", "defer { self.finishCheckpointDeferral() }", "guard remainingReads > 0 else",
		"self.instance == instance, case .success(let reply) = result, reply.error == nil", "bufferRevision = reply?.revision ?? 0")
	start := strings.Index(native, "    public func checkpoint()")
	end := strings.Index(native, "    private func finishStartup()")
	if start < 0 || end <= start {
		t.Fatal("missing lifecycle boundary")
	}
	for _, forbidden := range []string{`send("action"`, `send("respond"`, `send("file-result"`, `send("service-result"`, "host.dispatch(", "host.foreground.show("} {
		if strings.Contains(native[start:end], forbidden) {
			t.Errorf("lifecycle must not replay mutations or OS effects: %s", forbidden)
		}
	}
}

const reliabilityChecks = `
import Foundation

final class CheckpointEvents: @unchecked Sendable {
    private let lock = NSLock()
    private var events: [String] = []
    func append(_ event: String) { lock.lock(); defer { lock.unlock() }; events.append(event) }
    var snapshot: [String] { lock.lock(); defer { lock.unlock() }; return events }
}

@main struct ReliabilityChecks {
    static func main() {
        let sequencer = MobileCommandSequencer()
        let events = CheckpointEvents()
        let saveStarted = DispatchSemaphore(value: 0)
        let releaseSave = DispatchSemaphore(value: 0)
        let newerStored = DispatchSemaphore(value: 0)
        let first = BlockOperation { events.append("old-checkpoint") }
        sequencer.prepare(first, kind: "checkpoint", instance: "editor")
        let save = BlockOperation {
            events.append("save")
            saveStarted.signal()
            precondition(releaseSave.wait(timeout: .now() + 10) == .success)
        }
        sequencer.prepare(save, kind: "action", instance: "editor")
        precondition(save.dependencies.contains { $0 === first }, "older buffer checkpoints must precede a mutation")
        let newer = BlockOperation { events.append("new-checkpoint"); newerStored.signal() }
        sequencer.prepare(newer, kind: "checkpoint", instance: "editor")
        precondition(newer.dependencies.contains { $0 === first })
        precondition(!newer.dependencies.contains { $0 === save }, "typing checkpoints must not wait for a pending save")
        let completion = BlockOperation { events.append("complete") }
        sequencer.prepare(completion, kind: "complete", instance: "editor")
        precondition(completion.dependencies.contains { $0 === newer }, "complete must not overtake an older queued checkpoint")
        let other = BlockOperation {}
        sequencer.prepare(other, kind: "checkpoint", instance: "other-editor")
        precondition(other.dependencies.isEmpty, "independent editors must remain independent")
        let watch = BlockOperation {}
        sequencer.prepare(watch, kind: "watch", instance: "editor")
        precondition(watch.dependencies.isEmpty, "long reads must not hold the checkpoint lane")
        let queue = OperationQueue()
        queue.maxConcurrentOperationCount = 4
        queue.addOperations([first, save, newer], waitUntilFinished: false)
        precondition(saveStarted.wait(timeout: .now() + 10) == .success)
        precondition(newerStored.wait(timeout: .now() + 10) == .success, "a live pending save must not starve durability")
        releaseSave.signal()
        save.waitUntilFinished()
        queue.addOperations([completion, other, watch], waitUntilFinished: true)
        let recorded = events.snapshot
        precondition(recorded.filter { $0 == "save" }.count == 1, "no mutation may be replayed")
        precondition(recorded.firstIndex(of: "old-checkpoint")! < recorded.firstIndex(of: "save")!)
        precondition(recorded.firstIndex(of: "old-checkpoint")! < recorded.firstIndex(of: "new-checkpoint")!)
        precondition(recorded.firstIndex(of: "new-checkpoint")! < recorded.firstIndex(of: "complete")!)
        let after = BlockOperation {}
        sequencer.prepare(after, kind: "checkpoint", instance: "editor")
        precondition(after.dependencies.isEmpty, "finished operations must not accumulate a dependency history")

        for kind in ["respond", "file-result", "service-result"] {
            let checkpoint = BlockOperation {}
            sequencer.prepare(checkpoint, kind: "checkpoint", instance: kind)
            let mutation = BlockOperation {}
            sequencer.prepare(mutation, kind: kind, instance: kind)
            precondition(mutation.dependencies.contains { $0 === checkpoint })
            let next = BlockOperation {}
            sequencer.prepare(next, kind: "checkpoint", instance: kind)
            precondition(!next.dependencies.contains { $0 === mutation }, "foreground mutation replies must not block newer buffers")
            let close = BlockOperation {}
            sequencer.prepare(close, kind: "close", instance: kind)
            precondition(close.dependencies.contains { $0 === next }, "close must not release ownership ahead of a submitted checkpoint")
        }

        var fence = MobileCheckpointFence()
        let beforeSave = fence.begin(revision: 3)
        precondition(fence.accepts(beforeSave, revision: 3, replyRevision: 3))
        fence.invalidate()
        precondition(!fence.accepts(beforeSave, revision: 3, replyRevision: 3), "a late clean checkpoint must not unlock a pending/unknown action at the same buffer revision")
        let pendingSave = fence.begin(revision: 3)
        fence.invalidate()
        precondition(!fence.accepts(pendingSave, revision: 3, replyRevision: 3), "a pending snapshot must not relock an already completed save")
        let older = fence.begin(revision: 4)
        let latest = fence.begin(revision: 4)
        precondition(!fence.accepts(older, revision: 4, replyRevision: 4))
        precondition(!fence.accepts(latest, revision: 5, replyRevision: 4), "never adopt over a newer local edit")
        precondition(!fence.accepts(latest, revision: 4, replyRevision: nil))
        precondition(!fence.accepts(latest, revision: 4, replyRevision: 3))
        precondition(fence.accepts(latest, revision: 4, replyRevision: 4))

        var deferral = MobileCheckpointDeferral()
        var deferredResults: [Bool] = []
        precondition(!deferral.hold { deferredResults.append($0) }, "pending business saves still allow immediate checkpoints")
        deferral.begin()
        precondition(deferral.hold { deferredResults.append($0) })
        precondition(deferral.hold { deferredResults.append($0) })
        deferral.begin()
        precondition(deferredResults.isEmpty, "hold raw buffers while a completion may canonicalize them or leave may discard them")
        let callbacks = deferral.finish()
        precondition(callbacks.count == 2)
        callbacks.forEach { $0(true) }
        precondition(deferredResults == [true, true], "coalesce deferred requests into one fresh post-completion snapshot")
        precondition(deferral.finish().isEmpty, "never repeat deferred storage or callbacks")
        precondition(!deferral.hold { _ in })
        deferral.begin()
        precondition(deferral.hold { deferredResults.append($0) })
        deferral.finish().forEach { $0(false) }
        precondition(deferredResults == [true, true, false], "successful explicit departure must not resurrect a discarded draft")

        var results: [Bool] = []
        let batch = MobileCheckpointBatch(count: 2) { results.append($0) }
        batch.complete(true)
        precondition(results.isEmpty, "hold background lifetime until every submitted checkpoint replies")
        batch.complete(true)
        batch.complete(false)
        batch.expire()
        precondition(results == [true], "end a completed batch only once")
        let failed = MobileCheckpointBatch(count: 2) { results.append($0) }
        failed.complete(false)
        failed.complete(true)
        precondition(results == [true, false], "one failed or uncertain editor prevents a durable-success claim")
        let expired = MobileCheckpointBatch(count: 2) { results.append($0) }
        expired.complete(true)
        expired.expire()
        expired.expire()
        expired.complete(true)
        precondition(results == [true, false, false], "expiration must finish once without replaying a checkpoint or mutation")

        let normal = MobileRetainedRoute(route: "/items/a", draftKey: "draft-a", uncertain: false)
        let unknown = MobileRetainedRoute(route: "/items/pending", draftKey: "draft-pending", uncertain: true)
        precondition(!MobileRestorationSafety.canOpen(route: normal.route, draftKey: nil, editor: true, retained: [normal]))
        precondition(!MobileRestorationSafety.canOpen(route: "/items/alias", draftKey: "draft-a", editor: true, retained: [normal]), "restore must not steal semantic draft ownership")
        precondition(!MobileRestorationSafety.canOpen(route: "/items/new", draftKey: "new", editor: true, retained: [unknown]), "a retained pending/unknown editor blocks speculative editor restoration")
        precondition(MobileRestorationSafety.canOpen(route: "/items", draftKey: nil, editor: false, retained: [unknown]), "safe read-only roots remain restorable")
        precondition(MobileRestorationSafety.canOpen(route: "/items/new", draftKey: "new", editor: true, retained: [normal]))
        precondition(MobileRestorationSafety.canOpen(route: "/other", draftKey: "", editor: true, retained: [MobileRetainedRoute(route: "/original", draftKey: "", uncertain: false)]))
    }
}
`
