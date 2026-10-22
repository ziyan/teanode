import CoreLocation
import Foundation
import Observation
import TeaNodeKit
import UIKit

/// LivePosition is where the phone is now, as the last reading said.
struct LivePosition: Equatable, Sendable {
    let latitudeDegrees: Double
    let longitudeDegrees: Double
    let accuracyMeters: Double
    let measuredAt: Date

    var coordinate: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: latitudeDegrees, longitude: longitudeDegrees) }
}

/// LocationReading is one reading of Core Location with only what the
/// recorder keeps, so that it can cross from the delegate to the main actor.
struct LocationReading: Sendable {
    let latitudeDegrees: Double
    let longitudeDegrees: Double
    let accuracyMeters: Double
    let altitudeMeters: Double?
    let speedMetersPerSecond: Double?
    let courseDegrees: Double?
    let measuredAt: Date

    init(_ location: CLLocation) {
        latitudeDegrees = location.coordinate.latitude
        longitudeDegrees = location.coordinate.longitude
        accuracyMeters = location.horizontalAccuracy
        // Core Location writes "could not tell" as a negative number.
        altitudeMeters = location.verticalAccuracy >= 0 ? location.altitude : nil
        speedMetersPerSecond = location.speed >= 0 ? location.speed : nil
        courseDegrees = location.course >= 0 ? location.course : nil
        measuredAt = location.timestamp
    }

    var reportedPoint: ReportedPoint {
        ReportedPoint(
            latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees, accuracyMeters: accuracyMeters, altitudeMeters: altitudeMeters,
            speedMetersPerSecond: speedMetersPerSecond, courseDegrees: courseDegrees, measuredAt: measuredAt)
    }
}

/// VisitReading is one visit iOS noticed.
struct VisitReading: Sendable {
    let latitudeDegrees: Double
    let longitudeDegrees: Double
    let accuracyMeters: Double
    /// Nil when iOS did not see the arrival.
    let arrivedAt: Date?
    /// Nil while the person is still there.
    let departedAt: Date?

    init(_ visit: CLVisit) {
        latitudeDegrees = visit.coordinate.latitude
        longitudeDegrees = visit.coordinate.longitude
        accuracyMeters = max(0, visit.horizontalAccuracy)
        arrivedAt = visit.arrivalDate == .distantPast ? nil : visit.arrivalDate
        departedAt = visit.departureDate == .distantFuture ? nil : visit.departureDate
    }

    var reportedVisit: ReportedVisit {
        ReportedVisit(
            clientVisitKey: LocationVisitKey.visit(
                latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees, arrivedAt: arrivedAt, departedAt: departedAt),
            latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees, accuracyMeters: accuracyMeters, arrivedAt: arrivedAt,
            departedAt: departedAt, visitSource: "visit")
    }
}

/// LocationManagerDelegate is what Core Location tells the app, passed on to
/// the recorder on the main actor. iOS relaunches a closed app for these
/// events and delivers them to the manager that exists then, so the
/// recorder makes its manager, and this delegate, as the app finishes
/// launching.
private final class LocationManagerDelegate: NSObject, CLLocationManagerDelegate, @unchecked Sendable {
    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        let authorizationStatus = manager.authorizationStatus
        Task { @MainActor in LocationRecorder.shared.authorizationDidChange(authorizationStatus) }
    }

    /// A significant change of location: about 500 meters.
    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        let readings = locations.map(LocationReading.init)
        Task { @MainActor in LocationRecorder.shared.significantChangesArrived(readings) }
    }

    func locationManager(_ manager: CLLocationManager, didVisit visit: CLVisit) {
        let reading = VisitReading(visit)
        Task { @MainActor in LocationRecorder.shared.visitArrived(reading) }
    }

    func locationManager(_ manager: CLLocationManager, didFailWithError error: Error) {
        // Nothing to do: the next event tells again.
    }
}

/// BackgroundTaskHolder keeps the app running for the few seconds it takes to
/// hand the server what was noticed while the app was in the background.
@MainActor
private final class BackgroundTaskHolder {
    private var identifier = UIBackgroundTaskIdentifier.invalid

    init(name: String) {
        identifier = UIApplication.shared.beginBackgroundTask(withName: name) { [weak self] in self?.end() }
    }

    func end() {
        guard identifier != .invalid else { return }
        UIApplication.shared.endBackgroundTask(identifier)
        identifier = .invalid
    }
}

/// LocationRecorder records where the person goes, if they let it, and hands
/// it to the server.
///
/// While the app is open it follows the person live. While it is closed it
/// relies on what iOS wakes it for: a visit (arriving somewhere, leaving),
/// a significant change of location, and the person entering or leaving a
/// place they named or labeled. There is no continuous tracking in the
/// background and no `location` background mode.
///
/// Everything noticed goes into an outbox kept in a file for each account
/// (LocationOutbox), and leaves it only after the server answered.
///
/// A pause is the person's, kept by the server (LocationPauseTracker holds what
/// the phone believes of it). While one is in effect the phone keeps
/// listening for visits, significant changes and places, so that it needs no
/// waking when the pause ends, but puts nothing in the outbox for an instant
/// inside the pause, and stops following the person live.
@MainActor
@Observable
final class LocationRecorder {
    static let shared = LocationRecorder()

    /// What the person allowed.
    private(set) var authorizationStatus = CLAuthorizationStatus.notDetermined
    /// Where the phone is now, while the app is open and allowed to know.
    private(set) var livePosition: LivePosition?
    /// Whether the switch "Record where I go" is on.
    private(set) var isRecordingOn = false
    /// How many readings and visits wait to be sent.
    private(set) var unsentCount = 0
    /// What the server said when it refused to take what the phone noticed
    /// (the account may not use Location, or the batch is not valid), while
    /// that is why recording has stopped. Nil otherwise. Recording starts
    /// again when the person turns the switch off and on, or changes account.
    private(set) var refusalText: String?
    /// What the phone believes about the person's pause: the one the server
    /// last said was in effect, and a change made on the phone that the
    /// server has not taken yet.
    private(set) var pauseTracker = LocationPauseTracker()
    /// Counts the changes of the pause, and the ends of timed ones, which a
    /// screen reading `activePause` needs to read it again at.
    private(set) var pauseRevision = 0
    /// What the server said when it refused a pause or a resume made on the
    /// phone, until a screen has said it.
    private(set) var pauseRefusalText: String?

    /// The pause in effect now, with any change made on the phone applied.
    var activePause: LocationPause? {
        _ = pauseRevision
        return pauseTracker.isPaused(at: Date()) ? pauseTracker.pause : nil
    }

    /// Whether recording is paused now.
    var isPaused: Bool { activePause != nil }

    /// Whether a pause or a resume made on the phone has not reached the
    /// server yet, which keeps trying.
    var hasPendingPauseChange: Bool { pauseTracker.hasPendingChange }

    /// Whether Core Location may be asked where the phone is, at least
    /// while the app is open.
    var isAuthorizedToLocate: Bool { authorizationStatus == .authorizedWhenInUse || authorizationStatus == .authorizedAlways }

    /// Whether the person said "Always", which is what recording while the
    /// app is closed needs.
    var isAuthorizedAlways: Bool { authorizationStatus == .authorizedAlways }

    /// Whether the person turned location off for the phone or is not allowed
    /// to turn it on.
    var isLocationDenied: Bool { authorizationStatus == .denied || authorizationStatus == .restricted }

    /// Whether iOS has not asked the person yet.
    var isAuthorizationUndecided: Bool { authorizationStatus == .notDetermined }

    @ObservationIgnored private var manager: CLLocationManager?
    private let delegate = LocationManagerDelegate()
    @ObservationIgnored private var serviceSession: CLServiceSession?
    @ObservationIgnored private var liveTask: Task<Void, Never>?
    @ObservationIgnored private var liveTaskId: UUID?
    @ObservationIgnored private var pointFilter = LocationPointFilter()
    @ObservationIgnored private var isAppActive = false
    /// How many screens that show the person's position are open, which
    /// keeps the live position coming with the switch off.
    @ObservationIgnored private var viewerCount = 0
    /// The outbox of the account in use. The others wait in their own files.
    @ObservationIgnored private var outbox = LocationOutbox()
    @ObservationIgnored private var isFlushing = false
    @ObservationIgnored private var isFlushRequestedAgain = false
    /// Set while the history is being erased, so that nothing is sent that
    /// the erase would then have to take back.
    @ObservationIgnored private var isReportingHeld = false
    @ObservationIgnored private var retryGate = LocationRetryGate()
    @ObservationIgnored private var isOutboxUnwritten = false
    @ObservationIgnored private var outboxWrittenAt = Date.distantPast
    @ObservationIgnored private var outboxWriteTask: Task<Void, Never>?
    @ObservationIgnored private var monitorStartup: Task<CLMonitor, Never>?
    @ObservationIgnored private var placeMonitorTask: Task<Void, Never>?
    @ObservationIgnored private var watchedPlaces: [String: WatchedPlace] = [:]
    @ObservationIgnored private var placeWatchRefreshedAt = Date.distantPast
    /// Set when the account changes, until the places watched are replaced by
    /// the new account's.
    @ObservationIgnored private var arePlacesWatchedForPreviousAccount = false
    /// When each place was last given to iOS again with a changed center or
    /// radius: iOS then says at once that the person is inside it, which is
    /// not a new arrival for a visit already in progress.
    @ObservationIgnored private var placeAddedAt: [String: Date] = [:]
    /// The task that makes the end of a timed pause count, and the one that
    /// sends a change of pause until the server has it.
    @ObservationIgnored private var pauseEndTask: Task<Void, Never>?
    @ObservationIgnored private var pauseSendTask: Task<Void, Never>?
    @ObservationIgnored private var pauseSendTaskId: UUID?
    /// Set while the task that sends a change of pause sleeps between two
    /// tries, so that a tap can wake it instead of waiting out the retry.
    @ObservationIgnored private var isPauseSendWaiting = false
    private let defaults: UserDefaults
    private let outboxDirectory: URL?

    private static let isRecordingOnKey = "locationIsRecordingOn"
    private static let hasAskedForAlwaysKey = "locationHasAskedForAlways"
    private static let hasDeclinedAlwaysKey = "locationHasDeclinedAlways"
    /// Kept for each account (with the account key after it): set while the
    /// places watched may be satisfied from before a pause, until they are
    /// given to iOS again after it.
    private static let hasPlacesToWatchAgainKeyPrefix = "locationHasPlacesToWatchAgain."
    /// How often the watched places are read again from the server because
    /// the person moved a long way: no more than this.
    private static let placeWatchRefreshSeconds = 60.0
    /// How soon after a place was watched again an arrival at it is taken to
    /// be iOS saying where the person already is.
    private static let placeAddedEchoSeconds = 30.0
    /// How long the outbox may go unwritten while positions keep coming: a
    /// full one is megabytes of JSON, and a write for every position would
    /// cost the battery. Visits are written at once.
    private static let outboxWriteSeconds = 1.0

    private init() {
        let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String
        defaults = group.flatMap { UserDefaults(suiteName: $0) } ?? .standard
        let fileManager = FileManager.default
        let container =
            group.flatMap { fileManager.containerURL(forSecurityApplicationGroupIdentifier: $0) }
            ?? fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
        let directory = container?.appendingPathComponent("Location", isDirectory: true)
        if let directory { try? fileManager.createDirectory(at: directory, withIntermediateDirectories: true) }
        outboxDirectory = directory
        // Which account is in use is told by the model while it is made
        // (accountDidChange), which reads that account's outbox then.
    }

    // MARK: Starting

    /// Makes the one Core Location manager. Called as the app finishes
    /// launching, in the foreground or relaunched for a location event.
    func launch() {
        #if DEBUG
            // The screenshots run without asking anything of the system.
            if DemoServer.isRequested {
                authorizationStatus = .authorizedAlways
                isRecordingOn = true
                return
            }
        #endif
        guard manager == nil else { return }
        let manager = CLLocationManager()
        manager.delegate = delegate
        self.manager = manager
        authorizationStatus = manager.authorizationStatus
        isRecordingOn = defaults.object(forKey: Self.isRecordingOnKey) as? Bool ?? isAuthorizedAlways
        applyMonitoring()
        Task { await flush() }
    }

    /// The app came to the foreground, or left it. Leaving it for the
    /// background sends what waits and reads the places again; becoming
    /// inactive (Control Center, a system alert, the app switcher) does not.
    func sceneDidChange(isActive: Bool, isBackground: Bool) {
        isAppActive = isActive
        guard manager != nil else { return }
        if isActive {
            if let manager { authorizationStatus = manager.authorizationStatus }
            // A new session at every launch and every return to the
            // foreground: iOS ends background updates for one that was not
            // started in the foreground.
            startServiceSession()
            pointFilter.reset()
            startLiveUpdates()
            schedulePauseEnd()
            sendPendingPauseChange(isForced: true)
            Task {
                await refreshPauseFromServer()
                await flush(isForced: true)
                await refreshPlaceWatch()
            }
        } else {
            stopLiveUpdates()
            writeOutbox()
            guard isBackground else { return }
            // Only the flush: the background task it holds ends with it, and
            // a place edited in the app was already read again by
            // placesDidChange.
            Task { await flush(isForced: true) }
        }
    }

    /// The person named, labeled, moved, resized or forgot a place, or
    /// checked in at one: the places watched are read again at once, not
    /// when the app is next opened.
    func placesDidChange() {
        Task { await refreshPlaceWatch(isForced: true) }
    }

    /// A screen that shows the person's position appeared or went away.
    func viewerAppeared() {
        viewerCount += 1
        startLiveUpdates()
    }

    func viewerDisappeared() {
        viewerCount = max(0, viewerCount - 1)
        if viewerCount == 0, !isRecordingOn || isPaused { stopLiveUpdates() }
    }

    /// The account in use changed or was signed out of. What was noticed
    /// belongs to the person who was signed in.
    func accountDidChange(accountKey: String?) {
        // Told by the model while it is being made, so it does not ask the
        // model which account is in use.
        if outbox.accountKey != accountKey {
            // At launch there is no account in memory yet, and nothing of a
            // previous one is watched: only a change from one account to
            // another leaves its places in the monitor.
            let hadPreviousAccount = outbox.accountKey != nil
            useOutbox(accountKey: accountKey)
            loadPauseTracker(accountKey: accountKey)
            refusalText = nil
            retryGate = LocationRetryGate()
            // The places watched are the previous account's: they are
            // dropped by the next refresh before it reads this account's, so
            // that no arrival at one is reported for this account if its own
            // places cannot be read.
            placeWatchRefreshedAt = .distantPast
            if hadPreviousAccount { arePlacesWatchedForPreviousAccount = true }
        }
        applyMonitoring()
        pauseDidChange()
        Task { await flush(isForced: true) }
    }

    /// An account was removed from the phone: what it had not sent can never
    /// be sent, so its outbox file goes.
    func accountWasRemoved(accountKey: String) {
        if outbox.accountKey == accountKey {
            outbox = LocationOutbox()
            unsentCount = 0
        }
        if let url = outboxURL(accountKey: accountKey) { try? FileManager.default.removeItem(at: url) }
        defaults.removeObject(forKey: LocationPauseTracker.defaultsKey(accountKey: accountKey))
    }

    /// Stops sending, and waits for a batch on its way to be answered. Used
    /// around erasing the history: nothing is sent between the person
    /// pressing Erase and the server having erased.
    func holdReporting() async {
        isReportingHeld = true
        while isFlushing { try? await Task.sleep(for: .milliseconds(100)) }
    }

    /// Lets sending go on. After an erase that worked, what was noticed in
    /// the erased range and not yet sent goes with it (with no range, all of
    /// it); after one that did not, nothing is lost.
    func releaseReporting(discardingFrom from: Date?, until: Date?, isErased: Bool) {
        isReportingHeld = false
        if isErased {
            outbox.discardUnsent(from: from, until: until)
            saveOutbox(isUrgent: true)
        }
        Task { await flush(isForced: true) }
    }

    // MARK: What the person chose

    /// The switch "Record where I go".
    func setRecording(_ isOn: Bool) {
        defaults.set(isOn, forKey: Self.isRecordingOnKey)
        isRecordingOn = isOn
        if isOn {
            // Turning it on again is how a refusal is tried again.
            refusalText = nil
            retryGate = LocationRetryGate()
            askForAuthorizationIfUndecided()
        }
        applyMonitoring()
        startLiveUpdates()
        if !isOn, viewerCount == 0 { stopLiveUpdates() }
    }

    // MARK: Pausing

    /// The person paused on the phone, for the length they chose. It applies at
    /// once, and the server is told until it has it.
    func pauseRecording(_ choice: LocationPauseChoice) {
        let now = Date()
        pauseRefusalText = nil
        pauseTracker.startPause(until: choice.endDate(from: now), at: now)
        pauseDidChange(isSendForced: true)
    }

    /// The person resumed on the phone.
    func resumeRecording() {
        pauseRefusalText = nil
        pauseTracker.endPause(at: Date())
        pauseDidChange(isSendForced: true)
    }

    /// Says the refusal of a pause has been shown.
    func clearPauseRefusal() { pauseRefusalText = nil }

    /// The server said what pause is in effect for the person, from
    /// `GetLocation` or from the answer to a report. A change made on the
    /// phone that the server has not taken yet still applies over it.
    func learnPause(_ serverPause: LocationPause?) {
        var learned = pauseTracker
        learned.learn(serverPause, at: Date())
        guard learned != pauseTracker else { return }
        pauseTracker = learned
        // A pause made elsewhere reaches the phone after the fact: what it
        // noticed since the pause began waits in the outbox, and is not told.
        if let pause = learned.pause, learned.isPaused(at: Date()), let startedDate = pause.startedDate {
            outbox.discardUnsent(from: startedDate, until: pause.endedDate)
            saveOutbox(isUrgent: false)
        }
        pauseDidChange()
    }

    /// Asks the server for the person's pause: at launch and when the app
    /// comes to the foreground, since a pause may have been made or ended
    /// anywhere.
    private func refreshPauseFromServer() async {
        #if DEBUG
            if DemoServer.isRequested { return }
        #endif
        guard let connection = AppModel.shared.connection, connection.accountKey == outbox.accountKey,
            let own = try? await connection.location()
        else { return }
        // The account was changed while the call was on its way.
        guard connection.accountKey == outbox.accountKey else { return }
        learnPause(own.pause)
    }

    private func loadPauseTracker(accountKey: String?) {
        var loaded = LocationPauseTracker()
        if let accountKey, let kept = defaults.data(forKey: LocationPauseTracker.defaultsKey(accountKey: accountKey)),
            let decoded = try? LocationPauseTracker.decoded(from: kept)
        {
            loaded = decoded
        }
        pauseTracker = loaded
        pauseRefusalText = nil
    }

    /// Keeps the tracker in the defaults, so a closed app that iOS wakes for
    /// a visit still knows.
    private func keepPauseTracker() {
        guard let accountKey = outbox.accountKey else { return }
        let key = LocationPauseTracker.defaultsKey(accountKey: accountKey)
        if pauseTracker == LocationPauseTracker() {
            defaults.removeObject(forKey: key)
        } else if let encoded = try? pauseTracker.encoded() {
            defaults.set(encoded, forKey: key)
        }
    }

    /// Everything that follows from the pause having changed: it is kept, the
    /// screens read it again, a pause that begins ends the visits in progress
    /// and stops the live updates, one that ends starts them again, and a
    /// change made here is sent. `isSendForced` sends at once over a wait
    /// between two tries, for a change the person just made.
    private func pauseDidChange(isSendForced: Bool = false) {
        pauseTracker.settle(at: Date())
        keepPauseTracker()
        pauseRevision += 1
        if let pause = pauseTracker.pause, pauseTracker.isPaused(at: Date()), let startedDate = pause.startedDate {
            // The server ended the visit in progress where the pause began.
            endOpenVisits(at: startedDate)
        } else {
            pointFilter.reset()
        }
        startLiveUpdates()
        schedulePauseEnd()
        watchPlacesAgainAfterPause()
        sendPendingPauseChange(isForced: isSendForced)
    }

    /// After a pause the places watched are still satisfied for the person
    /// who stayed inside one, so iOS sends no new enter: they are given to
    /// iOS again, which says at once which the person is inside. Noted in the
    /// defaults while a pause is in effect, so that a pause which ended while
    /// the app was closed is still followed by it.
    private func watchPlacesAgainAfterPause() {
        guard let accountKey = outbox.accountKey else { return }
        let key = Self.hasPlacesToWatchAgainKeyPrefix + accountKey
        if pauseTracker.isPaused(at: Date()) {
            if !defaults.bool(forKey: key) { defaults.set(true, forKey: key) }
            return
        }
        guard defaults.bool(forKey: key) else { return }
        defaults.removeObject(forKey: key)
        Task { await giveWatchedPlacesToMonitorAgain() }
    }

    private func giveWatchedPlacesToMonitorAgain() async {
        guard manager != nil, isMonitoringWanted, !watchedPlaces.isEmpty else { return }
        let monitor = await ensurePlaceMonitor()
        for place in watchedPlaces.values {
            // Not an arrival iOS repeats for a visit in progress: the visits
            // were ended where the pause began, so this one is a new arrival.
            placeAddedAt[place.placeId] = nil
            await monitor.remove(place.placeId)
            let condition = CLMonitor.CircularGeographicCondition(
                center: CLLocationCoordinate2D(latitude: place.latitudeDegrees, longitude: place.longitudeDegrees), radius: place.radiusMeters)
            await monitor.add(condition, identifier: place.placeId, assuming: .unsatisfied)
        }
    }

    /// Reads the pause again at the moment a timed one ends: the live updates
    /// start again with no screen open or app launch to prompt it.
    private func schedulePauseEnd() {
        pauseEndTask?.cancel()
        pauseEndTask = nil
        guard let endDate = pauseTracker.endDate(at: Date()) else { return }
        pauseEndTask = Task { [weak self] in
            // A moment past the end, so that it has passed whatever the clock's rounding.
            try? await Task.sleep(for: .seconds(max(0, endDate.timeIntervalSinceNow) + 1))
            guard !Task.isCancelled else { return }
            self?.pauseDidChange()
        }
    }

    /// Tells the server of a pause or a resume made on the phone, again and
    /// again, a little longer apart each time, until it has it or refuses it.
    /// `isForced` starts at once, over a wait in progress.
    private func sendPendingPauseChange(isForced: Bool = false) {
        guard pauseTracker.hasPendingChange else { return }
        if pauseSendTask != nil {
            // A try in flight is let finish, and the next one sends what
            // changed meanwhile; only a wait between tries is cut short.
            guard isForced, isPauseSendWaiting else { return }
            pauseSendTask?.cancel()
        }
        let taskId = UUID()
        isPauseSendWaiting = false
        pauseSendTaskId = taskId
        pauseSendTask = Task { [weak self] in await self?.deliverPauseChanges(taskId: taskId) }
    }

    private func deliverPauseChanges(taskId: UUID) async {
        defer {
            if pauseSendTaskId == taskId {
                pauseSendTask = nil
                pauseSendTaskId = nil
            }
        }
        var failureCount = 0
        while !Task.isCancelled, pauseSendTaskId == taskId, let change = pauseTracker.pendingChange {
            // Not signed in yet, as at launch: the next foreground, report or
            // account change starts this again.
            guard let connection = AppModel.shared.connection, connection.accountKey == outbox.accountKey else { return }
            let accountKey = connection.accountKey
            do {
                let answer: LocationPause?
                switch change {
                case .pause(_, let endedAt):
                    answer = try await connection.pauseLocation(until: endedAt)
                case .resume:
                    try await connection.resumeLocation()
                    answer = nil
                }
                guard outbox.accountKey == accountKey else { return }
                pauseTracker.changeWasTaken(change, answer: answer, at: Date())
                pauseDidChange()
            } catch {
                guard !Task.isCancelled, outbox.accountKey == accountKey else { return }
                switch LocationPauseFailure.classify(error) {
                case .refused(let reason):
                    pauseTracker.changeWasRefused(change)
                    pauseRefusalText = reason
                    pauseDidChange()
                case .retryLater:
                    failureCount += 1
                    isPauseSendWaiting = true
                    try? await Task.sleep(for: .seconds(LocationPauseRetry.delaySeconds(afterFailureCount: failureCount)))
                    if pauseSendTaskId == taskId { isPauseSendWaiting = false }
                }
            }
        }
    }

    /// Asks for "While Using the App", the first thing the first time the
    /// person opens Location or turns the switch on.
    func askForAuthorizationIfUndecided() {
        #if DEBUG
            if DemoServer.isRequested { return }
        #endif
        if authorizationStatus == .notDetermined { manager?.requestWhenInUseAuthorization() }
    }

    /// Whether the screen that says why Always is wanted should be shown:
    /// the person allowed While Using the App, and has not been asked for
    /// Always, nor said no to the screen.
    var shouldExplainAlways: Bool {
        authorizationStatus == .authorizedWhenInUse && !defaults.bool(forKey: Self.hasAskedForAlwaysKey)
            && !defaults.bool(forKey: Self.hasDeclinedAlwaysKey)
    }

    /// Asks for Always, once the person has read why.
    func requestAlways() {
        defaults.set(true, forKey: Self.hasAskedForAlwaysKey)
        manager?.requestAlwaysAuthorization()
    }

    /// The person chose not to be asked for Always from the explaining screen.
    func declineAlwaysExplanation() {
        defaults.set(true, forKey: Self.hasDeclinedAlwaysKey)
    }

    /// Whether the person said "Not now" to setting Location up, or finished
    /// it, on this phone for this account. The one thing about the setup that
    /// is kept; what is done is worked out from the server and the permission.
    func isSetupDismissed(accountKey: String?) -> Bool {
        #if DEBUG
            // The screenshots run the same app again and again.
            if DemoServer.isRequested { return false }
        #endif
        return defaults.bool(forKey: Self.setupDismissedKey(accountKey))
    }

    func dismissSetup(accountKey: String?) {
        #if DEBUG
            if DemoServer.isRequested { return }
        #endif
        defaults.set(true, forKey: Self.setupDismissedKey(accountKey))
    }

    private static func setupDismissedKey(_ accountKey: String?) -> String {
        "locationSetupIsDismissed.\(accountKey ?? "")"
    }

    // MARK: Authorization

    func authorizationDidChange(_ newAuthorizationStatus: CLAuthorizationStatus) {
        authorizationStatus = newAuthorizationStatus
        // Until the person has chosen, recording follows whether Always is
        // allowed: on once it is.
        if defaults.object(forKey: Self.isRecordingOnKey) == nil { isRecordingOn = newAuthorizationStatus == .authorizedAlways }
        applyMonitoring()
        startServiceSession()
        startLiveUpdates()
        Task { await refreshPlaceWatch(isForced: true) }
    }

    // MARK: Monitoring

    /// Whether to listen for visits, significant changes and places while the
    /// app is closed.
    private var isMonitoringWanted: Bool {
        isRecordingOn && authorizationStatus == .authorizedAlways && AppModel.shared.account != nil && refusalText == nil
    }

    /// Starts or stops everything iOS wakes the app for.
    private func applyMonitoring() {
        guard let manager else { return }
        if isMonitoringWanted {
            manager.startMonitoringVisits()
            manager.startMonitoringSignificantLocationChanges()
            startServiceSession()
            Task {
                _ = await ensurePlaceMonitor()
                await refreshPlaceWatch(isForced: true)
            }
        } else {
            manager.stopMonitoringVisits()
            manager.stopMonitoringSignificantLocationChanges()
            serviceSession?.invalidate()
            serviceSession = nil
            // Nothing will say the person left a place once it is not
            // watched, and a visit left open hides every later arrival
            // there. Ended here, while the account is still known.
            endOpenVisits()
            Task { await stopWatchingPlaces() }
        }
    }

    private func startServiceSession() {
        serviceSession?.invalidate()
        serviceSession = isMonitoringWanted ? CLServiceSession(authorization: .always) : nil
    }

    // MARK: Following live

    // Never without the manager: the screenshots run with no manager and must
    // not make Core Location ask the person anything.
    private var isLiveWanted: Bool {
        manager != nil && isAppActive && isAuthorizedToLocate && AppModel.shared.account != nil
            && ((isRecordingOn && refusalText == nil && !isPaused) || viewerCount > 0)
    }

    private func startLiveUpdates() {
        guard isLiveWanted, liveTask == nil else {
            if !isLiveWanted { stopLiveUpdates() }
            return
        }
        let taskId = UUID()
        liveTaskId = taskId
        liveTask = Task { [weak self] in
            do {
                for try await update in CLLocationUpdate.liveUpdates() {
                    guard let self else { return }
                    if update.authorizationDenied || update.authorizationDeniedGlobally { break }
                    guard let location = update.location else { continue }
                    self.liveReadingArrived(LocationReading(location), isStationary: update.stationary)
                }
            } catch {
                // The stream ended; it starts again at the next foreground.
            }
            self?.liveTaskDidEnd(taskId)
        }
    }

    /// Forgets the task that followed the person once it has ended, unless a
    /// newer one has taken its place.
    private func liveTaskDidEnd(_ taskId: UUID) {
        guard liveTaskId == taskId else { return }
        liveTask = nil
        liveTaskId = nil
    }

    private func stopLiveUpdates() {
        liveTask?.cancel()
        liveTask = nil
        liveTaskId = nil
    }

    private func liveReadingArrived(_ reading: LocationReading, isStationary: Bool) {
        livePosition = LivePosition(
            latitudeDegrees: reading.latitudeDegrees, longitudeDegrees: reading.longitudeDegrees, accuracyMeters: reading.accuracyMeters,
            measuredAt: reading.measuredAt)
        guard isRecordingOn, pauseTracker.isRecordable(pointMeasuredAt: reading.measuredAt) else { return }
        guard
            pointFilter.shouldKeep(
                latitudeDegrees: reading.latitudeDegrees, longitudeDegrees: reading.longitudeDegrees,
                speedMetersPerSecond: reading.speedMetersPerSecond, measuredAt: reading.measuredAt, isStationary: isStationary)
        else { return }
        enqueue(isUrgent: false) { $0.addPoint(reading.reportedPoint) }
    }

    // MARK: What iOS wakes the app for

    func significantChangesArrived(_ readings: [LocationReading]) {
        guard isRecordingOn else { return }
        let recordable = readings.filter { pauseTracker.isRecordable(pointMeasuredAt: $0.measuredAt) }
        if !recordable.isEmpty {
            enqueue(isUrgent: false) { outbox in
                for reading in recordable { outbox.addPoint(reading.reportedPoint) }
            }
        }
        Task { await refreshPlaceWatch() }
    }

    func visitArrived(_ reading: VisitReading) {
        guard isRecordingOn, pauseTracker.isRecordable(visitArrivedAt: reading.arrivedAt, departedAt: reading.departedAt) else { return }
        enqueue(isUrgent: true) { $0.addVisit(reading.reportedVisit) }
    }

    // MARK: Places the person named

    /// Watches the person's labeled and named places nearest to where they
    /// last were: iOS allows twenty at a time. Entering one becomes a visit
    /// reported with its place, and leaving it the same visit with its end.
    private func refreshPlaceWatch(isForced: Bool = false) async {
        guard manager != nil, isMonitoringWanted else { return }
        if arePlacesWatchedForPreviousAccount { await stopWatchingPlaces() }
        guard let connection = AppModel.shared.connection else { return }
        if !isForced, Date().timeIntervalSince(placeWatchRefreshedAt) < Self.placeWatchRefreshSeconds { return }
        placeWatchRefreshedAt = Date()
        var places: [LocationPlace] = []
        do {
            var offset = 0
            // Up to a thousand places: the ones that matter are the person's
            // own, which come among them.
            for _ in 0..<5 {
                let page = try await connection.locationPlaces(offset: offset, limitCount: 200)
                places += page.places
                guard let next = page.nextOffset else { break }
                offset = next
            }
        } catch {
            // Keep watching what is watched; the next change tries again.
            return
        }
        let anchor = livePosition?.coordinate ?? manager?.location?.coordinate
        let chosen = LocationPlaceWatch.choose(
            from: places, nearLatitudeDegrees: anchor?.latitude, nearLongitudeDegrees: anchor?.longitude)
        await watch(chosen)
        arePlacesWatchedForPreviousAccount = false
    }

    /// The one monitor of the person's places. It is made as soon as the app
    /// launches, before anything is read from the server: iOS relaunches a
    /// closed app for a place being entered or left, and delivers the event
    /// to a monitor of the same name that is made and listened to then.
    private func ensurePlaceMonitor() async -> CLMonitor {
        if let monitorStartup { return await monitorStartup.value }
        let startup = Task { await CLMonitor("places") }
        monitorStartup = startup
        let monitor = await startup.value
        if placeMonitorTask == nil {
            placeMonitorTask = Task { [weak self] in
                do {
                    for try await event in await monitor.events {
                        let isInside: Bool?
                        switch event.state {
                        case .satisfied: isInside = true
                        case .unsatisfied: isInside = false
                        default: isInside = nil
                        }
                        guard let isInside else { continue }
                        await self?.placeEventArrived(identifier: event.identifier, isInside: isInside, date: event.date)
                    }
                } catch {
                    // Starts again with the next launch.
                }
            }
        }
        return monitor
    }

    private func watch(_ chosen: [WatchedPlace]) async {
        let monitor = await ensurePlaceMonitor()
        let wanted = Dictionary(chosen.map { ($0.placeId, $0) }, uniquingKeysWith: { first, _ in first })
        for identifier in await monitor.identifiers where wanted[identifier] == nil {
            await monitor.remove(identifier)
            watchedPlaces[identifier] = nil
            // Nothing will say the person left a place no longer watched.
            closeOpenVisit(placeId: identifier, at: Date())
        }
        for place in chosen {
            if let record = await monitor.record(for: place.placeId),
                let condition = record.condition as? CLMonitor.CircularGeographicCondition,
                condition.center.latitude == place.latitudeDegrees, condition.center.longitude == place.longitudeDegrees,
                condition.radius == place.radiusMeters
            {
                watchedPlaces[place.placeId] = place
                continue
            }
            let condition = CLMonitor.CircularGeographicCondition(
                center: CLLocationCoordinate2D(latitude: place.latitudeDegrees, longitude: place.longitudeDegrees), radius: place.radiusMeters)
            await monitor.add(condition, identifier: place.placeId, assuming: .unsatisfied)
            placeAddedAt[place.placeId] = Date()
            watchedPlaces[place.placeId] = place
        }
    }

    private func stopWatchingPlaces() async {
        guard let monitorStartup else { return }
        let monitor = await monitorStartup.value
        for identifier in await monitor.identifiers { await monitor.remove(identifier) }
        watchedPlaces = [:]
    }

    private func placeEventArrived(identifier: String, isInside: Bool, date: Date) async {
        guard isRecordingOn else { return }
        if isInside {
            var place = watchedPlaces[identifier]
            if place == nil, let record = await ensurePlaceMonitor().record(for: identifier),
                let condition = record.condition as? CLMonitor.CircularGeographicCondition
            {
                // Relaunched for the event, before the places were read again.
                place = WatchedPlace(
                    placeId: identifier, latitudeDegrees: condition.center.latitude, longitudeDegrees: condition.center.longitude,
                    radiusMeters: condition.radius)
            }
            guard let place else { return }
            // Nothing of a pause is recorded, and nothing begins in it.
            guard !pauseTracker.isPaused(at: date) else { return }
            // Told at once about a place just watched again while the visit
            // to it goes on: not an arrival.
            if outbox.openRegionVisits[identifier] != nil, let addedAt = placeAddedAt[identifier],
                abs(date.timeIntervalSince(addedAt)) < Self.placeAddedEchoSeconds
            {
                return
            }
            enqueue(isUrgent: true) {
                $0.arrive(placeId: place.placeId, latitudeDegrees: place.latitudeDegrees, longitudeDegrees: place.longitudeDegrees, at: date)
            }
        } else {
            closeOpenVisit(placeId: identifier, at: date)
        }
    }

    private func closeOpenVisit(placeId: String, at date: Date) {
        guard outbox.openRegionVisits[placeId] != nil else { return }
        // Left in a pause: the server ended the visit where the pause began.
        let departureDate = pauseTracker.departureDate(noticedAt: date)
        enqueue(isUrgent: true) { $0.depart(placeId: placeId, at: departureDate) }
    }

    /// Ends every visit to a place that is still open, in the outbox of the
    /// account in use. Not held back by a refusal: the visit was begun, so
    /// its end is recorded too.
    private func endOpenVisits(at date: Date = Date()) {
        guard outbox.accountKey != nil, !outbox.openRegionVisits.isEmpty else { return }
        outbox.departEverywhere(at: pauseTracker.departureDate(noticedAt: date))
        saveOutbox(isUrgent: true)
        Task { await flush() }
    }

    // MARK: The outbox

    /// Changes the outbox, keeps it in its file, and sends it. What was
    /// noticed under another account is not reported as the one signed in.
    /// While the server is refusing what the phone notices, nothing new is
    /// kept.
    private func enqueue<Result>(isUrgent: Bool, _ change: (inout LocationOutbox) -> Result) {
        guard refusalText == nil, let accountKey = AppModel.shared.account?.accountKey else { return }
        if outbox.accountKey != accountKey { useOutbox(accountKey: accountKey) }
        _ = change(&outbox)
        saveOutbox(isUrgent: isUrgent)
        sendPendingPauseChange()
        Task { await flush() }
    }

    private func outboxURL(accountKey: String) -> URL? {
        outboxDirectory?.appendingPathComponent(LocationOutbox.fileName(accountKey: accountKey))
    }

    /// Puts the outbox of another account (or of none) in use. The one in use
    /// is kept in its file first, with the visits it had open ended: the
    /// phone does not record for it any more.
    private func useOutbox(accountKey: String?) {
        if outbox.accountKey != nil {
            outbox.departEverywhere(at: Date())
            // The departures are new: the file must get them even if the
            // outbox was written after its last change.
            isOutboxUnwritten = true
            writeOutbox()
        }
        var next = LocationOutbox()
        if let accountKey {
            if let url = outboxURL(accountKey: accountKey),
                let kept = (try? Data(contentsOf: url)).flatMap({ try? LocationOutbox.decoded(from: $0) })
            {
                next = kept
            } else if let legacyURL = outboxDirectory?.appendingPathComponent("outbox.json"),
                let legacy = (try? Data(contentsOf: legacyURL)).flatMap({ try? LocationOutbox.decoded(from: $0) }), legacy.accountKey == accountKey
            {
                // The single file kept before there was one for each account.
                next = legacy
                try? FileManager.default.removeItem(at: legacyURL)
                isOutboxUnwritten = true
            }
            next.accountKey = accountKey
        }
        outbox = next
        unsentCount = next.points.count + next.visits.count
        if isOutboxUnwritten { writeOutbox() }
    }

    /// Notes that the outbox changed, and writes it: at once when the change
    /// is urgent (a visit), else at most once a second.
    private func saveOutbox(isUrgent: Bool) {
        unsentCount = outbox.points.count + outbox.visits.count
        isOutboxUnwritten = true
        if isUrgent || Date().timeIntervalSince(outboxWrittenAt) >= Self.outboxWriteSeconds {
            writeOutbox()
            return
        }
        guard outboxWriteTask == nil else { return }
        outboxWriteTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(Self.outboxWriteSeconds))
            guard !Task.isCancelled else { return }
            self?.outboxWriteTask = nil
            self?.writeOutbox()
        }
    }

    /// Writes the outbox to the file of its account, if it changed since.
    private func writeOutbox() {
        outboxWriteTask?.cancel()
        outboxWriteTask = nil
        guard isOutboxUnwritten else { return }
        isOutboxUnwritten = false
        outboxWrittenAt = Date()
        guard let accountKey = outbox.accountKey, let url = outboxURL(accountKey: accountKey), let outboxData = try? outbox.encoded() else { return }
        // Readable after the first unlock, so a phone relaunched the moment
        // an app is woken in a pocket can still keep what it noticed.
        try? outboxData.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }

    /// Sends what waits, a batch at a time, until the outbox is empty or a
    /// call fails. A batch leaves the outbox only after the server answered:
    /// no connection or a refused sign-in keeps it for the next try, which is
    /// the next return to the foreground or leaving it, the next launch and,
    /// after the wait that follows a failure, the next event. `isForced`
    /// skips that wait.
    ///
    /// Only the account in use is sent, over its own connection: the check
    /// and the batch are taken with no suspension between them, and a batch
    /// is taken out only if the outbox is still that account's.
    func flush(isForced: Bool = false) async {
        if isFlushing {
            isFlushRequestedAgain = true
            return
        }
        guard refusalText == nil, !isReportingHeld, !outbox.isEmpty, isForced || retryGate.canTry(at: Date()) else { return }
        guard let connection = AppModel.shared.connection, connection.accountKey == outbox.accountKey else { return }
        isFlushing = true
        let background = BackgroundTaskHolder(name: "Report location")
        defer {
            isFlushing = false
            background.end()
            writeOutbox()
        }
        repeat {
            isFlushRequestedAgain = false
            guard !isReportingHeld, let connection = AppModel.shared.connection, connection.accountKey == outbox.accountKey else { return }
            let accountKey = connection.accountKey
            while outbox.accountKey == accountKey, AppModel.shared.connection === connection, !outbox.isEmpty, !isReportingHeld {
                let batch = outbox.nextBatch()
                do {
                    try await sendSplittingInvalid(batch, over: connection)
                } catch {
                    // The account was changed while the call was on its way:
                    // the answer says nothing about the new account's outbox,
                    // and the request that change queued is still wanted.
                    guard outbox.accountKey == accountKey else { break }
                    switch LocationReportFailure.classify(error) {
                    case .refused(let reason):
                        stopBecauseRefused(reason)
                        return
                    case .batchInvalid:
                        // sendSplittingInvalid drops the entry the server
                        // refuses and does not throw this; if it ever does,
                        // sending it again cannot change the answer.
                        outbox.remove(batch)
                        saveOutbox(isUrgent: false)
                        continue
                    case .retryLater, .phoneNotFound:
                        retryGate.didFail(at: Date())
                        return
                    }
                }
                retryGate.didSucceed()
                // The account was changed while the call was on its way: the
                // batch stays in that account's file and is sent again, which
                // the server takes as the same points and visits.
                guard outbox.accountKey == accountKey else { break }
                outbox.remove(batch)
                saveOutbox(isUrgent: false)
            }
        } while isFlushRequestedAgain
    }

    /// Sends a batch. If the server calls it invalid without saying which
    /// entry is wrong, it is sent again in two smaller batches, down to the
    /// single entry the server refuses on its own, which is dropped: one bad
    /// entry must not take the visits sent beside it.
    private func sendSplittingInvalid(_ batch: LocationBatch, over connection: Connection) async throws {
        do {
            try await send(batch, over: connection)
        } catch {
            guard case .batchInvalid = LocationReportFailure.classify(error) else { throw error }
            guard let halves = batch.split() else { return }
            try await sendSplittingInvalid(halves.first, over: connection)
            try await sendSplittingInvalid(halves.second, over: connection)
        }
    }

    /// Sends one batch. A phone the server does not know (the person deleted
    /// it) is left out of the call and the batch sent again, since the phone
    /// only tells the agent which device the position came from.
    private func send(_ batch: LocationBatch, over connection: Connection) async throws {
        let phoneId = AppModel.shared.phone?.id
        let result: ReportLocationResult
        do {
            result = try await connection.reportLocation(batch, agentPhoneId: phoneId)
        } catch {
            guard phoneId != nil, LocationReportFailure.classify(error) == .phoneNotFound else { throw error }
            result = try await connection.reportLocation(batch, agentPhoneId: nil)
        }
        // A pause made or ended somewhere else reaches the phone with the answer.
        if outbox.accountKey == connection.accountKey { learnPause(result.pause) }
    }

    /// The server will not take what the phone notices, and asking again
    /// cannot change that: say why, and stop listening for more.
    private func stopBecauseRefused(_ reason: String) {
        refusalText = reason
        applyMonitoring()
    }
}
