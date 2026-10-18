import BackgroundTasks
import Foundation
import Network
import Observation
import TeaNodeKit
import UIKit

/// PhotoBackupEngine backs up this iPhone's photo library to the server. The
/// screens read its `status` and turn it on and off; the work is done by a
/// PhotoBackupCoordinator (TeaNodeKit), which reads the library, queues the
/// files with the background URLSession (PhotoBackupSession) and records what
/// went up. This class decides when a pass runs: when the app comes to the
/// foreground, when the network comes back while files wait for it, when the
/// person taps Back up now (a continued processing task, so that it goes on with
/// the app in the background), and overnight on power (a processing task).
///
/// The background session is handed a long queue (PhotoBackupBudget.standard),
/// which iOS goes on sending after the person has left the app. When iOS wakes
/// the app because some of it finished, the app queues more files that are on
/// the phone already in the few seconds it has (`topUpAfterWake`); a whole pass,
/// which lists the library and may wait on iCloud, runs only while the app is
/// in the foreground or a background task is running.
@MainActor
@Observable
final class PhotoBackupEngine {
    static let shared = PhotoBackupEngine()

    /// The app's bundle identifier, which the task identifiers and the queue
    /// names start with. The identifiers are listed in Info.plist as
    /// `$(TEANODE_BUNDLE_IDENTIFIER).photo-backup` and `.photo-backup-now`
    /// (project.yml), so a build under another identifier names the same ones.
    nonisolated static var identifierPrefix: String { Bundle.main.bundleIdentifier ?? "com.teanode.app" }
    /// A pass overnight, on power and a network.
    static let processingIdentifier = identifierPrefix + ".photo-backup"
    /// A pass the person asked for, which the system shows in a Live Activity
    /// and lets run with the app in the background.
    static let continuedIdentifier = identifierPrefix + ".photo-backup-now"

    // MARK: What the screens read

    private(set) var status = PhotoBackupStatus()
    /// The name of the device password backup signs in with.
    private(set) var credentialName: String?
    /// How much of the library the app may read, when that is not enough.
    private(set) var accessProblem: PhotoBackupAccess?
    private(set) var isWorking = false
    /// Whether the person paused backup, which lasts until they resume, across
    /// launches (PhotoBackupPreferences.isPaused). Not the same as off: the
    /// password, the index and the place the listing got to are kept.
    private(set) var isPaused: Bool
    /// How many times backup has finished something (a pass ended, or the app
    /// queued more files after being woken). The Photo backup screen reads the
    /// size of the library from the server when this changes, and not otherwise.
    private(set) var finishedWorkCount = 0
    var problemText: String?
    var doneText: String?

    var usesMobileData: Bool {
        didSet {
            preferences.usesMobileData = usesMobileData
            // Each queued upload was made with the old setting (a request says
            // whether it may use mobile data), so they are cancelled and a new
            // pass queues them again under the new one.
            guard usesMobileData != oldValue else { return }
            Task { await self.restartPasses() }
        }
    }
    var includesHidden: Bool {
        didSet {
            preferences.includesHidden = includesHidden
            // Hidden photos already in the library are not "changes", so the
            // next pass lists the library again (skipping what is backed up).
            Task { try? await self.makeIndex().setChangeToken(nil) }
        }
    }

    var isEnabled: Bool { enabledAccountKey != nil }
    private(set) var enabledAccountKey: String?

    /// The account backup is on for, as the person knows it, such as
    /// "robin on photos.example.com"; nil when backup is off.
    var enabledAccountName: String? {
        enabledAccountKey.map(Self.name(ofAccountKey:))
    }

    /// An account key is the server, a bar and the lowercase username.
    nonisolated static func name(ofAccountKey accountKey: String) -> String {
        let parts = accountKey.split(separator: "|", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return accountKey }
        return String(localized: "\(parts[1]) on \(parts[0])")
    }

    // MARK: What it keeps

    @ObservationIgnored private let preferences = PhotoBackupPreferences()
    @ObservationIgnored private let credentials = PhotoBackupCredentialStore(secrets: KeychainSecrets())
    @ObservationIgnored private var index: PhotoBackupIndex?
    @ObservationIgnored private var coordinator: PhotoBackupCoordinator?
    @ObservationIgnored private var coordinatorAccountKey: String?
    @ObservationIgnored private var passTask: Task<Void, Never>?
    /// Waits for the time the server asked for before the files are sent again.
    @ObservationIgnored private var retryTask: Task<Void, Never>?
    /// Counts the passes started, so that a pass that ends late does not clear
    /// the task of the one that replaced it.
    @ObservationIgnored private var passGeneration = 0
    /// Set when an upload finished while the pass task was still winding up
    /// (it had stopped queueing, and had not yet been cleared), so that the
    /// task runs once more instead of the finished upload being lost.
    @ObservationIgnored private var isAnotherPassWanted = false
    /// True while backup is being turned off: no pass starts and no finished
    /// upload is recorded, including the cancelled ones the turning off causes.
    @ObservationIgnored private var isTurningOff = false
    /// Told of every status while a continued processing task runs, to report
    /// progress to the system.
    @ObservationIgnored private var progressSink: (@MainActor (PhotoBackupStatus) -> Void)?
    /// Watches the network while the app runs, to start a pass again when it
    /// comes back and files are waiting for it.
    @ObservationIgnored private var networkMonitor: NWPathMonitor?
    @ObservationIgnored private var lastNetworkState: PhotoBackupNetworkState?

    private init() {
        usesMobileData = preferences.usesMobileData
        includesHidden = preferences.includesHidden
        enabledAccountKey = preferences.enabledAccountKey
        isPaused = preferences.isPaused
        // The index is written every thirty seconds at most; going to the
        // background writes what is left, as the app may be suspended at once.
        NotificationCenter.default.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { PhotoBackupEngine.shared.sceneWentToBackground() }
        }
    }

    // MARK: Where the files are

    /// The backup's folder in the app group, which the index and the files
    /// being sent live in.
    nonisolated static func directory() throws -> URL {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String,
            let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
        else {
            throw TeaNodeError.refused(String(localized: "Photo backup storage is unavailable. Check this app's shared-container entitlement."))
        }
        return container.appendingPathComponent("PhotoBackup", isDirectory: true)
    }

    /// Where a file waits between being read from the library and being sent.
    nonisolated static func outgoingDirectory() throws -> URL {
        try directory().appendingPathComponent("outgoing", isDirectory: true)
    }

    private func makeIndex() throws -> PhotoBackupIndex {
        if let index { return index }
        let folder = try Self.directory()
        let made = try PhotoBackupIndex(directory: folder)
        // The whole folder stays out of the device backup, the index and the
        // library's change token with the files being sent: restored on
        // another phone they would be taken for that phone's own.
        Self.excludeFromDeviceBackup(folder)
        index = made
        return made
    }

    private static func excludeFromDeviceBackup(_ url: URL) {
        var excluded = URLResourceValues()
        excluded.isExcludedFromBackup = true
        var excludedURL = url
        try? excludedURL.setResourceValues(excluded)
    }

    // MARK: Launch

    /// Registers the two background tasks. It has to be done before the app has
    /// finished launching, or the system refuses them.
    static func registerBackgroundTasks() {
        // On the main queue, where the handler's main actor isolation holds:
        // with no queue the system calls it on one of its own.
        BGTaskScheduler.shared.register(forTaskWithIdentifier: processingIdentifier, using: .main) { task in
            nonisolated(unsafe) let task = task
            Task { @MainActor in
                guard let task = task as? BGProcessingTask else { return task.setTaskCompleted(success: false) }
                await shared.runOvernight(task)
            }
        }
        BGTaskScheduler.shared.register(forTaskWithIdentifier: continuedIdentifier, using: .main) { task in
            nonisolated(unsafe) let task = task
            Task { @MainActor in
                guard let task = task as? BGContinuedProcessingTask else { return task.setTaskCompleted(success: false) }
                await shared.runContinued(task)
            }
        }
    }

    /// Said by the app delegate when the system started the app to deliver what
    /// the background session finished.
    func handleBackgroundEvents(identifier: String, completion: @escaping () -> Void) {
        guard identifier == PhotoBackupSession.identifier else {
            completion()
            return
        }
        attachSession()
        PhotoBackupSession.shared.keep(backgroundCompletion: completion)
    }

    /// Says to the background session who hears of a finished upload. Done
    /// wherever a pass can start, the launch of a background task included,
    /// or an upload that finishes with nobody attached is not recorded.
    private func attachSession() {
        PhotoBackupSession.shared.attach(
            finishHandler: { job, outcome in
                await PhotoBackupEngine.shared.sessionFinished(job, outcome: outcome)
            },
            flushHandler: {
                await PhotoBackupEngine.shared.flushIndex()
            },
            topUpHandler: {
                await PhotoBackupEngine.shared.topUpAfterWake()
            })
    }

    /// How long the app spends queueing files when the system wakes it because
    /// uploads finished. The system gives a woken app about 30 seconds before
    /// it must call the completion handler; this stays well inside that, and
    /// leaves time for the index to be written.
    nonisolated static let wakeTopUpSeconds: TimeInterval = 18

    /// The app was woken in the background because uploads finished: the
    /// session has room again, so more files that are on the phone already are
    /// exported and queued, smallest first, until the deadline. Files in
    /// iCloud wait for the app to be opened or for the night. A pass that is
    /// working is left to do this itself.
    func topUpAfterWake() async {
        guard isEnabled, !isPaused, !isTurningOff, let coordinator = try? makeCoordinator() else { return }
        await coordinator.topUp(settings: currentSettings, until: Date().addingTimeInterval(Self.wakeTopUpSeconds))
        await refresh()
        finishedWorkCount += 1
    }

    /// Writes the index, if there is one yet. The background session asks for
    /// it before it tells the system it is done with its events.
    func flushIndex() async {
        await index?.flush()
    }

    /// The app has gone to the background: writes the index.
    func sceneWentToBackground() {
        guard isEnabled, let index else { return }
        let assertion = UIApplication.shared.beginBackgroundTask(withName: "Photo backup index")
        Task {
            await index.flush()
            UIApplication.shared.endBackgroundTask(assertion)
        }
    }

    /// The app has come to the foreground, or has started: read the library
    /// for what is new.
    func sceneBecameActive() {
        #if DEBUG
            if DemoServer.isRequested { return }
        #endif
        guard isEnabled else { return }
        attachSession()
        startWatchingNetwork()
        Task {
            await self.refresh()
            self.startPasses()
        }
    }

    // MARK: The network

    /// Watches the network while the app runs. When it comes back, or Wi-Fi
    /// replaces mobile data, and files were left for later, a pass starts at
    /// once instead of waiting for the app to be opened again.
    private func startWatchingNetwork() {
        guard networkMonitor == nil else { return }
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { path in
            let state = PhotoBackupNetworkState(isSatisfied: path.status == .satisfied, isExpensive: path.isExpensive)
            Task { @MainActor in PhotoBackupEngine.shared.networkChanged(to: state) }
        }
        monitor.start(queue: .main)
        networkMonitor = monitor
    }

    private func stopWatchingNetwork() {
        networkMonitor?.cancel()
        networkMonitor = nil
        lastNetworkState = nil
    }

    private func networkChanged(to state: PhotoBackupNetworkState) {
        let previous = lastNetworkState
        lastNetworkState = state
        guard isEnabled,
            PhotoBackupResumePolicy.shouldResume(
                from: previous, to: state, usesMobileData: usesMobileData, leftForLaterFileCount: status.leftForLaterFileCount,
                isPassRunning: passTask != nil)
        else { return }
        startPasses()
    }

    // MARK: Status

    /// Reads what the screens show: the switches, the password, the counts.
    func refresh() async {
        #if DEBUG
            if DemoServer.isRequested {
                applyDemo()
                return
            }
        #endif
        enabledAccountKey = preferences.enabledAccountKey
        guard let accountKey = enabledAccountKey else {
            credentialName = nil
            status = PhotoBackupStatus(phase: .off)
            return
        }
        credentialName = (try? credentials.load(accountKey: accountKey))?.credentialName
        let access = PhotoKitLibrary().access()
        accessProblem = access == .full ? nil : access
        if let coordinator, coordinatorAccountKey == accountKey {
            // With no pass running, uploads go on with the system: the session
            // is asked what it holds, so that the screen can say so.
            apply(await coordinator.refreshedStatus())
            return
        }
        guard let index = try? makeIndex() else { return }
        await index.adopt(accountKey: accountKey)
        let summary = await index.summary()
        var phase = PhotoBackupStatus.Phase.idle
        if access != .full {
            phase = .needsAccess(access)
        } else if summary.isSignInNeeded {
            phase = .needsSignIn
        }
        if isPaused { phase = .paused }
        apply(
            PhotoBackupStatus(
                phase: phase, backedUpItemCount: summary.backedUpItemCount, libraryItemCount: summary.libraryItemCount,
                refusedFileCount: summary.refusedFileCount, lastFailure: summary.lastFailure,
                lastPassFinishedAt: summary.lastPassFinishedAt))
    }

    private func apply(_ newStatus: PhotoBackupStatus) {
        guard isEnabled else {
            status = PhotoBackupStatus(phase: .off)
            return
        }
        status = newStatus
        if case .needsAccess(let access) = newStatus.phase { accessProblem = access }
        progressSink?(newStatus)
    }

    // MARK: Turning on and off

    /// Turns backup on: asks for the whole library, makes a device password for
    /// photos only, keeps it in the keychain, and starts the first pass.
    func turnOn(connection: Connection) async {
        guard !isWorking, AppModel.shared.isPhotosOffered else { return }
        if let enabledAccountName, enabledAccountKey != connection.accountKey {
            // One account at a time: the person turns the other off first.
            problemText = String(localized: "Photo backup is already on for \(enabledAccountName). Turn it off for that account first.")
            return
        }
        isWorking = true
        defer { isWorking = false }
        problemText = nil
        let access = await PhotoKitLibrary.requestAccess()
        guard access == .full else {
            accessProblem = access
            return
        }
        accessProblem = nil
        do {
            let credential = try await connection.createPhotoBackupCredential(named: Self.passwordName())
            try await saveOrRevoke(credential, accountKey: connection.accountKey, connection: connection)
            // The new password has not been refused: a sign-in flag kept from
            // before backup was turned off would stop the first pass.
            try await makeIndex().clearError()
            preferences.enabledAccountKey = connection.accountKey
            enabledAccountKey = connection.accountKey
            credentialName = credential.credentialName
            coordinator = nil
            coordinatorAccountKey = nil
            await refresh()
            attachSession()
            startWatchingNetwork()
            startPasses()
        } catch {
            problemText = AppModel.shared.describe(error)
        }
    }

    /// Keeps a device password the server just made in the keychain. When the
    /// keychain refuses it, the server's copy is revoked before the error is
    /// raised, so that no password nobody holds is left to work on the server.
    private func saveOrRevoke(_ credential: PhotoBackupCredential, accountKey: String, connection: Connection) async throws {
        do {
            try credentials.save(credential, accountKey: accountKey)
        } catch {
            try? await connection.revokeSyncCredential(credential.credentialId)
            throw error
        }
    }

    /// Turns backup off: stops what is running, revokes the device password and
    /// forgets it. What went up stays on the server, and the index stays, so
    /// that turning backup on again does not send everything again.
    func turnOff(connection: Connection?) async {
        guard !isWorking else { return }
        isWorking = true
        defer { isWorking = false }
        problemText = nil
        guard let accountKey = enabledAccountKey else { return }
        await turnOffNow(accountKey: accountKey, connection: connection)
    }

    /// The part of turning off that waits for nothing: the pass that is
    /// working is cancelled first, then backup is turned off.
    private func turnOffNow(accountKey: String, connection: Connection?) async {
        // First of all: a pass that is stopped, and the uploads that are
        // cancelled, report back, and none of that may start another pass.
        isTurningOff = true
        defer { isTurningOff = false }
        preferences.enabledAccountKey = nil
        enabledAccountKey = nil
        // Turned on again later, backup starts running, not paused.
        preferences.isPaused = false
        isPaused = false
        await stopEverything()
        stopWatchingNetwork()
        credentialName = nil
        accessProblem = nil
        status = PhotoBackupStatus(phase: .off)
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.processingIdentifier)
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.continuedIdentifier)
        guard let credential = try? credentials.load(accountKey: accountKey) else { return }
        try? credentials.delete(accountKey: accountKey)
        let revoking = connection?.accountKey == accountKey ? connection : Self.makeConnection(accountKey: accountKey)
        guard let revoking else {
            problemText = String(localized: "Photo backup is off. Revoke its password under Devices, then Sync passwords.")
            return
        }
        do {
            try await revoking.revokeSyncCredential(credential.credentialId)
            doneText = String(localized: "Photo backup is off and its password is revoked.")
        } catch {
            problemText = String(localized: "Photo backup is off, but its password could not be revoked. Revoke it under Devices, then Sync passwords.")
        }
    }

    /// An account is being signed out of or removed. If backup is on for it,
    /// the pass that is working is cancelled and backup turned off, even while
    /// a switch is being turned (nothing waits for that to finish). What is
    /// kept for the account goes with it: the index of what went up, and the
    /// previews on disk and in memory.
    func accountRemoved(accountKey: String, connection: Connection?) async {
        if enabledAccountKey == accountKey || preferences.enabledAccountKey == accountKey {
            isWorking = true
            defer { isWorking = false }
            problemText = nil
            await turnOffNow(accountKey: accountKey, connection: connection)
        }
        if let index = try? makeIndex() { await index.forget(accountKey: accountKey) }
        await PhotoImages.forget(accountKey: accountKey)
    }

    /// The server did not accept the device password. A new one is made, the
    /// old one revoked if the server still knows it, and the backup goes on.
    func signInAgain(connection: Connection) async {
        guard !isWorking, let accountKey = enabledAccountKey else { return }
        isWorking = true
        defer { isWorking = false }
        problemText = nil
        do {
            let old = try? credentials.load(accountKey: accountKey)
            let credential = try await connection.createPhotoBackupCredential(named: Self.passwordName())
            try await saveOrRevoke(credential, accountKey: accountKey, connection: connection)
            if let old { try? await connection.revokeSyncCredential(old.credentialId) }
            credentialName = credential.credentialName
            coordinator = nil
            coordinatorAccountKey = nil
            try await makeIndex().clearError()
            await refresh()
            startPasses()
        } catch {
            problemText = AppModel.shared.describe(error)
        }
    }

    private static func passwordName() -> String {
        String(localized: "\(String(UIDevice.current.name.prefix(100))) photo backup")
    }

    private static func makeConnection(accountKey: String) -> Connection? {
        if let connection = AppModel.shared.connection, connection.accountKey == accountKey { return connection }
        guard let account = try? KeychainAccountStore().load(accountKey: accountKey) else { return nil }
        return Connection(account: account, transport: URLSessionTransport(), store: KeychainAccountStore())
    }

    private func stopEverything() async {
        let working = passTask
        working?.cancel()
        await coordinator?.stop()
        // The pass ends soon after it is asked to stop; one stuck on a long
        // download from iCloud is not waited for.
        if let working { await Self.wait(for: working, upTo: .seconds(5)) }
        await PhotoBackupSession.shared.cancelAll()
        passTask = nil
        passGeneration += 1
        coordinator = nil
        coordinatorAccountKey = nil
        if let outgoing = try? Self.outgoingDirectory() { try? FileManager.default.removeItem(at: outgoing) }
    }

    private static func wait(for task: Task<Void, Never>, upTo limit: Duration) async {
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await task.value }
            group.addTask { try? await Task.sleep(for: limit) }
            await group.next()
            group.cancelAll()
        }
    }

    // MARK: Passes

    /// Back up now: clears the files the server refused so that they are tried
    /// again, runs a pass at once, and asks the system for a continued
    /// processing task so that the pass goes on with the app in the background.
    func backUpNow() async {
        guard isEnabled, !isPaused else { return }
        problemText = nil
        if let index = try? makeIndex() {
            await index.clearRefusals()
            await index.clearErrorUnlessSignInIsNeeded()
        }
        startPasses()
        let request = BGContinuedProcessingTaskRequest(
            identifier: Self.continuedIdentifier, title: String(localized: "Backing up photos"), subtitle: String(localized: "Starting"))
        request.strategy = .queue
        // If the system will not run it, the pass above still goes on while the
        // app is open.
        try? BGTaskScheduler.shared.submit(request)
        await refresh()
    }

    /// Pauses backup until the person resumes: the pass that is working is
    /// stopped, the uploads the background session holds are cancelled so that
    /// the phone stops sending, and no pass, background task or retry starts
    /// until `resume()`. What went up, the index and the place the listing got
    /// to are kept. A file that was part way up starts again from its first
    /// byte after the pause, since the app starts each file as a new upload.
    func pause() async {
        guard isEnabled, !isPaused, !isTurningOff else { return }
        isPaused = true
        preferences.isPaused = true
        problemText = nil
        retryTask?.cancel()
        retryTask = nil
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.processingIdentifier)
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.continuedIdentifier)
        let working = passTask
        working?.cancel()
        await coordinator?.pause()
        if let working { await Self.wait(for: working, upTo: .seconds(5)) }
        // Whatever the pass queued as it was stopping, and the files exported
        // for it. Also the uploads of an earlier launch, when no coordinator
        // has been made in this one.
        await coordinator?.pause()
        await PhotoBackupSession.shared.cancelAll()
        passTask = nil
        passGeneration += 1
        await refresh()
    }

    /// Lets backup go on, and starts a pass at once.
    func resume() async {
        guard isEnabled, isPaused, !isTurningOff else { return }
        isPaused = false
        preferences.isPaused = false
        await coordinator?.resume()
        startWatchingNetwork()
        startPasses()
        await refresh()
    }

    /// Runs passes until one sends nothing new, and then again whenever an
    /// upload finishes (`sessionFinished`) while files are still to send, so
    /// that the library keeps being worked through as uploads finish, not only
    /// when the app is next opened. A pass already running is joined, not
    /// started twice.
    @discardableResult
    func startPasses() -> Task<Void, Never>? {
        if let passTask { return passTask }
        guard !isPaused, !isTurningOff, let coordinator = try? makeCoordinator() else { return nil }
        passGeneration += 1
        let generation = passGeneration
        let task = Task { [weak self] in
            var result: PhotoBackupPassResult
            var wantsAnotherPass: Bool
            repeat {
                guard let self else { return }
                self.isAnotherPassWanted = false
                result = await coordinator.runPass(settings: self.currentSettings)
                wantsAnotherPass = self.isAnotherPassWanted
            } while (result.uploadedFileCount > 0 || wantsAnotherPass) && !result.wasStopped && !result.wasAlreadyRunning && !Task.isCancelled
            await self?.passesEnded(generation: generation)
        }
        passTask = task
        return task
    }

    private var currentSettings: PhotoBackupCoordinator.Settings {
        PhotoBackupCoordinator.Settings(usesMobileData: usesMobileData, includesHidden: includesHidden)
    }

    private func passesEnded(generation: Int) async {
        if generation == passGeneration { passTask = nil }
        await refresh()
        finishedWorkCount += 1
        scheduleOvernightPass()
        await scheduleRetryPass()
    }

    /// When the server asked for the files to be sent again later (a
    /// Retry-After), a pass starts then, if the app is still running.
    private func scheduleRetryPass() async {
        retryTask?.cancel()
        retryTask = nil
        guard isEnabled, !isPaused, let retryTime = await coordinator?.pendingRetryTime else { return }
        retryTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(max(1, retryTime.timeIntervalSinceNow)))
            guard !Task.isCancelled else { return }
            self?.startPasses()
        }
    }

    func stopPasses() {
        Task { await self.coordinator?.stop() }
    }

    /// A setting the queued uploads were made with has changed: the pass is
    /// stopped, the session's tasks are cancelled, and a new pass queues the
    /// files again under the new setting.
    private func restartPasses() async {
        guard isEnabled, !isPaused, !isTurningOff else { return }
        let working = passTask
        working?.cancel()
        await coordinator?.stop()
        if let working { await Self.wait(for: working, upTo: .seconds(5)) }
        // Not for a backup turned off or paused while this waited.
        guard isEnabled, !isPaused, !isTurningOff else { return }
        await PhotoBackupSession.shared.cancelAll()
        passTask = nil
        passGeneration += 1
        startPasses()
    }

    /// The coordinator for the account backup is on for, made once.
    private func makeCoordinator() throws -> PhotoBackupCoordinator? {
        guard let accountKey = enabledAccountKey ?? preferences.enabledAccountKey else { return nil }
        if let coordinator, coordinatorAccountKey == accountKey { return coordinator }
        guard let account = try KeychainAccountStore().load(accountKey: accountKey),
            let credential = try credentials.load(accountKey: accountKey)
        else { return nil }
        let outgoing = try Self.outgoingDirectory()
        try FileManager.default.createDirectory(at: outgoing, withIntermediateDirectories: true)
        Self.excludeFromDeviceBackup(outgoing)
        let connection = Self.makeConnection(accountKey: accountKey)
        let made = PhotoBackupCoordinator(
            index: try makeIndex(), library: PhotoKitLibrary(), transfer: PhotoBackupSession.shared, lookup: connection, network: DeviceNetwork(),
            outgoingDirectory: outgoing, server: account.server, accountKey: accountKey, credential: credential,
            freeSpaceBytes: { Self.freeSpaceBytes(at: outgoing) }, isPaused: isPaused)
        coordinator = made
        coordinatorAccountKey = accountKey
        // A pass started by a background task launch has not come through the
        // scene, and the uploads it queues report to whoever is attached.
        attachSession()
        Task {
            await made.setStatusHandler { newStatus in
                DispatchQueue.main.async {
                    MainActor.assumeIsolated { PhotoBackupEngine.shared.apply(newStatus) }
                }
            }
        }
        return made
    }

    /// An upload finished in the background session, perhaps while the app was
    /// closed. It is recorded. When it went up and files are still to send, a
    /// pass is started to queue more, if the app is not in the background: in the
    /// background the app has seconds, not a pass, and `topUpAfterWake` queues
    /// files when the system has delivered everything it woke the app for. A
    /// refused file, one to try again later, and one the app cancelled start
    /// nothing, so a file that always fails cannot start passes in a loop.
    func sessionFinished(_ job: PhotoBackupJob, outcome: PhotoBackupOutcome) async {
        guard !isTurningOff, enabledAccountKey == job.accountKey || preferences.enabledAccountKey == job.accountKey,
            let coordinator = try? makeCoordinator()
        else {
            PhotoBackupSession.removeTemporaryFile(of: job)
            return
        }
        let shouldStartPass = await coordinator.finished(job, outcome: outcome)
        guard shouldStartPass, !isTurningOff, UIApplication.shared.applicationState != .background else { return }
        if passTask != nil {
            // The pass task is still there, perhaps just about to end: it runs
            // once more before it does.
            isAnotherPassWanted = true
        } else {
            startPasses()
        }
    }

    /// The room the phone has left for the person's own use, which is what the
    /// system counts as available (it can clear caches to make more). The
    /// queue of exported files is held to a quarter of this.
    nonisolated static func freeSpaceBytes(at url: URL) -> Int64? {
        let values = try? url.resourceValues(forKeys: [.volumeAvailableCapacityForImportantUsageKey])
        return values?.volumeAvailableCapacityForImportantUsage
    }

    // MARK: Background tasks

    /// Asks the system for a pass overnight, when the phone is on power and a
    /// network.
    func scheduleOvernightPass() {
        guard isEnabled, !isPaused else { return }
        let request = BGProcessingTaskRequest(identifier: Self.processingIdentifier)
        request.requiresNetworkConnectivity = true
        request.requiresExternalPower = true
        // Soon when files wait for the network or the listing of the library is not
        // over, otherwise only a look for new photos.
        request.earliestBeginDate = Date(
            timeIntervalSinceNow: PhotoBackupSchedule.earliestBeginDelaySeconds(
                leftForLaterFileCount: status.leftForLaterFileCount + status.waitingFileCount, isListingUnfinished: status.isListingUnfinished))
        try? BGTaskScheduler.shared.submit(request)
    }

    private func runOvernight(_ task: BGProcessingTask) async {
        guard isEnabled, !isPaused else {
            task.setTaskCompleted(success: true)
            return
        }
        task.expirationHandler = {
            Task { @MainActor in
                PhotoBackupEngine.shared.stopPasses()
                await PhotoBackupEngine.shared.flushIndex()
            }
        }
        scheduleOvernightPass()
        await startPasses()?.value
        task.setTaskCompleted(success: true)
    }

    /// The units the continued processing task's progress is counted in: a
    /// thousandth of the pass each, so that the listing of a library and the
    /// files after it can share one bar.
    private static let continuedProgressUnitCount: Int64 = 1_000

    private func runContinued(_ task: BGContinuedProcessingTask) async {
        guard isEnabled, !isPaused else {
            task.setTaskCompleted(success: true)
            return
        }
        // iOS ends a task whose progress stays where it was. When it does, the
        // pass is stopped and the index written, so that the next pass goes on
        // from where this one got to: the listing from its resume point, the
        // files from the order they are in.
        task.expirationHandler = {
            Task { @MainActor in
                PhotoBackupEngine.shared.stopPasses()
                await PhotoBackupEngine.shared.flushIndex()
            }
        }
        task.progress.totalUnitCount = Self.continuedProgressUnitCount
        task.progress.completedUnitCount = 0
        progressSink = { [weak task] status in
            guard let task else { return }
            // The listing of the library counts as a share of the work and the
            // files as the rest, so the bar moves while the library is read. It
            // does not go back when the files planned grow with each batch.
            let completedUnitCount = Int64(status.progressFraction * Double(Self.continuedProgressUnitCount))
            task.progress.completedUnitCount = max(task.progress.completedUnitCount, min(completedUnitCount, Self.continuedProgressUnitCount))
            task.updateTitle(String(localized: "Backing up photos"), subtitle: Self.progressSubtitle(for: status))
        }
        await startPasses()?.value
        progressSink = nil
        task.setTaskCompleted(success: status.phase != .needsSignIn)
    }

    /// What the system shows under the title of the task: how far the listing
    /// of the library has got while it lists, the files after that.
    private static func progressSubtitle(for status: PhotoBackupStatus) -> String {
        if status.phase == .listing, let listedItemCount = status.listedItemCount {
            return String(localized: "Reading your photo library: \(listedItemCount.formatted()) of \(status.libraryItemCount.formatted())")
        }
        return String(localized: "\(status.shownFinishedFileCount) of \(status.plannedFileCount) files")
    }

    // MARK: Demo

    #if DEBUG
        /// The two Photo Backup screens CI photographs, with invented counts.
        private func applyDemo() {
            let isWaiting = DemoServer.screen == "photos-backup-waiting"
            // `photos-backup-background`: the app was left, the system is sending a long queue, and
            // more files wait for the app to be opened or for the night.
            let isInBackground = DemoServer.screen == "photos-backup-background"
            // `photos-backup-paused`: the person paused backup part way through the library.
            let isPausedDemo = DemoServer.screen == "photos-backup-paused"
            let isOn = DemoServer.screen == "photos-backup-on" || isWaiting || isInBackground || isPausedDemo
            // `settings-photo-backup-other-account` and `photos-backup-other-account` have backup on for a
            // different account than the one signed in.
            let isOnForOtherAccount = DemoServer.screen == "settings-photo-backup-other-account" || DemoServer.screen == "photos-backup-other-account"
            // The signed-in demo account's own key, as the backup screen compares it:
            // a key typed by hand left out the scheme the account's address writes.
            enabledAccountKey = isOn ? DemoServer.account.accountKey : (isOnForOtherAccount ? "photos.example.org|sam" : nil)
            credentialName = isOn ? "Robin's iPhone photo backup" : nil
            accessProblem = nil
            if isWaiting {
                status = PhotoBackupStatus(
                    phase: .waitingForNetwork, backedUpItemCount: 3_890, libraryItemCount: 3_912, leftForLaterFileCount: 22,
                    lastFailure: .serverReason("photo storage is unavailable"), lastPassFinishedAt: Date(timeIntervalSinceNow: -600))
                return
            }
            if isPausedDemo {
                status = PhotoBackupStatus(
                    phase: .paused, backedUpItemCount: 1_284, libraryItemCount: 3_912, lastPassFinishedAt: Date(timeIntervalSinceNow: -3_600))
                return
            }
            if isInBackground {
                status = PhotoBackupStatus(
                    phase: .idle, backedUpItemCount: 1_284, libraryItemCount: 3_912, queuedFileCount: 200,
                    lastPassFinishedAt: Date(timeIntervalSinceNow: -3_600), waitingFileCount: 1_407)
                return
            }
            status =
                isOn
                ? PhotoBackupStatus(
                    phase: .uploading, backedUpItemCount: 1284, libraryItemCount: 3912, plannedFileCount: 2891, finishedFileCount: 1284,
                    queuedFileCount: 4, lastPassFinishedAt: Date(timeIntervalSinceNow: -86_400), estimatedSecondsLeft: 3 * 3_600 + 200)
                : (isOnForOtherAccount
                    ? PhotoBackupStatus(phase: .idle, backedUpItemCount: 3_912, libraryItemCount: 3_912, lastPassFinishedAt: Date(timeIntervalSinceNow: -3_600))
                    : PhotoBackupStatus(phase: .off))
        }
    #endif
}
