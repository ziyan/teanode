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
    /// The server says the person erased their Health data after the erase
    /// this phone knew of: everything has been forgotten and Health is off.
    case erased
    case stopped
}

/// HealthPassProgress is how far a pass over every type has got, for the
/// settings to show while the first one runs.
struct HealthPassProgress: Equatable {
    var typesDone: Int
    var typesTotal: Int
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

/// HealthWake answers one wake from HealthKit to iOS, once: when the pass that
/// serves it has ended, or after a while, whichever is first. iOS counts a wake
/// as answered when its completion handler is called, and may suspend the app
/// after; a wake answered at once for work that has not been done is answered
/// too early, and one answered only when a long history is done too late.
@MainActor
private final class HealthWake {
    private var finish: (@Sendable () -> Void)?

    init(finish: @escaping @Sendable () -> Void) {
        self.finish = finish
    }

    func done() {
        finish?()
        finish = nil
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
    /// How far the pass over every type has got, while one runs.
    private(set) var passProgress: HealthPassProgress?
    /// Whether Health is off because the server told of an erase the phone
    /// had not heard of. Kept until the person turns Health on again.
    private(set) var isStoppedByErase = false
    /// How many entries the server refused one by one and were left out.
    private(set) var droppedEntryCount = 0

    @ObservationIgnored private let reader = HealthKitReader()
    /// What is remembered for the account in use.
    @ObservationIgnored private var syncState = HealthSyncState()
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
    @ObservationIgnored private var pendingRequest = HealthPassRequest()
    /// The passes under way, which a request made meanwhile is served by.
    @ObservationIgnored private var runningPasses: Task<Void, Never>?
    /// What was last sent of the person's characteristics, to send them again
    /// only when they change.
    @ObservationIgnored private var sentCharacteristics: [HealthCharacteristicInput] = []
    /// How many passes in a row HealthKit would not read a type for a reason
    /// other than the lock. After a few the type is left out of the history,
    /// so that it does not hold the history open for ever.
    @ObservationIgnored private var unreadableAttemptCounts: [String: Int] = [:]
    private let stateDirectory: URL?

    /// A page is up to this many added and deleted objects.
    private static let pageItemCountMost = 1_000
    /// How many routes of workouts are sent in one call. Each is read when it
    /// is about to be sent, so this is also the most routes held at once.
    private static let routeCountPerCall = 3
    /// How long a wake from HealthKit waits for the pass that serves it
    /// before iOS is told it is answered.
    private static let wakeSecondsMost = 25.0
    /// How far back workouts are looked at again for a route that came late.
    private static let recentRouteWindowSeconds = 2.0 * 24 * 3600
    /// How soon the history task may start.
    private static let historyTaskDelaySeconds = 15.0 * 60
    /// How many passes in a row may fail to read a type before it is left out.
    private static let unreadableAttemptCountMost = 3

    private init() {
        isAvailable = HKHealthStore.isHealthDataAvailable()
        let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String
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
        // The account may have been known before this, when nothing could be
        // scheduled yet.
        scheduleHistoryTask()
        guard isAvailable, !accountKeysTurnedOn.isEmpty else { return }
        startObserving()
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

    /// The accounts Health is turned on for, from the files that say so, since
    /// launch must know whether to watch HealthKit before any account is known.
    private var accountKeysTurnedOn: Set<String> {
        stateDirectory.map { HealthSyncState.accountKeysTurnedOn(inDirectory: $0) } ?? []
    }

    /// The account in use changed or was signed out of. What was remembered
    /// belongs to the person who was signed in.
    func accountDidChange(accountKey: String?) {
        guard !Self.isDemo, accountKey != syncState.accountKey else { return }
        stopGeneration += 1
        if let accountKey {
            syncState =
                stateDirectory.map { HealthSyncState.load(accountKey: accountKey, inDirectory: $0) } ?? HealthSyncState(accountKey: accountKey)
        } else {
            syncState = HealthSyncState()
        }
        retryGate = HealthRetryGate()
        isRefused = false
        problemText = nil
        pendingRequest = HealthPassRequest()
        sentCharacteristics = []
        unreadableAttemptCounts = [:]
        passProgress = nil
        isOn = syncState.isTurnedOn
        isHistoryDone = syncState.isHistoryDone
        refreshPublishedValues()
        guard isOn else { return }
        startObserving()
        scheduleHistoryTask()
        if UIApplication.shared.applicationState == .active { requestForegroundPass() }
    }

    /// An account was removed from the phone: what it remembered can never be
    /// used, so its file goes, and so does its choice.
    func accountWasRemoved(accountKey: String) {
        guard !Self.isDemo else { return }
        if let stateDirectory { HealthSyncState.remove(accountKey: accountKey, inDirectory: stateDirectory) }
        if syncState.accountKey == accountKey {
            stopGeneration += 1
            syncState = HealthSyncState()
            isOn = false
            isHistoryDone = false
            passProgress = nil
            refreshPublishedValues()
        }
        if accountKeysTurnedOn.isEmpty { Task { await reader.stopObserving() } }
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
        pendingRequest.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: !isHistoryDone))
        Task { await runPendingPasses() }
    }

    // MARK: What the person chose

    /// Asks HealthKit for read access to everything that can be read, learns
    /// from the server the last time the person erased their Health data, and
    /// starts copying. Apple shows its screen the first time; after that it
    /// shows nothing, and the person changes their mind in the Health app.
    ///
    /// It returns as soon as Apple's screen has been answered and the server
    /// has been asked, so that the setup sheet closes at once. The first pass
    /// over the data runs afterwards, in the background, and the Health
    /// settings show how far it has got (`passProgress`). A failure that stops
    /// Health from being turned on is in `problemText`. A cancelled caller
    /// (the sheet's Cancel) turns nothing on.
    func turnOn() async {
        if Self.isDemo {
            isOn = true
            return
        }
        guard isAvailable else {
            problemText = String(localized: "Health data is not available on this device.")
            return
        }
        guard let accountKey = AppModel.shared.account?.accountKey else {
            problemText = String(localized: "Sign in to turn on Health.")
            return
        }
        if syncState.accountKey != accountKey { accountDidChange(accountKey: accountKey) }
        problemText = nil
        do {
            try await reader.requestAuthorization()
        } catch {
            guard !Task.isCancelled else { return }
            problemText = String(localized: "TeaNode could not ask Apple Health for access. Try again.")
            return
        }
        guard !Task.isCancelled, AppModel.shared.account?.accountKey == accountKey else { return }
        // The last erase of the person's Health on the server: what the phone
        // tells the server with every report, so that an erase it has not
        // heard of is not undone by what it sends.
        guard let connection = AppModel.shared.connection, connection.accountKey == accountKey else { return }
        let healthErasedAt: String?
        do {
            healthErasedAt = try await connection.healthSettings().healthErasedAt
        } catch {
            guard !Task.isCancelled else { return }
            problemText = Self.turnOnProblemText(error)
            return
        }
        guard !Task.isCancelled, AppModel.shared.account?.accountKey == accountKey else { return }
        isRefused = false
        retryGate = HealthRetryGate()
        unreadableAttemptCounts = [:]
        syncState.turnOn(healthErasedAt: healthErasedAt)
        saveState()
        isOn = true
        refreshPublishedValues()
        scheduleHistoryTask()
        startObserving()
        // The first page of every type, then the rest of the history for as
        // long as the app stays open, and later on power.
        pendingRequest.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: true))
        Task { await runPendingPasses() }
    }

    /// Why Health could not be turned on, when the server could not be asked.
    private static func turnOnProblemText(_ error: Error) -> String {
        switch HealthReportFailure.classify(error) {
        case .refused(let reason):
            return String(localized: "Your server will not take Health data: \(reason)")
        case .retryLater, .phoneNotFound, .batchInvalid:
            return String(localized: "Could not reach your server. Try again.")
        }
    }

    /// Stops copying and forgets where it got to, so that turning Health on
    /// again reads everything again (the server ignores what it has). What
    /// the server already holds stays there until the person erases it.
    func turnOff() {
        if Self.isDemo {
            isOn = false
            return
        }
        stopCopying()
        syncState.turnOff()
        saveState()
        refreshPublishedValues()
        if accountKeysTurnedOn.isEmpty { Task { await reader.stopObserving() } }
    }

    /// Stops what is running and asks for nothing more, without touching what
    /// the state remembers.
    private func stopCopying() {
        stopGeneration += 1
        isOn = false
        isHistoryDone = false
        isPassWaitingForUnlock = false
        isRefused = false
        pendingRequest = HealthPassRequest()
        sentCharacteristics = []
        unreadableAttemptCounts = [:]
        passProgress = nil
        problemText = nil
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.historyTaskIdentifier)
    }

    /// The person pressed Sync now: everything, all the way, and a refusal or
    /// a wait after a failure is tried again.
    func syncNow() async {
        guard isOn, !Self.isDemo else { return }
        isRefused = false
        retryGate = HealthRetryGate()
        pendingRequest.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: false))
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
        await runningPasses?.value
        try await connection.eraseHealthData(healthType: nil)
        turnOff()
    }

    // MARK: Waking

    /// Makes the observers now, in the step that called it, so that they
    /// exist before launching is over (iOS delivers an event only to an
    /// observer that exists then); only asking iOS for background deliveries
    /// waits.
    private func startObserving() {
        guard isAvailable else { return }
        let started = reader.startObservers { healthType, finish in
            Task { @MainActor in HealthSync.shared.healthKitReported(healthType: healthType, finish: finish) }
        }
        Task { await reader.enableBackgroundDelivery(started) }
    }

    /// HealthKit has something new of a type. iOS is told the wake is answered
    /// when the pass that reads it has ended, and at the latest after a while.
    private func healthKitReported(healthType: String, finish: @escaping @Sendable () -> Void) {
        let wake = HealthWake(finish: finish)
        guard isOn else {
            wake.done()
            return
        }
        Task {
            try? await Task.sleep(for: .seconds(Self.wakeSecondsMost))
            wake.done()
        }
        pendingRequest.merge(HealthPassRequest(healthTypes: [healthType], isFirstPageOnly: false))
        Task {
            await runPendingPasses()
            wake.done()
        }
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
        // Only once the task is registered, which happens as the app launches.
        guard isLaunched, isOn, !isHistoryDone, !Self.isDemo else { return }
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
        pendingRequest.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: false))
        await runPendingPasses()
        task.setTaskCompleted(success: true)
    }

    // MARK: Passes

    /// Serves what was asked for, one pass after another until nothing is
    /// left. A request that arrives while passes run is served by them, and
    /// its caller waits for them to end.
    private func runPendingPasses() async {
        guard !Self.isDemo else { return }
        if let runningPasses {
            await runningPasses.value
            return
        }
        // Cleared by the passes themselves, in the same step as they end, so
        // that a request made right after is not told to wait for a pass
        // that has already looked for work and found none.
        let passes = Task {
            await servePendingRequests()
            runningPasses = nil
        }
        runningPasses = passes
        await passes.value
    }

    private func servePendingRequests() async {
        isSyncing = true
        isBackgroundTimeEnded = false
        let background = HealthBackgroundTaskHolder(name: "Copy Health")
        defer {
            isSyncing = false
            passProgress = nil
            background.end()
            saveState()
            refreshPublishedValues()
            if isOn, !isHistoryDone { scheduleHistoryTask() }
        }
        while !pendingRequest.isEmpty, !isBackgroundTimeEnded {
            guard isOn, isAvailable, !isRefused, !isErasing, syncState.isTurnedOn else {
                pendingRequest = HealthPassRequest()
                return
            }
            // A person without the permission is refused for every call, and
            // the refusal reads like a missing server.
            if AppModel.shared.hasReadPermissions, !AppModel.shared.canUseHealth {
                pendingRequest = HealthPassRequest()
                problemText = String(
                    localized: "Your account is not allowed to copy or read Apple Health data. Whoever manages your TeaNode server can allow it.")
                return
            }
            guard UIApplication.shared.isProtectedDataAvailable else {
                isPassWaitingForUnlock = true
                return
            }
            guard pendingRequest.isForced || retryGate.canTry(at: Date()) else {
                // The next event, or the next time the app opens, tries again.
                pendingRequest = HealthPassRequest()
                return
            }
            guard let connection = AppModel.shared.connection, connection.accountKey == syncState.accountKey else {
                pendingRequest = HealthPassRequest()
                return
            }
            let request = pendingRequest
            pendingRequest = HealthPassRequest()
            let outcome = await runPass(request, generation: stopGeneration, connection: connection)
            if outcome.isLocked {
                // Asked again when the phone is unlocked.
                pendingRequest.merge(request)
                isPassWaitingForUnlock = true
                return
            }
            if outcome.hasFailed { return }
            if outcome.hasReachedPageLimit, request.isFirstPageOnly, UIApplication.shared.applicationState == .active {
                pendingRequest.merge(HealthPassRequest(isFullPass: true, isForced: true, isFirstPageOnly: false))
            }
        }
    }

    /// Whether a pass that began with this generation, over this connection,
    /// should go on.
    private func isCurrent(_ generation: Int, _ connection: Connection) -> Bool {
        generation == stopGeneration && !isBackgroundTimeEnded && isOn && !isErasing && syncState.accountKey == connection.accountKey
    }

    private static let readableTypeInfos: [HealthTypeInfo] = HealthTypes.all.filter { $0.valueKind != .characteristic }

    private func runPass(_ request: HealthPassRequest, generation: Int, connection: Connection) async -> HealthPassOutcome {
        let calendar = Calendar.current
        var outcome = HealthPassOutcome()

        if request.isFullPass {
            let characteristics = await reader.readCharacteristics()
            if !characteristics.isEmpty, characteristics != sentCharacteristics {
                let built = HealthBatch.requests(characteristics: characteristics)
                switch await send(built, generation: generation, connection: connection) {
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

        let typeInfos = request.isFullPass ? Self.readableTypeInfos : Self.readableTypeInfos.filter { request.healthTypes.contains($0.healthType) }
        var givenUpTypeNames: [String] = []
        for (typeIndex, typeInfo) in typeInfos.enumerated() {
            if request.isFullPass { passProgress = HealthPassProgress(typesDone: typeIndex, typesTotal: typeInfos.count) }
            let typeOutcome = await syncType(typeInfo, request: request, calendar: calendar, generation: generation, connection: connection)
            if typeOutcome == .unreadable {
                unreadableAttemptCounts[typeInfo.healthType, default: 0] += 1
                if unreadableAttemptCounts[typeInfo.healthType, default: 0] >= Self.unreadableAttemptCountMost {
                    // Left out of the history, which would otherwise stay
                    // open for ever; the next pass that reads it fresh tries
                    // again.
                    givenUpTypeNames.append(HealthWords.typeName(healthType: typeInfo.healthType, serverName: typeInfo.typeName))
                    continue
                }
            } else if typeOutcome == .finished || typeOutcome == .reachedPageLimit {
                unreadableAttemptCounts[typeInfo.healthType] = nil
            }
            if absorb(typeOutcome, into: &outcome) { return outcome }
        }
        if request.isFullPass { passProgress = HealthPassProgress(typesDone: typeInfos.count, typesTotal: typeInfos.count) }

        let flushOutcome = await flushPendingDays(calendar: calendar, generation: generation, connection: connection)
        if absorb(flushOutcome, into: &outcome) { return outcome }
        if request.isFullPass || request.healthTypes.contains("workout") || request.healthTypes.contains(HealthKitReader.workoutRouteHealthType) {
            let routeOutcome = await sendRecentRoutes(generation: generation, connection: connection)
            if absorb(routeOutcome, into: &outcome) { return outcome }
        }

        if outcome.hasUnreadableType {
            problemText = String(localized: "Apple Health could not be read just now. TeaNode will try again.")
        } else if !givenUpTypeNames.isEmpty {
            let namesText = givenUpTypeNames.joined(separator: ", ")
            problemText = String(localized: "Apple Health would not let TeaNode read: \(namesText).")
        } else if !isRefused {
            problemText = nil
        }
        // Every type read to its end, with nothing held back: the history is
        // done, even if only the first page of each type was asked for.
        if request.isFullPass, outcome.isComplete {
            isHistoryDone = true
            syncState.markHistoryDone()
            saveState()
            BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.historyTaskIdentifier)
        }
        return outcome
    }

    /// Notes how a part of a pass ended in the pass's outcome. Returns whether
    /// the pass must end there.
    private func absorb(_ typeOutcome: HealthTypeOutcome, into outcome: inout HealthPassOutcome) -> Bool {
        switch typeOutcome {
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
    /// days it touched (and a workout's route, read as it is sent), and moves
    /// the anchor after the server answered.
    private func syncType(
        _ typeInfo: HealthTypeInfo, request: HealthPassRequest, calendar: Calendar, generation: Int, connection: Connection
    ) async -> HealthTypeOutcome {
        var pageCount = 0
        while true {
            guard isCurrent(generation, connection) else { return .stopped }
            let page: HealthKitPage
            do {
                page = try await reader.readPage(
                    typeInfo: typeInfo, anchorData: syncState.anchor(for: typeInfo.healthType), limit: Self.pageItemCountMost)
            } catch {
                return HealthKitReader.isLockedFailure(error) ? .locked : .unreadable
            }
            guard isCurrent(generation, connection) else { return .stopped }
            if page.itemCount == 0 { return .finished }

            // Every day a reading touches needs its summary worked out again,
            // and so do days left over from a send that failed.
            for sample in page.samples {
                syncState.markPending(healthType: typeInfo.healthType, from: sample.startedAt, to: sample.endedAt, calendar: calendar)
            }
            let days = syncState.pendingDays(for: typeInfo.healthType)
            let summaries: [HealthDaySummaryInput]
            // The time the days are worked out, told to the server with each
            // summary so that a late resend cannot overwrite a newer one.
            let workedOutAt = Date()
            do {
                summaries = try await reader.daySummaries(typeInfo: typeInfo, days: days, calendar: calendar).map { $0.workedOut(at: workedOutAt) }
            } catch {
                return HealthKitReader.isLockedFailure(error) ? .locked : .unreadable
            }
            guard isCurrent(generation, connection) else { return .stopped }

            let built = HealthBatch.requests(samples: page.samples, deletedHealthSampleIds: page.deletedHealthSampleIds, daySummaries: summaries)
            switch await send(built, generation: generation, connection: connection) {
            case .sent(let result):
                syncState.clearPending(days.map { HealthPendingDay(healthType: typeInfo.healthType, day: $0) })
                // The server answers with the kinds and times of the samples
                // it deleted, from which the days to work out again follow.
                syncState.markPending(deletedSamples: result.deletedSamples, calendar: calendar)
            case .failed:
                return .failed
            case .erased, .stopped:
                return .stopped
            }

            // The routes of the workouts the server now has, one workout at a
            // time. A route that cannot be read now is found again by the
            // look at recent workouts, so it does not hold the anchor back.
            if typeInfo.valueKind == .workout {
                let routeOutcome = await sendRoutes(
                    ofWorkouts: page.samples.map(\.healthSampleId), generation: generation, connection: connection)
                if routeOutcome != .finished { return routeOutcome }
            }
            syncState.setAnchor(page.newAnchorData, for: typeInfo.healthType)
            saveState()
            refreshPublishedValues()

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
            let healthTypes = Set(syncState.pendingDays.map(\.healthType)).sorted()
            if healthTypes.isEmpty { return .finished }
            for healthType in healthTypes {
                guard isCurrent(generation, connection) else { return .stopped }
                let days = syncState.pendingDays(for: healthType)
                let waiting = days.map { HealthPendingDay(healthType: healthType, day: $0) }
                // A type this app does not know how to read cannot be worked
                // out; its days must not wait for ever.
                guard let typeInfo = HealthTypes.info(for: healthType), typeInfo.valueKind != .characteristic else {
                    syncState.clearPending(waiting)
                    continue
                }
                let summaries: [HealthDaySummaryInput]
                let workedOutAt = Date()
                do {
                    summaries = try await reader.daySummaries(typeInfo: typeInfo, days: days, calendar: calendar).map { $0.workedOut(at: workedOutAt) }
                } catch {
                    return HealthKitReader.isLockedFailure(error) ? .locked : .unreadable
                }
                guard isCurrent(generation, connection) else { return .stopped }
                let built = HealthBatch.requests(daySummaries: summaries)
                switch await send(built, generation: generation, connection: connection) {
                case .sent(let result):
                    syncState.clearPending(waiting)
                    syncState.markPending(deletedSamples: result.deletedSamples, calendar: calendar)
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
        let healthSampleIds: [String]
        do {
            healthSampleIds = try await reader.recentWorkoutHealthSampleIds(since: since)
        } catch {
            return HealthKitReader.isLockedFailure(error) ? .locked : .unreadable
        }
        return await sendRoutes(ofWorkouts: healthSampleIds, generation: generation, connection: connection)
    }

    /// Sends the routes of workouts that have none sent yet. Each route is
    /// read when its turn comes and a few are sent together, so that never
    /// more than a few routes are held at once. A route that is not there (a
    /// workout without one) or cannot be read is left; the look at recent
    /// workouts tries again.
    private func sendRoutes(ofWorkouts healthSampleIds: [String], generation: Int, connection: Connection) async -> HealthTypeOutcome {
        var waiting: [HealthRouteInput] = []
        for healthSampleId in healthSampleIds where !syncState.hasSentRoute(healthSampleId: healthSampleId) {
            guard isCurrent(generation, connection) else { return .stopped }
            let route: HealthRouteInput?
            do {
                route = try await reader.routeInput(forWorkoutHealthSampleId: healthSampleId)
            } catch {
                if HealthKitReader.isLockedFailure(error) { return .locked }
                continue
            }
            guard let route else { continue }
            waiting.append(route)
            if waiting.count >= Self.routeCountPerCall {
                let sendOutcome = await sendRouteInputs(waiting, generation: generation, connection: connection)
                waiting = []
                if sendOutcome != .finished { return sendOutcome }
            }
        }
        if waiting.isEmpty { return .finished }
        return await sendRouteInputs(waiting, generation: generation, connection: connection)
    }

    private func sendRouteInputs(_ routes: [HealthRouteInput], generation: Int, connection: Connection) async -> HealthTypeOutcome {
        let built = HealthBatch.requests(routes: routes)
        switch await send(built, generation: generation, connection: connection) {
        case .sent(let result):
            // A route the server could not match to a workout is sent again
            // later, so it is not noted.
            let unmatchedIds = Set(result.unmatchedRouteHealthSampleIds)
            for route in routes where !unmatchedIds.contains(route.healthSampleId) {
                syncState.recordRouteSent(healthSampleId: route.healthSampleId)
            }
            saveState()
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
    /// one; a failure leaves the caller to keep what it read. Entries left out
    /// because the server would not take them are counted once the whole page
    /// was answered.
    private func send(
        _ built: (requests: [HealthBatch], rejectedEntryCount: Int), generation: Int, connection: Connection
    ) async -> HealthSendResult {
        var answered = ReportHealthResult.nothing
        var droppedEntryCount = built.rejectedEntryCount
        let phoneId = AppModel.shared.phone?.id
        for request in built.requests {
            guard isCurrent(generation, connection) else { return .stopped }
            do {
                // Told with every call, so that the server can tell a phone
                // that has heard of the person's latest erase from one that
                // is still sending what it read before it.
                let healthErasedAt = syncState.healthErasedAt
                let sent = try await HealthBatchSender.sendSplittingInvalid(request) { @Sendable piece in
                    try await Self.sendPiece(piece, over: connection, phoneId: phoneId, healthErasedAt: healthErasedAt)
                }
                // The account was changed while the call was on its way: the
                // answer says nothing about the new account.
                guard isCurrent(generation, connection) else { return .stopped }
                retryGate.didSucceed()
                // An answer that names a later erase than the one the phone
                // sent means the server stored nothing of the batch: Health
                // is turned off, the state is forgotten, and no anchor moves
                // for it.
                guard syncState.accept(sent.result, for: request, at: Date()) else {
                    didLearnOfErase()
                    return .erased
                }
                droppedEntryCount += sent.droppedEntryCount
                answered = answered.merged(with: sent.result)
            } catch {
                guard isCurrent(generation, connection) else { return .stopped }
                noteSendFailure(error)
                return .failed
            }
        }
        syncState.recordDropped(entryCount: droppedEntryCount)
        return .sent(answered)
    }

    /// Sends one batch. A phone the server does not know (the person deleted
    /// it) is left out of the call and the batch sent again, since the phone
    /// only tells the agent which device the data came from.
    nonisolated private static func sendPiece(
        _ piece: HealthBatch, over connection: Connection, phoneId: String?, healthErasedAt: String?
    ) async throws -> ReportHealthResult {
        do {
            return try await connection.reportHealth(piece, agentPhoneId: phoneId, healthErasedAt: healthErasedAt)
        } catch {
            guard phoneId != nil, HealthReportFailure.classify(error) == .phoneNotFound else { throw error }
            return try await connection.reportHealth(piece, agentPhoneId: nil, healthErasedAt: healthErasedAt)
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

    /// A report's answer named an erase on the server that the phone had not
    /// heard of (the state has already forgotten everything and kept the
    /// reason): stop, and let the settings say why.
    private func didLearnOfErase() {
        stopCopying()
        saveState()
        refreshPublishedValues()
        if accountKeysTurnedOn.isEmpty { Task { await reader.stopObserving() } }
    }

    // MARK: The state file

    private func saveState() {
        guard let stateDirectory else { return }
        try? syncState.save(inDirectory: stateDirectory)
    }

    /// Copies what the state knows into what the screens read.
    private func refreshPublishedValues() {
        lastSentAt = syncState.lastSentAt
        sentSampleCount = syncState.sentSampleCount
        pendingDayCount = syncState.pendingDays.count
        isStoppedByErase = syncState.isStoppedByErase
        droppedEntryCount = syncState.droppedEntryCount
    }
}
