import CoreLocation
import Foundation
import Observation
import TeaNodeKit

/// LocationModel is the Location screen: where the person is, the visits
/// they have made (newest first, a page at a time) and the places those
/// visits were at.
@MainActor
@Observable
final class LocationModel {
    nonisolated static let visitPageSize = 50
    nonisolated static let placePageSize = 50
    nonisolated static let searchPageSize = 20
    /// How many address matches one search lists.
    nonisolated static let addressSearchLimit = 8

    let connection: Connection

    private(set) var location: PersonLocation?
    private(set) var visits: [PlaceVisit] = []
    private(set) var nextVisitOffset: Int?
    private(set) var places: [LocationPlace] = []
    private(set) var nextPlaceOffset: Int?
    /// Whether the first read has finished, one way or the other.
    private(set) var hasLoaded = false
    private(set) var isLoading = false
    private(set) var isLoadingMoreVisits = false
    private(set) var isLoadingMorePlaces = false
    /// Why the first read failed, when there is nothing to show instead.
    private(set) var loadFailedText: String?
    var problemText: String?
    var doneText: String?

    // MARK: What the lists are filtered by

    /// Only places with this label, when one is chosen.
    private(set) var placeLabelFilter: PlaceLabel?
    /// Words the places' names are searched for, as typed.
    var placeSearchText = ""
    /// The words the places shown were read for.
    private var readPlaceQuery = ""

    // MARK: People sharing, the person's own shares and the setup

    /// The people sharing their location with the person, as the server last
    /// said.
    private(set) var people: [PersonLocation] = []
    /// The shares of the person's own location.
    private(set) var shares: [LocationShare] = []
    /// The circles the person is in, for the setup and the share sheet.
    private(set) var circles: [PeopleCircle] = []
    /// The person's places labeled home, for the setup.
    private(set) var homePlaces: [LocationPlace] = []
    /// Whether the shares and the home places were both read, which is what
    /// the setup is worked out from: a failed read must not end it.
    private(set) var isSetupKnown = false
    /// Why the shares could not be read, said on the Sharing list.
    private(set) var sharingProblemText: String?
    /// Whether the person said "Not now" to the setup, or finished it.
    private(set) var isSetupDismissed: Bool

    // MARK: The person selected on the map

    /// Who the map is centered on: somebody sharing, or nobody for the person's
    /// own. Their visits show in the drawer, under their name.
    private(set) var selectedUserId: String?

    init(connection: Connection) {
        self.connection = connection
        isSetupDismissed = LocationRecorder.shared.isSetupDismissed(accountKey: AppModel.shared.account?.accountKey)
    }

    /// How many times the person asked to see their own position on the map;
    /// the map centers on it each time this changes.
    private(set) var ownFocusCount = 0

    /// Goes back to the person's own location and centers the map on it, even
    /// when it was already the one shown.
    func focusOnOwn() {
        clearSelection()
        ownFocusCount += 1
    }

    /// The selected person, once the server has said they share.
    var selectedPerson: PersonLocation? { people.first { $0.userId == selectedUserId } }

    /// How far setting Location up has got.
    var setup: LocationSetup {
        LocationSetup(isPermissionDone: LocationRecorder.shared.isAuthorizedAlways, homePlaces: homePlaces, circles: circles, shares: shares)
    }

    /// Whether the setup is shown in place of the lists.
    var isSetupShown: Bool {
        hasLoaded && isSetupKnown && location != nil && setup.isShown(isDismissed: isSetupDismissed, hasLatestPoint: location?.latestPoint != nil)
    }

    /// The person's own user identifier, once their location is read.
    var ownUserId: String? { location?.userId }

    /// The visits grouped by day, newest first.
    var days: [LocationVisitDay] { LocationDays.group(visits) }

    /// Whether the places shown are narrowed by a label or by words.
    var isFilteringPlaces: Bool { placeLabelFilter != nil || !trimmedPlaceSearchText.isEmpty }

    private var trimmedPlaceSearchText: String { placeSearchText.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// The places in the order the server gives them: labeled first, then
    /// named by the person, then by the last visit.
    var sortedPlaces: [LocationPlace] { places }

    /// Where the map is centered when nothing says better: the latest point
    /// the server has.
    var latestCoordinate: CLLocationCoordinate2D? {
        guard let point = location?.latestPoint else { return nil }
        return CLLocationCoordinate2D(latitude: point.latitudeDegrees, longitude: point.longitudeDegrees)
    }

    // MARK: Reading

    /// Reads where the person is, the first page of visits and of places.
    func load() async {
        guard !isLoading else { return }
        isLoading = true
        defer {
            isLoading = false
            hasLoaded = true
        }
        let connection = self.connection
        let placeLabel = placeLabelFilter?.rawValue
        let placeQuery = trimmedPlaceSearchText
        async let readLocation = attempt { try await connection.location() }
        async let readVisits = attempt { try await connection.locationVisits(limitCount: LocationModel.visitPageSize) }
        async let readPlaces = attempt {
            try await connection.locationPlaces(placeLabel: placeLabel, query: placeQuery, limitCount: LocationModel.placePageSize)
        }
        async let readPeople = attempt { try await connection.sharedLocations() }
        async let readShares = attempt { try await connection.locationShares() }
        async let readCircles = attempt { try await connection.circles() }
        async let readHomes = attempt { try await connection.locationPlaces(placeLabel: PlaceLabel.home.rawValue, limitCount: 1) }
        let (locationResult, visitsResult, placesResult) = await (readLocation, readVisits, readPlaces)
        let (peopleResult, sharesResult, circlesResult, homesResult) = await (readPeople, readShares, readCircles, readHomes)
        apply(people: peopleResult, shares: sharesResult, circles: circlesResult, homes: homesResult)
        var failure: Error?
        switch locationResult {
        case .success(let read): location = read
        case .failure(let error): failure = error
        }
        switch visitsResult {
        case .success(let page):
            visits = page.placeVisits
            nextVisitOffset = page.nextOffset
        case .failure(let error): failure = failure ?? error
        }
        switch placesResult {
        case .success(let page):
            places = page.places
            nextPlaceOffset = page.nextOffset
            readPlaceQuery = placeQuery
        case .failure(let error): failure = failure ?? error
        }
        if let failure {
            // Said in a toast when there is something to show, and as the
            // whole screen's problem when there is not.
            if location == nil && visits.isEmpty && places.isEmpty {
                loadFailedText = failure.loadProblemText
            } else {
                problemText = failure.localizedDescription
            }
        } else {
            loadFailedText = nil
        }
        finishSetupIfDone()
    }

    private func apply(
        people peopleResult: Result<[PersonLocation], Error>, shares sharesResult: Result<[LocationShare], Error>,
        circles circlesResult: Result<[PeopleCircle], Error>, homes homesResult: Result<PlacePage, Error>
    ) {
        if case .success(let read) = peopleResult {
            people = read
            // Somebody who stopped sharing is no longer selected.
            if let selectedUserId, !read.contains(where: { $0.userId == selectedUserId }) { clearSelection() }
        }
        // A person with no circle permission has no circles; that is not a failure.
        circles = (try? circlesResult.get()) ?? []
        switch (sharesResult, homesResult) {
        case (.success(let readShares), .success(let page)):
            shares = readShares
            homePlaces = page.places
            isSetupKnown = true
            sharingProblemText = nil
        case (.failure(let error), _), (_, .failure(let error)):
            sharingProblemText = error.loadProblemText
        }
    }

    /// The next page of visits, once the last one read comes into view.
    func loadMoreVisits() async {
        guard let offset = nextVisitOffset, !isLoadingMoreVisits else { return }
        isLoadingMoreVisits = true
        defer { isLoadingMoreVisits = false }
        do {
            let page = try await connection.locationVisits(offset: offset, limitCount: Self.visitPageSize)
            let known = Set(visits.map(\.id))
            visits += page.placeVisits.filter { !known.contains($0.id) }
            nextVisitOffset = page.nextOffset
        } catch {
            problemText = error.localizedDescription
        }
    }

    func loadMorePlaces() async {
        guard let offset = nextPlaceOffset, !isLoadingMorePlaces else { return }
        isLoadingMorePlaces = true
        defer { isLoadingMorePlaces = false }
        do {
            let page = try await connection.locationPlaces(
                placeLabel: placeLabelFilter?.rawValue, query: readPlaceQuery, offset: offset, limitCount: Self.placePageSize)
            let known = Set(places.map(\.id))
            places += page.places.filter { !known.contains($0.id) }
            nextPlaceOffset = page.nextOffset
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Shows only places with this label, or all of them.
    func setPlaceLabelFilter(_ label: PlaceLabel?) async {
        guard label != placeLabelFilter else { return }
        placeLabelFilter = label
        await reloadPlaces()
    }

    /// Reads the first page of places again, for the label and the words
    /// typed; nothing is read when they are what the places shown were read
    /// for.
    func reloadPlaces() async {
        let query = trimmedPlaceSearchText
        do {
            let page = try await connection.locationPlaces(
                placeLabel: placeLabelFilter?.rawValue, query: query, limitCount: Self.placePageSize)
            // The words changed while this was read.
            guard query == trimmedPlaceSearchText else { return }
            places = page.places
            nextPlaceOffset = page.nextOffset
            readPlaceQuery = query
        } catch {
            problemText = error.localizedDescription
        }
    }

    /// Whether the words typed are not what the places shown were read for.
    var isPlaceSearchPending: Bool { trimmedPlaceSearchText != readPlaceQuery }

    /// The points of one day, for drawing the path.
    func points(on day: Date) async throws -> [LocationPoint] {
        let window = LocationDays.window(of: day)
        return try await connection.locationPoints(from: window.from, until: window.until)
    }

    /// The visits to one place, newest first, a page at a time.
    func visits(atPlace placeId: String, offset: Int = 0) async throws -> PlaceVisitPage {
        try await connection.locationVisits(placeId: placeId, offset: offset, limitCount: Self.visitPageSize)
    }

    // MARK: Changing

    /// Names, labels or resizes a place. The place is told to the lists that
    /// show it, and said plainly when the server refused.
    @discardableResult
    func save(_ change: PlaceChange) async -> LocationPlace? {
        do {
            let saved = try await connection.savePlace(change)
            replace(saved)
            LocationRecorder.shared.placesDidChange()
            return saved
        } catch {
            problemText = error.localizedDescription
            return nil
        }
    }

    /// Moves a place to an address the person chose, or makes the place there
    /// (labeled, when the change says so). The reason is returned, not
    /// toasted, for the address screen to say itself.
    func saveAddress(_ change: PlaceChange) async -> (saved: LocationPlace?, refusalText: String?) {
        do {
            let saved = try await connection.savePlace(change)
            replace(saved)
            LocationRecorder.shared.placesDidChange()
            return (saved, nil)
        } catch {
            return (nil, error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again.")))
        }
    }

    private func replace(_ saved: LocationPlace) {
        if let index = places.firstIndex(where: { $0.id == saved.id }) { places[index] = saved }
        visits = visits.map { visit in
            guard visit.place.id == saved.id else { return visit }
            return PlaceVisit(
                id: visit.id, place: saved, arrivedAt: visit.arrivedAt, departedAt: visit.departedAt, durationSeconds: visit.durationSeconds,
                latitudeDegrees: visit.latitudeDegrees, longitudeDegrees: visit.longitudeDegrees, accuracyMeters: visit.accuracyMeters,
                visitSource: visit.visitSource)
        }
        if let open = location?.openVisit, open.place.id == saved.id, let current = location {
            location = PersonLocation(
                userId: current.userId, username: current.username, name: current.name, avatarUrl: current.avatarUrl,
                avatarColorIndex: current.avatarColorIndex, shareLevel: current.shareLevel, latestPoint: current.latestPoint,
                openVisit: PlaceVisit(
                    id: open.id, place: saved, arrivedAt: open.arrivedAt, departedAt: open.departedAt, durationSeconds: open.durationSeconds,
                    latitudeDegrees: open.latitudeDegrees, longitudeDegrees: open.longitudeDegrees, accuracyMeters: open.accuracyMeters,
                    visitSource: open.visitSource))
        }
    }

    /// Deletes a place and its visits.
    @discardableResult
    func forget(_ place: LocationPlace) async -> Bool {
        do {
            try await connection.forgetPlace(placeId: place.id)
            places.removeAll { $0.id == place.id }
            visits.removeAll { $0.place.id == place.id }
            if location?.openVisit?.place.id == place.id { await load() }
            LocationRecorder.shared.placesDidChange()
            doneText = String(localized: "Forgot \(place.shownName)")
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    /// Where to check in: the phone's position now, from the recorder when it
    /// has a fresh one, else asked of the phone.
    func currentPosition() async -> (latitudeDegrees: Double, longitudeDegrees: Double)? {
        guard let reading = await currentReading() else { return nil }
        return (reading.latitudeDegrees, reading.longitudeDegrees)
    }

    /// The same position with how precise the phone says it is, in meters
    /// (negative when the phone could not tell).
    func currentReading() async -> (latitudeDegrees: Double, longitudeDegrees: Double, accuracyMeters: Double)? {
        if let live = LocationRecorder.shared.livePosition, Date().timeIntervalSince(live.measuredAt) < 60 {
            return (live.latitudeDegrees, live.longitudeDegrees, live.accuracyMeters)
        }
        let reading = await LocationReader.current()
        if let latitude = reading.latitudeDegrees, let longitude = reading.longitudeDegrees {
            return (latitude, longitude, reading.accuracyMeters ?? -1)
        }
        problemText = String(localized: "The phone could not tell where it is. Check that location is allowed for TeaNode, then try again.")
        return nil
    }

    func nearby(latitudeDegrees: Double, longitudeDegrees: Double) async throws -> [FoundPlace] {
        try await connection.nearbyPlaces(latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees)
    }

    /// Places matching words near a position, from OpenStreetMap, a page at
    /// a time.
    func searchPlaces(words: String, near position: (latitudeDegrees: Double, longitudeDegrees: Double), offset: Int = 0) async throws
        -> FoundPlacePage
    {
        try await connection.searchPlaces(
            query: words, latitudeDegrees: position.latitudeDegrees, longitudeDegrees: position.longitudeDegrees, offset: offset,
            limitCount: Self.searchPageSize)
    }

    /// Addresses matching the words typed, anywhere, nearest first to the
    /// place when there is one, else to where the person last was.
    func searchAddresses(words: String, placeId: String? = nil) async throws -> [FoundPlace] {
        try await connection.searchAddresses(query: words, placeId: placeId, limitCount: Self.addressSearchLimit)
    }

    /// Records that the person is at a place now. Nil when it worked, else
    /// the reason, for the sheet that asked to say.
    func checkIn(_ target: CheckInTarget, placeName: String?) async -> String? {
        do {
            try await connection.checkIn(target)
            doneText = placeName.map { String(localized: "Checked in at \($0)") } ?? String(localized: "Checked in here")
            LocationRecorder.shared.placesDidChange()
            await load()
            return nil
        } catch {
            return error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }

    /// Erases the positions and visits of a range (with no range, all of
    /// them), and the places that leaves with no visit that the person did
    /// not name or label. What the phone has noticed in that range and not
    /// yet sent goes too, and nothing is sent while the erase is on its way.
    @discardableResult
    func eraseHistory(from: Date? = nil, until: Date? = nil, dayText: String? = nil) async -> Bool {
        let recorder = LocationRecorder.shared
        await recorder.holdReporting()
        do {
            _ = try await connection.eraseLocationHistory(from: from, until: until)
        } catch {
            recorder.releaseReporting(discardingFrom: from, until: until, isErased: false)
            problemText = error.localizedDescription
            return false
        }
        recorder.releaseReporting(discardingFrom: from, until: until, isErased: true)
        recorder.placesDidChange()
        if from == nil, until == nil {
            location = nil
            visits = []
            places = []
            nextVisitOffset = nil
            nextPlaceOffset = nil
            doneText = String(localized: "Your positions and visits are erased")
        } else if let dayText {
            doneText = String(localized: "Erased the positions and visits of \(dayText)")
        }
        await load()
        return true
    }

    // MARK: Following people

    /// Keeps the screen current while it is visible: reads again whoever the
    /// server says moved, and everyone every thirty seconds in case a
    /// message was missed. Ends when the screen goes away.
    func follow() async {
        async let listening: Void = listen()
        async let polling: Void = poll()
        _ = await (listening, polling)
    }

    private static let pollInterval = Duration.seconds(30)

    private func poll() async {
        while !Task.isCancelled {
            try? await Task.sleep(for: Self.pollInterval)
            if Task.isCancelled { return }
            await refreshEveryone()
        }
    }

    private func listen() async {
        #if DEBUG
            // The demo has no socket to listen to.
            if DemoServer.isRequested { return }
        #endif
        var delay = Duration.seconds(1)
        while !Task.isCancelled {
            do {
                for try await event in LocationFeed.events(connection: connection) {
                    await refresh(userId: event.userId)
                    delay = .seconds(1)
                }
            } catch is CancellationError {
                return
            } catch {
                // The feed dropped; what was missed is read again below.
            }
            try? await Task.sleep(for: delay)
            if Task.isCancelled { return }
            delay = min(delay * 2, .seconds(30))
            await refreshEveryone()
        }
    }

    /// Reads the person who moved again: the person's own, or somebody
    /// sharing with them.
    func refresh(userId: String) async {
        if userId == ownUserId {
            await refreshOwn()
            return
        }
        do {
            let fresh = try await connection.location(userId: userId)
            guard let index = people.firstIndex(where: { $0.userId == userId }) else {
                // Somebody not listed yet, such as a share just made.
                await refreshPeople()
                return
            }
            people[index] = fresh
        } catch {
            // They may have stopped sharing; the list says so.
            await refreshPeople()
        }
    }

    /// The person's own position and newest visits, which a report from the
    /// phone changes.
    private func refreshOwn() async {
        let connection = self.connection
        async let readLocation = attempt { try await connection.location() }
        async let readVisits = attempt { try await connection.locationVisits(limitCount: LocationModel.visitPageSize) }
        let (locationResult, visitsResult) = await (readLocation, readVisits)
        if case .success(let read) = locationResult { location = read }
        if case .success(let page) = visitsResult {
            // The newest page in front of what was scrolled to, each visit once.
            let freshIds = Set(page.placeVisits.map(\.id))
            if visits.isEmpty { nextVisitOffset = page.nextOffset }
            visits = page.placeVisits + visits.filter { !freshIds.contains($0.id) }
        }
    }

    private func refreshPeople() async {
        guard let read = try? await connection.sharedLocations() else { return }
        people = read
        if let selectedUserId, !read.contains(where: { $0.userId == selectedUserId }) { clearSelection() }
    }

    private func refreshEveryone() async {
        await refreshOwn()
        await refreshPeople()
    }

    // MARK: The selected person

    /// Selects somebody sharing: the map centers on them. The person is in
    /// `people` once the server has said so; until then the selection waits
    /// for it.
    func select(userId: String) {
        guard userId != ownUserId else {
            clearSelection()
            return
        }
        selectedUserId = userId
    }

    /// Goes back to the person's own location.
    func clearSelection() {
        selectedUserId = nil
    }

    // MARK: Sharing

    /// The words for a thing shared with: a person's name, or "Everyone in" a
    /// circle.
    static func shareName(of share: LocationShare) -> String {
        share.isWithCircle ? String(localized: "Everyone in \(share.granteeDisplayName)") : share.granteeDisplayName
    }

    private func reloadShares() async {
        do {
            shares = try await connection.locationShares()
            sharingProblemText = nil
        } catch {
            sharingProblemText = error.loadProblemText
        }
    }

    /// Shares the person's location with a person or a circle, at a level.
    /// Nil when it worked, else the reason, for the sheet that asked to say.
    func share(with target: ShareTarget, name: String, level: LocationShareLevel) async -> String? {
        do {
            try await connection.shareLocation(granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId, level: level)
            doneText = String(localized: "Shared with \(name)")
            await reloadShares()
            finishSetupIfDone()
            return nil
        } catch {
            return error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }

    /// Changes how much of the person's location a share gives.
    func change(_ share: LocationShare, to level: LocationShareLevel) async {
        do {
            try await connection.shareLocation(granteeUserId: share.granteeUserId, granteeCircleId: share.granteeCircleId, level: level)
            doneText = String(localized: "Changed what \(Self.shareName(of: share)) can see")
            await reloadShares()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }

    /// Takes a share back.
    func stop(_ share: LocationShare) async {
        do {
            try await connection.unshareLocation(shareId: share.id)
            doneText = String(localized: "Stopped sharing with \(Self.shareName(of: share))")
            await reloadShares()
        } catch {
            problemText = error.refusalReason(orSay: String(localized: "That did not work. Check your connection and try again."))
        }
    }

    // MARK: Setting Location up

    /// The person's "Not now": the setup does not come back.
    func dismissSetup() {
        isSetupDismissed = true
        LocationRecorder.shared.dismissSetup(accountKey: AppModel.shared.account?.accountKey)
    }

    /// Ends the setup when every row that applies is done, saying so once.
    func finishSetupIfDone() {
        guard isSetupKnown, !isSetupDismissed, setup.isFinished else { return }
        // Said when it was on the screen: nothing to say of one finished elsewhere.
        let wasOnScreen = hasLoaded && location != nil && location?.latestPoint == nil
        dismissSetup()
        if wasOnScreen { doneText = String(localized: "Location is set up") }
    }

    /// "This is home": labels the place the person is at now. A place of
    /// theirs that already holds the position is the one labeled (a visit
    /// may have made it); otherwise `SavePlace` with the coordinates makes a
    /// new place there, with no visit recorded, which a check-in would add.
    func markHome() async {
        guard let position = await currentPosition() else { return }
        let here = CLLocation(latitude: position.latitudeDegrees, longitude: position.longitudeDegrees)
        // The person's places are read here, up to 200 with no filter, not
        // taken from the screen's list, which is a page narrowed by the label
        // filter and the search words and may not hold the place at hand.
        let allPlaces: [LocationPlace]
        do {
            allPlaces = try await connection.locationPlaces(limitCount: 200).places
        } catch {
            problemText = error.localizedDescription
            return
        }
        let holding = allPlaces.filter { CLLocation(latitude: $0.latitudeDegrees, longitude: $0.longitudeDegrees).distance(from: here) <= $0.radiusMeters }
        let nearest = holding.min {
            CLLocation(latitude: $0.latitudeDegrees, longitude: $0.longitudeDegrees).distance(from: here)
                < CLLocation(latitude: $1.latitudeDegrees, longitude: $1.longitudeDegrees).distance(from: here)
        }
        let change =
            nearest.map { PlaceChange(placeId: $0.id, placeLabel: PlaceLabel.home.rawValue) }
            ?? PlaceChange(
                placeLabel: PlaceLabel.home.rawValue, latitudeDegrees: position.latitudeDegrees, longitudeDegrees: position.longitudeDegrees)
        guard await save(change) != nil else { return }
        doneText = String(localized: "This place is now labeled Home")
        await load()
    }

    /// "Type my address": labels Home at the address the person chose. The
    /// server uses their own place there or makes one, so no place is named
    /// here. Nil when it worked, else the reason, for the address screen.
    func markHome(atAddress address: FoundPlace) async -> String? {
        let result = await saveAddress(
            PlaceChange(
                placeLabel: PlaceLabel.home.rawValue, latitudeDegrees: address.latitudeDegrees, longitudeDegrees: address.longitudeDegrees,
                addressText: address.chosenAddressText))
        guard result.saved != nil else { return result.refusalText }
        doneText = String(localized: "This place is now labeled Home")
        await load()
        return nil
    }

    /// Shares the person's location with one of their family circles from
    /// the setup, which is on the screen itself, so a refusal is said there.
    func shareWithFamily(_ circle: PeopleCircle, at level: LocationShareLevel) async {
        let refusalText = await share(with: .circle(circle.id), name: String(localized: "Everyone in \(circle.name)"), level: level)
        if let refusalText { problemText = refusalText }
    }
}

/// Runs a read and says how it went, so several can be waited for together
/// and one failing does not hide the others.
private func attempt<Value: Sendable>(_ work: @Sendable () async throws -> Value) async -> Result<Value, Error> {
    do {
        return .success(try await work())
    } catch {
        return .failure(error)
    }
}
