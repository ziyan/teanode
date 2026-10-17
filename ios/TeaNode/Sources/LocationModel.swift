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
    nonisolated static let placePageSize = 100

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

    init(connection: Connection) {
        self.connection = connection
    }

    /// The visits grouped by day, newest first.
    var days: [LocationVisitDay] { LocationDays.group(visits) }

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
        async let readLocation = attempt { try await connection.location() }
        async let readVisits = attempt { try await connection.locationVisits(limitCount: LocationModel.visitPageSize) }
        async let readPlaces = attempt { try await connection.locationPlaces(limitCount: LocationModel.placePageSize) }
        let (locationResult, visitsResult, placesResult) = await (readLocation, readVisits, readPlaces)
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
            let page = try await connection.locationPlaces(offset: offset, limitCount: Self.placePageSize)
            let known = Set(places.map(\.id))
            places += page.places.filter { !known.contains($0.id) }
            nextPlaceOffset = page.nextOffset
        } catch {
            problemText = error.localizedDescription
        }
    }

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
            return saved
        } catch {
            problemText = error.localizedDescription
            return nil
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
                avatarColorIndex: current.avatarColorIndex, locationShareLevel: current.locationShareLevel, latestPoint: current.latestPoint,
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
        if let live = LocationRecorder.shared.livePosition, Date().timeIntervalSince(live.measuredAt) < 60 {
            return (live.latitudeDegrees, live.longitudeDegrees)
        }
        let reading = await LocationReader.current()
        if let latitude = reading.latitudeDegrees, let longitude = reading.longitudeDegrees { return (latitude, longitude) }
        problemText = String(localized: "The phone could not tell where it is. Check that location is allowed for TeaNode, then try again.")
        return nil
    }

    func nearby(latitudeDegrees: Double, longitudeDegrees: Double) async throws -> [FoundPlace] {
        try await connection.nearbyPlaces(latitudeDegrees: latitudeDegrees, longitudeDegrees: longitudeDegrees)
    }

    /// Records that the person is at a place now.
    @discardableResult
    func checkIn(_ target: CheckInTarget, placeName: String) async -> Bool {
        do {
            try await connection.checkIn(target)
            doneText = String(localized: "Checked in at \(placeName)")
            await load()
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    /// Deletes everything recorded: every point, visit and place.
    @discardableResult
    func eraseHistory() async -> Bool {
        do {
            _ = try await connection.forgetLocationHistory()
            location = nil
            visits = []
            places = []
            nextVisitOffset = nil
            nextPlaceOffset = nil
            doneText = String(localized: "Your location history is erased")
            await load()
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
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
