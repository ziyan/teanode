import BackgroundTasks
import Foundation
import Observation
import TeaNodeKit
import UIKit

/// PhotoBackupEngine backs up this iPhone's photo library to the server. The
/// screens read its `status` and turn it on and off; the work is done by a
/// PhotoBackupCoordinator (TeaNodeKit), which reads the library, queues the
/// files with the background URLSession (PhotoBackupSession) and records what
/// went up. This class decides when a pass runs: when the app comes to the
/// foreground, when the person taps Back up now (a continued processing task, so
/// that it goes on with the app in the background), and overnight on power
/// (a processing task).
@MainActor
@Observable
final class PhotoBackupEngine {
    static let shared = PhotoBackupEngine()

    /// A pass overnight, on power and a network.
    static let processingIdentifier = "com.teanode.app.photo-backup"
    /// A pass the person asked for, which the system shows in a Live Activity
    /// and lets run with the app in the background.
    static let continuedIdentifier = "com.teanode.app.photo-backup-now"

    // MARK: What the screens read

    private(set) var status = PhotoBackupStatus()
    /// The name of the device password backup signs in with.
    private(set) var credentialName: String?
    /// How much of the library the app may read, when that is not enough.
    private(set) var accessProblem: PhotoBackupAccess?
    private(set) var isWorking = false
    var problemText: String?
    var doneText: String?

    var usesMobileData: Bool {
        didSet { preferences.usesMobileData = usesMobileData }
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

    // MARK: What it keeps

    @ObservationIgnored private let preferences = PhotoBackupPreferences()
    @ObservationIgnored private let credentials = PhotoBackupCredentialStore(secrets: KeychainSecrets())
    @ObservationIgnored private var index: PhotoBackupIndex?
    @ObservationIgnored private var coordinator: PhotoBackupCoordinator?
    @ObservationIgnored private var coordinatorAccountKey: String?
    @ObservationIgnored private var passTask: Task<Void, Never>?
    /// Told of every status while a continued processing task runs, to report
    /// progress to the system.
    @ObservationIgnored private var progressSink: (@MainActor (PhotoBackupStatus) -> Void)?

    private init() {
        usesMobileData = preferences.usesMobileData
        includesHidden = preferences.includesHidden
        enabledAccountKey = preferences.enabledAccountKey
    }

    // MARK: Where the files are

    /// The backup's folder in the app group, which the index and the files
    /// being sent live in.
    nonisolated static func directory() throws -> URL {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String,
            let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
        else {
            throw TeaNodeError.refused("Backup storage is unavailable. Check this app's shared-container entitlement.")
        }
        return container.appendingPathComponent("PhotoBackup", isDirectory: true)
    }

    /// Where a file waits between being read from the library and being sent.
    nonisolated static func outgoingDirectory() throws -> URL {
        try directory().appendingPathComponent("outgoing", isDirectory: true)
    }

    private func makeIndex() throws -> PhotoBackupIndex {
        if let index { return index }
        let made = try PhotoBackupIndex(directory: Self.directory())
        index = made
        return made
    }

    // MARK: Launch

    /// Registers the two background tasks. It has to be done before the app has
    /// finished launching, or the system refuses them.
    static func registerBackgroundTasks() {
        BGTaskScheduler.shared.register(forTaskWithIdentifier: processingIdentifier, using: nil) { task in
            nonisolated(unsafe) let task = task
            Task { @MainActor in
                guard let task = task as? BGProcessingTask else { return task.setTaskCompleted(success: false) }
                await shared.runOvernight(task)
            }
        }
        BGTaskScheduler.shared.register(forTaskWithIdentifier: continuedIdentifier, using: nil) { task in
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
        PhotoBackupSession.shared.attach { job, outcome in
            await PhotoBackupEngine.shared.sessionFinished(job, outcome: outcome)
        }
        PhotoBackupSession.shared.keep(backgroundCompletion: completion)
    }

    /// The app has come to the foreground, or has started: read the library
    /// for what is new.
    func sceneBecameActive() {
        #if DEBUG
            if DemoServer.isRequested { return }
        #endif
        guard isEnabled else { return }
        PhotoBackupSession.shared.attach { job, outcome in
            await PhotoBackupEngine.shared.sessionFinished(job, outcome: outcome)
        }
        Task {
            await self.refresh()
            self.startPasses()
        }
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
            apply(await coordinator.status())
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
        apply(
            PhotoBackupStatus(
                phase: phase, backedUpItemCount: summary.backedUpItemCount, libraryItemCount: summary.libraryItemCount,
                refusedFileCount: summary.refusedFileCount, lastErrorMessage: summary.lastErrorMessage,
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
        guard !isWorking else { return }
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
            try credentials.save(credential, accountKey: connection.accountKey)
            preferences.enabledAccountKey = connection.accountKey
            enabledAccountKey = connection.accountKey
            credentialName = credential.credentialName
            coordinator = nil
            coordinatorAccountKey = nil
            await refresh()
            PhotoBackupSession.shared.attach { job, outcome in
                await PhotoBackupEngine.shared.sessionFinished(job, outcome: outcome)
            }
            startPasses()
        } catch {
            problemText = AppModel.shared.describe(error)
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
        await stopEverything()
        preferences.enabledAccountKey = nil
        enabledAccountKey = nil
        credentialName = nil
        accessProblem = nil
        status = PhotoBackupStatus(phase: .off)
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.processingIdentifier)
        BGTaskScheduler.shared.cancel(taskRequestWithIdentifier: Self.continuedIdentifier)
        guard let credential = try? credentials.load(accountKey: accountKey) else { return }
        try? credentials.delete(accountKey: accountKey)
        let revoking = connection?.accountKey == accountKey ? connection : Self.makeConnection(accountKey: accountKey)
        guard let revoking else {
            problemText = String(localized: "Backup is off. Revoke its password under Devices, then Sync passwords.")
            return
        }
        do {
            try await revoking.revokeSyncCredential(credential.credentialId)
            doneText = String(localized: "Backup is off and its password is revoked.")
        } catch {
            problemText = String(localized: "Backup is off, but its password could not be revoked. Revoke it under Devices, then Sync passwords.")
        }
    }

    /// The account backup is on for is being signed out of: backup goes with it.
    func accountRemoved(accountKey: String, connection: Connection?) async {
        guard enabledAccountKey == accountKey else { return }
        await turnOff(connection: connection)
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
            try credentials.save(credential, accountKey: accountKey)
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
        await coordinator?.stop()
        await PhotoBackupSession.shared.cancelAll()
        passTask = nil
        coordinator = nil
        coordinatorAccountKey = nil
        if let outgoing = try? Self.outgoingDirectory() { try? FileManager.default.removeItem(at: outgoing) }
    }

    // MARK: Passes

    /// Back up now: clears the files the server refused so that they are tried
    /// again, runs a pass at once, and asks the system for a continued
    /// processing task so that the pass goes on with the app in the background.
    func backUpNow() async {
        guard isEnabled else { return }
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

    /// Runs passes until one sends nothing new. A pass already running is
    /// joined, not started twice.
    @discardableResult
    func startPasses() -> Task<Void, Never>? {
        if let passTask { return passTask }
        guard let coordinator = try? makeCoordinator() else { return nil }
        let task = Task { [weak self] in
            var result: PhotoBackupPassResult
            repeat {
                guard let self else { return }
                result = await coordinator.runPass(settings: self.currentSettings)
            } while result.uploadedFileCount > 0 && !result.wasStopped && !result.wasAlreadyRunning
            await self?.passesEnded()
        }
        passTask = task
        return task
    }

    private var currentSettings: PhotoBackupCoordinator.Settings {
        PhotoBackupCoordinator.Settings(usesMobileData: usesMobileData, includesHidden: includesHidden)
    }

    private func passesEnded() async {
        passTask = nil
        await refresh()
        scheduleOvernightPass()
    }

    func stopPasses() {
        Task { await self.coordinator?.stop() }
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
        var excluded = URLResourceValues()
        excluded.isExcludedFromBackup = true
        var outgoingURL = outgoing
        try? outgoingURL.setResourceValues(excluded)
        let connection = Self.makeConnection(accountKey: accountKey)
        let made = PhotoBackupCoordinator(
            index: try makeIndex(), library: PhotoKitLibrary(), transfer: PhotoBackupSession.shared, lookup: connection, network: DeviceNetwork(),
            outgoingDirectory: outgoing, server: account.server, accountKey: accountKey, credential: credential)
        coordinator = made
        coordinatorAccountKey = accountKey
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
    /// closed. It is recorded, and a pass is started to queue more.
    func sessionFinished(_ job: PhotoBackupJob, outcome: PhotoBackupOutcome) async {
        guard enabledAccountKey == job.accountKey || preferences.enabledAccountKey == job.accountKey,
            let coordinator = try? makeCoordinator()
        else {
            PhotoBackupSession.removeTemporaryFile(of: job)
            return
        }
        await coordinator.finished(job, outcome: outcome)
        startPasses()
    }

    // MARK: Background tasks

    /// Asks the system for a pass overnight, when the phone is on power and a
    /// network.
    func scheduleOvernightPass() {
        guard isEnabled else { return }
        let request = BGProcessingTaskRequest(identifier: Self.processingIdentifier)
        request.requiresNetworkConnectivity = true
        request.requiresExternalPower = true
        request.earliestBeginDate = Date(timeIntervalSinceNow: 60 * 60)
        try? BGTaskScheduler.shared.submit(request)
    }

    private func runOvernight(_ task: BGProcessingTask) async {
        guard isEnabled else {
            task.setTaskCompleted(success: true)
            return
        }
        task.expirationHandler = {
            Task { @MainActor in PhotoBackupEngine.shared.stopPasses() }
        }
        scheduleOvernightPass()
        await startPasses()?.value
        task.setTaskCompleted(success: true)
    }

    private func runContinued(_ task: BGContinuedProcessingTask) async {
        guard isEnabled else {
            task.setTaskCompleted(success: true)
            return
        }
        task.expirationHandler = {
            Task { @MainActor in PhotoBackupEngine.shared.stopPasses() }
        }
        task.progress.totalUnitCount = 1
        task.progress.completedUnitCount = 0
        progressSink = { [weak task] status in
            guard let task else { return }
            // Items done out of items planned in this run.
            task.progress.totalUnitCount = Int64(max(status.plannedFileCount, 1))
            task.progress.completedUnitCount = Int64(min(status.finishedFileCount, max(status.plannedFileCount, 1)))
            task.updateTitle(
                String(localized: "Backing up photos"),
                subtitle: String(localized: "\(status.finishedFileCount) of \(status.plannedFileCount) files"))
        }
        await startPasses()?.value
        progressSink = nil
        task.setTaskCompleted(success: status.phase != .needsSignIn)
    }

    // MARK: Demo

    #if DEBUG
        /// The two Photo Backup screens CI photographs, with invented counts.
        private func applyDemo() {
            let isOn = DemoServer.screen == "photos-backup-on"
            enabledAccountKey = isOn ? "demo.example.com|robin" : nil
            credentialName = isOn ? "Robin's iPhone photo backup" : nil
            accessProblem = nil
            status =
                isOn
                ? PhotoBackupStatus(
                    phase: .uploading, backedUpItemCount: 1284, libraryItemCount: 3912, plannedFileCount: 2891, finishedFileCount: 1284,
                    queuedFileCount: 4, currentFilename: "IMG_2041.MOV", lastPassFinishedAt: Date(timeIntervalSinceNow: -86_400))
                : PhotoBackupStatus(phase: .off)
        }
    #endif
}
