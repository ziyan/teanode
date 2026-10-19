import BackgroundTasks
import Foundation
import HealthKit
import Observation
import TeaNodeKit
import UIKit

/// HealthPassRequest is what asked for a pass over HealthKit: every type, or
/// only the ones HealthKit said have something new. Requests that arrive while
/// a pass runs are merged and served by the pass that follows.
private struct HealthPassRequest {
    /// Read every type, not only the ones named.
    var isFullPass = false
    var healthTypes: Set<String> = []
    /// Skip the wait that follows a failure.
    var isForced = false
    /// Read one page of each type, so that the first data shows quickly; the
    /// rest of a long history follows in a later pass.
    var isFirstPageOnly = true

    var isEmpty: Bool { !isFullPass && healthTypes.isEmpty }

    mutating func merge(_ other: HealthPassRequest) {
        guard !other.isEmpty else { return }
        isFirstPageOnly = isEmpty ? other.isFirstPageOnly : (isFirstPageOnly && other.isFirstPageOnly)
        isFullPass = isFullPass || other.isFullPass
        healthTypes.formUnion(other.healthTypes)
        isForced = isForced || other.isForced
    }
}

/// HealthPassOutcome is how a pass ended, for deciding what to do next.
private struct HealthPassOutcome {
    /// HealthKit said the phone is locked.
    var isLocked = false
    var hasFailed = false
    var hasStopped = false
    var hasReachedPageLimit = false
    var hasUnreadableType = false

    /// Whether every type was read to its end.
    var isComplete: Bool { !isLocked && !hasFailed && !hasStopped && !hasReachedPageLimit && !hasUnreadableType }
}

/// HealthTypeOutcome is how reading one type ended.
private enum HealthTypeOutcome {
    case finished
    case reachedPageLimit
    case locked
    /// HealthKit would not read it, for a reason other than the lock.
    case unreadable
    case failed
    case stopped
}

/// HealthSendResult is how sending a page's requests ended.
private enum HealthSendResult {
    /// The server took them all; carries what it answered, added up.
    case sent(ReportHealthResult)
    case failed
    /// The server says the person erased their Health data after Health was
    /// turned on here: everything has been forgotten.
    case erased
    case stopped
}

/// HealthBackgroundTaskHolder keeps the app running for the few seconds iOS
/// gives it after the person leaves, so that a pass can finish its page.
@MainActor
private final class HealthBackgroundTaskHolder {
    private var identifier = UIBackgroundTaskIdentifier.invalid

    init(name: String) {
        identifier = UIApplication.shared.beginBackgroundTask(withName: name) { [weak self] in
            // The time is up: the pass stops after the page it is on and
            // leaves its place in the anchors.
            HealthSync.shared.backgroundTimeEnded()
            self?.end()
        }
    }

    func end() {
        guard identifier != .invalid else { return }
        UIApplication.shared.endBackgroundTask(identifier)
        identifier = .invalid
    }
}

/// HealthSync copies the person's Apple Health data to their own server, if
/// they turn it on.
///
/// For each type it reads HealthKit from a saved anchor (an
/// HKAnchoredObjectQuery), a page of up to 1,000 at a time, works out the
/// summary of every day the page touched, and sends both. The anchor moves
/// only after the server answered, so a failed send is read again, and the
/// server takes a batch sent twice as the same samples. The phone keeps no
/// copy of any health data: only the anchors, the days waiting for a new
/// summary and the counts (HealthSyncState).
///
/// iOS wakes the app about new data through an observer for each type, made
/// as the app launches. Nothing is read while the phone is locked, since
/// HealthKit's data is encrypted then; a pass asked for during that waits for
/// the unlock.
@MainActor
@Observable
final class HealthSync {
    static let shared = HealthSync()

    /// The person's choice, for the account in use.
    private(set) var isOn = false
    /// Whether this device has Health at all (an iPad may not).
    private(set) var isAvailable: Bool
    private(set) var isSyncing = false
    private(set) var lastSentAt: Date?
    /// Samples sent since Health was turned on.
    private(set) var sentSampleCount = 0
    /// Days waiting for a new summary.
    private(set) var pendingDayCount = 0
    /// Whether the first full read of everything has finished.
    private(set) var isHistoryDone = false
    /// A plain sentence about why nothing is being copied, or nil.
    private(set) var problemText: String?

    @ObservationIgnored private let reader = HealthKitReader()
    /// What is remembered for the account in use.
    @ObservationIgnored private var state = HealthSyncState()
    @ObservationIgnored private var retryGate = HealthRetryGate()
    @ObservationIgnored private var isLaunched = false
    /// Set when the server said it will not take Health data from this person;
    /// asking again cannot change that, so passes stop until the person turns
    /// Health off and on, or presses Sync now.
    @ObservationIgnored private var isRefused = false
    /// Set while everything is being erased, so that nothing is sent that the
    /// erase would then have to take back.
    @ObservationIgnored private var isErasing = false
    /// Raised when a running pass must give up (Health turned off, account
    /// changed, erased); a pass remembers the number it began with.
    @ObservationIgnored private var stopGeneration = 0
    /// Set when iOS says the time the app was given is up.
    @ObservationIgnored private var isBackgroundTimeEnded = false
    /// Set when a pass was asked for while the phone was locked.
    @ObservationIgnored private var isPassWaitingForUnlock = false
    @ObservationIgnored private var pending = HealthPassRequest()
    /// The workouts whose route has been sent, so that the sweep of recent
    /// workouts does not send it again.
    @ObservationIgnored private var sentRouteHealthSampleIds: Set<String> = []
    /// What was last sent of the person's characteristics, to send them again
    /// only when they change.
    @ObservationIgnored private var sentCharacteristics: [HealthCharacteristicInput] = []
    private let defaults: UserDefaults
    private let stateDirectory: URL?

    /// The accounts the person turned Health on for, kept apart from the
    /// per-account state so that launch knows whether to start watching
    /// before any account is known.
    private static let accountKeysOnKey = "healthAccountKeysOn"
    /// A page is up to this many added and deleted objects.
    private static let pageItemCountMost = 1_000
    /// How far back workouts are looked at again for a route that came late.
    private static let recentRouteWindowSeconds = 2.0 * 24 * 3600
    /// How soon the history task may start.
    private static let historyTaskDelaySeconds = 15.0 * 60

    private init() {
        isAvailable = HKHealthStore.isHealthDataAvailable()
        let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String
        defaults = group.flatMap { UserDefaults(suiteName: $0) } ?? .standard
        let fileManager = FileManager.default
        let container =
            group.flatMap { fileManager.containerURL(forSecurityApplicationGroupIdentifier: $0) }
            ?? fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
        stateDirectory = container?.appendingPathComponent("Health", isDirectory: true)
        // Which account is in use is told by the model while it is made
        // (accountDidChange), which reads that account's state then.
    }

    /// The screenshots run without asking anything of the system.
    private static var isDemo: Bool {
        #if DEBUG
            return DemoServer.isRequested
        #else
            return false
        #endif
    }

    /// The identifier of the task that goes on with the history on power. It is
    /// listed in Info.plist as `$(TEANODE_BUNDLE_IDENTIFIER).health-history`
    /// (project.yml), so a build under another identifier names the same one.
    static var historyTaskIdentifier: String { (Bundle.main.bundleIdentifier ?? "com.teanode.app") + ".health-history" }

    // MARK: Launch

    /// Called as the app finishes launching, in the foreground or relaunched by
    /// iOS for new data: registers the history task, which has to be done
    /// before launching is over, and starts the observers, since iOS delivers
    /// an event only to an observer that exists then.
    func launch() {
        guard !isLaunched else { return }
        isLaunched = true
        if Self.isDemo {
            showDemoValues()
            return
        }
        Self.registerHistoryTask()
        NotificationCenter.default.addObserver(forName: UIApplication.protectedDataDidBecomeAvailableNotification, object: nil, queue: .main) { _ in
            Task { @MainActor in HealthSync.shared.protectedDataDidBecomeAvailable() }
        }
        guard isAvailable, !accountKeysOn.isEmpty else { return }
        Task { await startObserving() }
    }

    /// The numbers the screenshots show.
    private func showDemoValues() {
        isOn = true
        isAvailable = true
        isHistoryDone = true
        lastSentAt = Date(timeIntervalSinceNow: -600)
        sentSampleCount = 48_213
        pendingDayCount = 0
    }

    private var accountKeysOn: Set<String> { Set(defaults.stringArray(forKey: Self.accountKeysOnKey) ?? []) }

    private func historyDoneKey(_ accountKey: String) -> String { "healthIsHistoryDone.\(accountKey)" }

    /// The account in use changed or was signed out of. What was remembered
    /// belongs to the person who was signed in.
    func accountDidChange(accountKey: String?) {
        guard !Self.isDemo, accountKey != state.accountKey else { return }
        stopGeneration += 1
        if let accountKey {
            state = stateDirectory.map { HealthSyncState.load(accountKey: accountKey, inDirectory: $0) } ?? HealthSyncState(accountKey: accountKey)
        } else {
            state = HealthSyncState()
        }
        retryGate = HealthRetryGate()
        isRefused = false
        problemText = nil
        pending = HealthPassRequest()
        sentRouteHealthSampleIds = []
        sentCharacteristics = []
        if let accountKey {
            isOn = accountKeysOn.contains(accountKey)
            isHistoryDone = defaults.bool(forKey: historyDoneKey(accountKey))
            // The file was lost but the person's choice was not: start again.
            if isOn, !state.isTurnedOn { state.turnOn(at: Date()) }
        } else {
            isOn = false
            isHistoryDone = false
        }
        refreshPublishedValues()
        guard isOn else { return }
        Task { await startObserving() }
        scheduleHistoryTask()
        if UIApplication.shared.applicationState == .active { requestForegroundPass() }
    }

    /// An account was removed from the phone: what it remembered can never be
    /// used, so its file goes, and so does its choice.
    func accountWasRemoved(accountKey: String) {
        guard !Self.isDemo else { return }
        if let stateDirectory {
            try? FileManager.default.removeItem(at: stateDirectory.appendingPathComponent(HealthSyncState.fileName(accountKey: accountKey)))
        }
        var keys = accountKeysOn
        keys.remove(accountKey)
        defaults.set(keys.sorted(), forKey: Self.accountKeysOnKey)
        defaults.removeObject(forKey: historyDoneKey(accountKey))
        if state.accountKey == accountKey {
            stopGeneration += 1
            state = HealthSyncState()
            isOn = false
            isHistoryDone = false
            refreshPublishedValues()
        }
        if keys.isEmpty { Task { await reader.stopObserving() } }
    }

    /// The app came to the foreground, or left it.
    func sceneDidChange(isActive: Bool, isBackground: Bool) {
        guard isOn, isAvailable, !Self.isDemo else { return }
        if isActive {
            requestForegroundPass()
        } else if isBackground {
            scheduleHistoryTask()
        }
    }

    /// While the history is unfinished, a pass in the foreground reads one page
    /// of each type, so that the first data arrives quickly; the pass then goes
    /// on with the rest for as long as the app stays open.
    private func requestForegroundPass() {
        pending.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: !isHistoryDone))
        Task { await runPendingPasses() }
    }

    // MARK: What the person chose

    /// Asks HealthKit for read access to everything that can be read, then
    /// starts copying. Apple shows the sheet the first time; after that it
    /// shows nothing, and the person changes their mind in the Health app.
    func turnOn() async {
        if Self.isDemo {
            isOn = true
            return
        }
        guard isAvailable else {
            problemText = String(localized: "Health data is not available on this iPhone.")
            return
        }
        guard let accountKey = AppModel.shared.account?.accountKey else { return }
        if state.accountKey != accountKey { accountDidChange(accountKey: accountKey) }
        do {
            try await reader.requestAuthorization()
        } catch {
            problemText = String(localized: "TeaNode could not ask Apple Health for access. Try again.")
            return
        }
        guard AppModel.shared.account?.accountKey == accountKey else { return }
        var keys = accountKeysOn
        keys.insert(accountKey)
        defaults.set(keys.sorted(), forKey: Self.accountKeysOnKey)
        isOn = true
        isRefused = false
        retryGate = HealthRetryGate()
        problemText = nil
        state.turnOn(at: Date())
        saveState()
        refreshPublishedValues()
        await startObserving()
        scheduleHistoryTask()
        await syncNow()
    }

    /// Stops copying and forgets where it got to, so that turning Health on
    /// again reads everything again (the server ignores what it has). What
    /// the server already holds stays there until the person erases it.
    func turnOff() {
        if Self.isDemo {
            isOn = false
            return
        }
        stopGeneration += 1
        if let accountKey = state.accountKey {
            var keys = accountKeysOn
            keys.remove(accountKey)
            defaults.set(keys.sorted(), forKey: Self.accountKeysOnKey)
            defaults.removeObject(forKey: historyDoneKey(accountKey))
            if keys.isEmpty { Task { await reader.stopObserving() } }
        }
        isOn = false
        isHistoryDone = false
        isPassWaitingForUnlock = false
        isRefused = false
        pending = HealthPassRequest()
        sentRouteHealthSampleIds = []
        sentCharacteristics = []
        problemText = nil
        state.turnOff()
        saveState()
        refreshPublishedValues()
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.historyTaskIdentifier)
    }

    /// The person pressed Sync now: everything, all the way, and a refusal or
    /// a wait after a failure is tried again.
    func syncNow() async {
        guard isOn, !Self.isDemo else { return }
        isRefused = false
        retryGate = HealthRetryGate()
        pending.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: false))
        await runPendingPasses()
    }

    /// Erases everything Health holds for the person on the server, then stops
    /// copying. Nothing is sent between the person pressing Erase and the
    /// server having erased.
    func eraseEverything() async throws {
        if Self.isDemo {
            turnOff()
            return
        }
        guard let connection = AppModel.shared.connection else { throw TeaNodeError.signedOut }
        isErasing = true
        stopGeneration += 1
        defer { isErasing = false }
        while isSyncing { try? await Task.sleep(for: .milliseconds(100)) }
        try await connection.eraseHealthData(healthType: nil)
        turnOff()
    }

    // MARK: Waking

    private func startObserving() async {
        guard isAvailable else { return }
        await reader.startObserving { healthType, finish in
            Task { @MainActor in
                await HealthSync.shared.healthKitReported(healthType: healthType)
                // Always, or iOS counts the wake as unanswered and wakes the
                // app less often.
                finish()
            }
        }
    }

    /// HealthKit has something new of a type.
    private func healthKitReported(healthType: String) async {
        guard isOn else { return }
        pending.merge(HealthPassRequest(healthTypes: [healthType], isFirstPageOnly: false))
        await runPendingPasses()
    }

    private func protectedDataDidBecomeAvailable() {
        guard isPassWaitingForUnlock, isOn else { return }
        isPassWaitingForUnlock = false
        Task { await runPendingPasses() }
    }

    func backgroundTimeEnded() {
        isBackgroundTimeEnded = true
    }

    // MARK: The history task

    private static func registerHistoryTask() {
        // On the main queue, where the handler's main actor isolation holds:
        // with no queue the system calls it on one of its own.
        BGTaskScheduler.shared.register(forTaskWithIdentifier: historyTaskIdentifier, using: .main) { task in
            nonisolated(unsafe) let task = task
            Task { @MainActor in
                guard let task = task as? BGProcessingTask else { return task.setTaskCompleted(success: false) }
                await shared.runHistoryTask(task)
            }
        }
    }

    /// Asks iOS for a pass on power and a network, which goes on with a long
    /// history. Nothing can be read while the phone is locked, so a pass that
    /// starts then does nothing and asks again; the history mostly moves while
    /// the app is open.
    private func scheduleHistoryTask() {
        guard isOn, !isHistoryDone, !Self.isDemo else { return }
        let request = BGProcessingTaskRequest(identifier: Self.historyTaskIdentifier)
        request.requiresNetworkConnectivity = true
        request.requiresExternalPower = true
        request.earliestBeginDate = Date(timeIntervalSinceNow: Self.historyTaskDelaySeconds)
        try? BGTaskScheduler.shared.submit(request)
    }

    private func runHistoryTask(_ task: BGProcessingTask) async {
        guard isOn, !isHistoryDone else {
            task.setTaskCompleted(success: true)
            return
        }
        task.expirationHandler = {
            Task { @MainActor in HealthSync.shared.backgroundTimeEnded() }
        }
        scheduleHistoryTask()
        pending.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: false))
        await runPendingPasses()
        task.setTaskCompleted(success: true)
    }

    // MARK: Passes

    /// Serves what was asked for, one pass after another until nothing is
    /// left. A request that arrives while a pass runs is served by the loop;
    /// the second caller returns at once.
    private func runPendingPasses() async {
        guard !isSyncing, !Self.isDemo else { return }
        isSyncing = true
        isBackgroundTimeEnded = false
        let background = HealthBackgroundTaskHolder(name: "Copy Health")
        defer {
            isSyncing = false
            background.end()
            saveState()
            refreshPublishedValues()
            if isOn, !isHistoryDone { scheduleHistoryTask() }
        }
        while !pending.isEmpty, !isBackgroundTimeEnded {
            guard isOn, isAvailable, !isRefused, !isErasing, state.isTurnedOn else {
                pending = HealthPassRequest()
                return
            }
            guard UIApplication.shared.isProtectedDataAvailable else {
                isPassWaitingForUnlock = true
                return
            }
            guard pending.isForced || retryGate.canTry(at: Date()) else {
                // The next event, or the next time the app opens, tries again.
                pending = HealthPassRequest()
                return
            }
            guard let connection = AppModel.shared.connection, connection.accountKey == state.accountKey else {
                pending = HealthPassRequest()
                return
            }
            let request = pending
            pending = HealthPassRequest()
            let outcome = await runPass(request, generation: stopGeneration, connection: connection)
            if outcome.isLocked {
                // Asked again when the phone is unlocked.
                pending.merge(request)
                isPassWaitingForUnlock = true
                return
            }
            if outcome.hasFailed { return }
            if outcome.hasReachedPageLimit, request.isFirstPageOnly, UIApplication.shared.applicationState == .active {
                pending.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: false))
            }
        }
    }

    /// Whether a pass that began with this generation, over this connection,
    /// should go on.
    private func isCurrent(_ generation: Int, _ connection: Connection) -> Bool {
        generation == stopGeneration && !isBackgroundTimeEnded && isOn && !isErasing && state.accountKey == connection.accountKey
    }

    private static let readableInfos: [HealthTypeInfo] = HealthTypes.all.filter { $0.valueKind != .characteristic }

    private func runPass(_ request: HealthPassRequest, generation: Int, connection: Connection) async -> HealthPassOutcome {
        let calendar = Calendar.current
        var outcome = HealthPassOutcome()

        if request.isFullPass {
            let characteristics = await reader.readCharacteristics()
            if !characteristics.isEmpty, characteristics != sentCharacteristics {
                let built = HealthBatch.requests(characteristics: characteristics)
                switch await send(built.requests, generation: generation, connection: connection) {
                case .sent: sentCharacteristics = characteristics
                case .failed:
                    outcome.hasFailed = true
                    return outcome
                case .erased, .stopped:
                    outcome.hasStopped = true
                    return outcome
                }
            }
        }

        let infos = request.isFullPass ? Self.readableInfos : Self.readableInfos.filter { request.healthTypes.contains($0.healthType) }
        for info in infos {
            let end = await syncType(info, request: request, calendar: calendar, generation: generation, connection: connection)
            if absorb(end, into: &outcome) { return outcome }
        }

        let flushEnd = await flushPendingDays(calendar: calendar, generation: generation, connection: connection)
        if absorb(flushEnd, into: &outcome) { return outcome }
        if request.isFullPass || request.healthTypes.contains("workout") || request.healthTypes.contains(HealthKitReader.workoutRouteHealthType) {
            let routeEnd = await sendRecentRoutes(generation: generation, connection: connection)
            if absorb(routeEnd, into: &outcome) { return outcome }
        }

        if outcome.hasUnreadableType {
            problemText = String(localized: "Apple Health could not be read just now. TeaNode will try again.")
        } else if !isRefused {
            problemText = nil
        }
        // Every type read to its end, with nothing held back: the history is
        // done.
        if request.isFullPass, !request.isFirstPageOnly, outcome.isComplete, let accountKey = state.accountKey {
            isHistoryDone = true
            defaults.set(true, forKey: historyDoneKey(accountKey))
            BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.historyTaskIdentifier)
        }
        return outcome
    }

    /// Notes how a part of a pass ended in the pass's outcome. Returns whether
    /// the pass must end there.
    private func absorb(_ end: HealthTypeOutcome, into outcome: inout HealthPassOutcome) -> Bool {
        switch end {
        case .finished:
            return false
        case .reachedPageLimit:
            outcome.hasReachedPageLimit = true
            return false
        case .unreadable:
            outcome.hasUnreadableType = true
            return false
        case .locked:
            outcome.isLocked = true
            return true
        case .failed:
            outcome.hasFailed = true
            return true
        case .stopped:
            outcome.hasStopped = true
            return true
        }
    }

    /// Reads a type page by page, sending each page with the summaries of the
    /// days it touched, and moves the anchor after the server answered.
    private func syncType(
        _ info: HealthTypeInfo, request: HealthPassRequest, calendar: Calendar, generation: Int, connection: Connection
    ) async -> HealthTypeOutcome {
        var pageCount = 0
        while true {
            guard isCurrent(generation, connection) else { return .stopped }
            let page: HealthKitPage
            do {
                page = try await reader.readPage(info: info, anchorData: state.anchor(for: info.healthType), limit: Self.pageItemCountMost)
            } catch {
                return HealthKitReader.isLockedFailure(error) ? .locked : .unreadable
            }
            guard isCurrent(generation, connection) else { return .stopped }
            if page.itemCount == 0 { return .finished }

            // Every day a reading touches needs its summary worked out again,
            // and so do days left over from a send that failed.
            for sample in page.samples {
                state.markPending(healthType: info.healthType, from: sample.startedAt, to: sample.endedAt, calendar: calendar)
            }
            let days = state.pendingDays(for: info.healthType)
            let summaries: [HealthDaySummaryInput]
            do {
                summaries = try await reader.daySummaries(info: info, days: days, calendar: calendar)
            } catch {
                return HealthKitReader.isLockedFailure(error) ? .locked : .unreadable
            }
            guard isCurrent(generation, connection) else { return .stopped }

            let built = HealthBatch.requests(
                samples: page.samples, deletedHealthSampleIds: page.deletedHealthSampleIds, daySummaries: summaries, routes: page.routes)
            switch await send(built.requests, generation: generation, connection: connection) {
            case .sent(let result):
                state.clearPending(days.map { HealthPendingDay(healthType: info.healthType, day: $0) })
                // The server answers with the kinds and times of the samples
                // it deleted, from which the days to work out again follow.
                state.markPending(deletedSamples: result.deletedSamples, calendar: calendar)
                state.setAnchor(page.newAnchorData, for: info.healthType)
                for route in page.routes { sentRouteHealthSampleIds.insert(route.healthSampleId) }
                saveState()
                refreshPublishedValues()
            case .failed:
                return .failed
            case .erased, .stopped:
                return .stopped
            }

            pageCount += 1
            if page.itemCount < Self.pageItemCountMost { return .finished }
            if request.isFirstPageOnly, pageCount >= 1 { return .reachedPageLimit }
        }
    }

    /// Works out the days that still wait, of any type: those the server's
    /// answers named after deleting samples, and any left from earlier.
    private func flushPendingDays(calendar: Calendar, generation: Int, connection: Connection) async -> HealthTypeOutcome {
        // A summary sent can bring a deletion that names more days; a few
        // rounds settle it, and anything left waits for the next pass.
        for _ in 0..<3 {
            let healthTypes = Set(state.pendingDays.map(\.healthType)).sorted()
            if healthTypes.isEmpty { return .finished }
            for healthType in healthTypes {
                guard isCurrent(generation, connection) else { return .stopped }
                let days = state.pendingDays(for: healthType)
                let waiting = days.map { HealthPendingDay(healthType: healthType, day: $0) }
                // A type this app does not know how to read cannot be worked
                // out; its days must not wait for ever.
                guard let info = HealthTypes.info(for: healthType), info.valueKind != .characteristic else {
                    state.clearPending(waiting)
                    continue
                }
                let summaries: [HealthDaySummaryInput]
                do {
                    summaries = try await reader.daySummaries(info: info, days: days, calendar: calendar)
                } catch {
                    return HealthKitReader.isLockedFailure(error) ? .locked : .unreadable
                }
                guard isCurrent(generation, connection) else { return .stopped }
                let built = HealthBatch.requests(daySummaries: summaries)
                switch await send(built.requests, generation: generation, connection: connection) {
                case .sent(let result):
                    state.clearPending(waiting)
                    state.markPending(deletedSamples: result.deletedSamples, calendar: calendar)
                    saveState()
                    refreshPublishedValues()
                case .failed:
                    return .failed
                case .erased, .stopped:
                    return .stopped
                }
            }
        }
        return .finished
    }

    /// Looks again at recent workouts for a route that was saved after the
    /// workout was read.
    private func sendRecentRoutes(generation: Int, connection: Connection) async -> HealthTypeOutcome {
        let since = Date(timeIntervalSinceNow: -Self.recentRouteWindowSeconds)
        let routes: [HealthRouteInput]
        do {
            routes = try await reader.recentWorkoutRoutes(since: since, excluding: sentRouteHealthSampleIds)
        } catch {
            return HealthKitReader.isLockedFailure(error) ? .locked : .unreadable
        }
        guard !routes.isEmpty, isCurrent(generation, connection) else { return .finished }
        let built = HealthBatch.requests(routes: routes)
        switch await send(built.requests, generation: generation, connection: connection) {
        case .sent:
            for route in routes { sentRouteHealthSampleIds.insert(route.healthSampleId) }
            refreshPublishedValues()
            return .finished
        case .failed:
            return .failed
        case .erased, .stopped:
            return .stopped
        }
    }

    // MARK: Sending

    /// Sends requests in order. They are taken only if the server answered each
    /// one; a failure leaves the caller to keep what it read.
    private func send(_ requests: [HealthBatch], generation: Int, connection: Connection) async -> HealthSendResult {
        var answered = ReportHealthResult.nothing
        let phoneId = AppModel.shared.phone?.id
        for request in requests {
            guard isCurrent(generation, connection) else { return .stopped }
            do {
                let sent = try await HealthBatchSender.sendSplittingInvalid(request) { @Sendable piece in
                    try await Self.sendPiece(piece, over: connection, phoneId: phoneId)
                }
                // The account was changed while the call was on its way: the
                // answer says nothing about the new account.
                guard isCurrent(generation, connection) else { return .stopped }
                retryGate.didSucceed()
                state.recordSent(request, result: sent.result, at: Date())
                answered = answered.merged(with: sent.result)
                if state.applyServerErase(healthErasedAt: sent.result.healthErasedDate) {
                    didLearnOfErase()
                    return .erased
                }
            } catch {
                guard isCurrent(generation, connection) else { return .stopped }
                noteSendFailure(error)
                return .failed
            }
        }
        return .sent(answered)
    }

    /// Sends one batch. A phone the server does not know (the person deleted
    /// it) is left out of the call and the batch sent again, since the phone
    /// only tells the agent which device the data came from.
    nonisolated private static func sendPiece(_ piece: HealthBatch, over connection: Connection, phoneId: String?) async throws -> ReportHealthResult {
        do {
            return try await connection.reportHealth(piece, agentPhoneId: phoneId)
        } catch {
            guard phoneId != nil, HealthReportFailure.classify(error) == .phoneNotFound else { throw error }
            return try await connection.reportHealth(piece, agentPhoneId: nil)
        }
    }

    private func noteSendFailure(_ error: Error) {
        switch HealthReportFailure.classify(error) {
        case .refused(let reason):
            isRefused = true
            problemText = String(localized: "Your server will not take Health data: \(reason)")
        case .retryLater, .phoneNotFound, .batchInvalid:
            retryGate.didFail(at: Date())
            problemText = String(localized: "Could not reach your server. TeaNode will send Health data when it can.")
        }
    }

    /// A report's answer said the person erased their Health data on the
    /// server after turning it on here (the state has already forgotten
    /// everything): stop, and say why.
    private func didLearnOfErase() {
        turnOff()
        problemText = String(localized: "Health data was erased on your server, so TeaNode stopped copying it. Turn Health on again to copy it again.")
    }

    // MARK: The state file

    private func saveState() {
        guard let stateDirectory else { return }
        try? state.save(inDirectory: stateDirectory)
    }

    /// Copies what the state knows into what the screens read.
    private func refreshPublishedValues() {
        lastSentAt = state.lastSentAt
        sentSampleCount = state.sentSampleCount
        pendingDayCount = state.pendingDays.count
    }
}
