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
        let status = manager.authorizationStatus
        Task { @MainActor in LocationRecorder.shared.authorizationDidChange(status) }
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
/// it to the server (docs/planning/location-execplan.md, Milestone 2).
///
/// While the app is open it follows the person live. While it is closed it
/// relies on what iOS wakes it for: a visit (arriving somewhere, leaving),
/// a significant change of location, and the person entering or leaving a
/// place they named or labeled. There is no continuous tracking in the
/// background and no `location` background mode.
///
/// Everything noticed goes into an outbox kept in a file (LocationOutbox),
/// and leaves it only after the server answered.
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
    @ObservationIgnored private let delegate = LocationManagerDelegate()
    @ObservationIgnored private var serviceSession: CLServiceSession?
    @ObservationIgnored private var liveTask: Task<Void, Never>?
    @ObservationIgnored private var liveTaskId: UUID?
    @ObservationIgnored private var pointFilter = LocationPointFilter()
    @ObservationIgnored private var isAppActive = false
    /// How many screens that show the person's position are open, which
    /// keeps the live position coming with the switch off.
    @ObservationIgnored private var viewerCount = 0
    @ObservationIgnored private var outbox: LocationOutbox
    @ObservationIgnored private var isFlushing = false
    @ObservationIgnored private var isFlushRequestedAgain = false
    @ObservationIgnored private var monitorStartup: Task<CLMonitor, Never>?
    @ObservationIgnored private var placeMonitorTask: Task<Void, Never>?
    @ObservationIgnored private var watchedPlaces: [String: WatchedPlace] = [:]
    @ObservationIgnored private var placeWatchRefreshedAt = Date.distantPast
    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let outboxURL: URL?

    private static let isRecordingOnKey = "locationIsRecordingOn"
    private static let hasAskedForAlwaysKey = "locationHasAskedForAlways"
    private static let hasDeclinedAlwaysKey = "locationHasDeclinedAlways"
    /// How often the watched places are read again from the server because
    /// the person moved a long way: no more than this.
    private static let placeWatchRefreshSeconds = 60.0

    private init() {
        let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String
        defaults = group.flatMap { UserDefaults(suiteName: $0) } ?? .standard
        let fileManager = FileManager.default
        let container =
            group.flatMap { fileManager.containerURL(forSecurityApplicationGroupIdentifier: $0) }
            ?? fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
        let directory = container?.appendingPathComponent("Location", isDirectory: true)
        if let directory { try? fileManager.createDirectory(at: directory, withIntermediateDirectories: true) }
        outboxURL = directory?.appendingPathComponent("outbox.json")
        let kept = outboxURL.flatMap { try? Data(contentsOf: $0) }.flatMap { try? LocationOutbox.decoded(from: $0) } ?? LocationOutbox()
        outbox = kept
        unsentCount = kept.points.count + kept.visits.count
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

    /// The app came to the foreground or went to the background.
    func sceneDidChange(isActive: Bool) {
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
            Task {
                await flush()
                await refreshPlaceWatch()
            }
        } else {
            stopLiveUpdates()
        }
    }

    /// A screen that shows the person's position appeared or went away.
    func viewerAppeared() {
        viewerCount += 1
        startLiveUpdates()
    }

    func viewerDisappeared() {
        viewerCount = max(0, viewerCount - 1)
        if viewerCount == 0, !isRecordingOn { stopLiveUpdates() }
    }

    /// The account in use changed or was signed out of. What was noticed
    /// belongs to the person who was signed in.
    func accountDidChange(accountKey: String?) {
        // Told by the model while it is being made, so it does not ask the
        // model which account is in use.
        if outbox.accountKey != accountKey, outbox.accountKey != nil || !outbox.isEmpty {
            outbox = LocationOutbox()
            saveOutbox()
        }
        applyMonitoring()
        Task { await flush() }
    }

    // MARK: What the person chose

    /// The switch "Record where I go".
    func setRecording(_ isOn: Bool) {
        defaults.set(isOn, forKey: Self.isRecordingOnKey)
        isRecordingOn = isOn
        if isOn { askForAuthorizationIfUndecided() }
        applyMonitoring()
        startLiveUpdates()
        if !isOn, viewerCount == 0 { stopLiveUpdates() }
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

    // MARK: Authorization

    func authorizationDidChange(_ status: CLAuthorizationStatus) {
        authorizationStatus = status
        // Until the person has chosen, recording follows whether Always is
        // allowed: on once it is.
        if defaults.object(forKey: Self.isRecordingOnKey) == nil { isRecordingOn = status == .authorizedAlways }
        applyMonitoring()
        startServiceSession()
        startLiveUpdates()
        Task { await refreshPlaceWatch(isForced: true) }
    }

    // MARK: Monitoring

    /// Whether to listen for visits, significant changes and places while the
    /// app is closed.
    private var isMonitoringWanted: Bool {
        isRecordingOn && authorizationStatus == .authorizedAlways && AppModel.shared.account != nil
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
            Task { await stopWatchingPlaces() }
        }
    }

    private func startServiceSession() {
        serviceSession?.invalidate()
        serviceSession = isMonitoringWanted ? CLServiceSession(authorization: .always) : nil
    }

    // MARK: Following live

    private var isLiveWanted: Bool {
        isAppActive && isAuthorizedToLocate && AppModel.shared.account != nil && (isRecordingOn || viewerCount > 0)
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
        guard isRecordingOn else { return }
        guard
            pointFilter.shouldKeep(
                latitudeDegrees: reading.latitudeDegrees, longitudeDegrees: reading.longitudeDegrees,
                speedMetersPerSecond: reading.speedMetersPerSecond, measuredAt: reading.measuredAt, isStationary: isStationary)
        else { return }
        enqueue { $0.addPoint(reading.reportedPoint) }
    }

    // MARK: What iOS wakes the app for

    func significantChangesArrived(_ readings: [LocationReading]) {
        guard isRecordingOn else { return }
        enqueue { outbox in
            for reading in readings { outbox.addPoint(reading.reportedPoint) }
        }
        Task { await refreshPlaceWatch() }
    }

    func visitArrived(_ reading: VisitReading) {
        guard isRecordingOn else { return }
        enqueue { $0.addVisit(reading.reportedVisit) }
    }

    // MARK: Places the person named

    /// Watches the person's labeled and named places nearest to where they
    /// last were: iOS allows twenty at a time. Entering one becomes a visit
    /// reported with its place, and leaving it the same visit with its end.
    private func refreshPlaceWatch(isForced: Bool = false) async {
        guard isMonitoringWanted, let connection = AppModel.shared.connection else { return }
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
            enqueue { $0.arrive(placeId: place.placeId, latitudeDegrees: place.latitudeDegrees, longitudeDegrees: place.longitudeDegrees, at: date) }
        } else {
            closeOpenVisit(placeId: identifier, at: date)
        }
    }

    private func closeOpenVisit(placeId: String, at date: Date) {
        guard outbox.openRegionVisits[placeId] != nil else { return }
        enqueue { $0.depart(placeId: placeId, at: date) }
    }

    // MARK: The outbox

    /// Changes the outbox, keeps it in its file, and sends it. What was
    /// noticed under another account is not reported as the one signed in.
    private func enqueue<Result>(_ change: (inout LocationOutbox) -> Result) {
        guard let accountKey = AppModel.shared.account?.accountKey else { return }
        if outbox.accountKey != accountKey { outbox = LocationOutbox(accountKey: accountKey) }
        _ = change(&outbox)
        saveOutbox()
        Task { await flush() }
    }

    private func saveOutbox() {
        unsentCount = outbox.points.count + outbox.visits.count
        guard let outboxURL, let data = try? outbox.encoded() else { return }
        // Readable after the first unlock, so a phone relaunched the moment
        // an app is woken in a pocket can still keep what it noticed.
        try? data.write(to: outboxURL, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
    }

    /// Sends what waits, a batch at a time, until the outbox is empty or a
    /// call fails. A batch leaves the outbox only after the server answered:
    /// no connection or a refused sign-in keeps it for the next try, which is
    /// the next event, the next launch and the next return to the foreground.
    func flush() async {
        if isFlushing {
            isFlushRequestedAgain = true
            return
        }
        guard !outbox.isEmpty, let connection = AppModel.shared.connection, connection.accountKey == outbox.accountKey else { return }
        isFlushing = true
        let background = BackgroundTaskHolder(name: "Report location")
        defer {
            isFlushing = false
            background.end()
        }
        repeat {
            isFlushRequestedAgain = false
            while !outbox.isEmpty {
                let batch = outbox.nextBatch()
                do {
                    _ = try await connection.reportLocation(batch, agentPhoneId: AppModel.shared.phone?.id)
                } catch {
                    return
                }
                outbox.remove(batch)
                saveOutbox()
            }
        } while isFlushRequestedAgain
    }
}
