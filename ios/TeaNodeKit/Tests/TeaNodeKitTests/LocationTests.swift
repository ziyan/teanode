import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// LocationTests cover what the phone keeps of where the person goes: what
/// the server's answers decode to, what each call sends, which positions are
/// kept, how a visit is named, and what waits in the outbox.
final class LocationTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private let placeAnswer = #"""
        {"id":"place-1","userId":"user-1","placeName":"Fixture Park","isNamedByPerson":false,"placeLabel":"","latitudeDegrees":10.5,
         "longitudeDegrees":20.5,"radiusMeters":75,"osmType":"way","osmId":123456789012,"osmName":"Fixture Park","osmCategory":"leisure=park",
         "addressText":"","visitCount":3,"lastVisitAt":"2024-01-02T10:00:00.123456789Z","namingErrorMessage":"",
         "createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-02T10:00:00Z"}
        """#

    private var visitAnswer: String {
        #"""
        {"id":"visit-1","arrivedAt":"2024-01-02T09:00:00Z","departedAt":null,"durationSeconds":null,"latitudeDegrees":10.5,
         "longitudeDegrees":20.5,"accuracyMeters":30,"visitSource":"visit","place":\#(placeAnswer)}
        """#
    }

    private func date(_ text: String) -> Date { ServerTime.date(text)! }

    // MARK: Reading

    func testReadsWhereThePersonIsWithTheVisitInProgress() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"GetLocation":{"userId":"user-1","username":"robin","name":null,"avatarUrl":null,"avatarColorIndex":3,
                 "shareLevel":"owner",
                 "latestPoint":{"latitudeDegrees":10.5,"longitudeDegrees":20.5,"accuracyMeters":12.5,"altitudeMeters":null,
                  "speedMetersPerSecond":null,"courseDegrees":null,"measuredAt":"2024-01-02T09:30:00Z","locationSource":"phone"},
                 "openVisit":\#(visitAnswer)}}}
                """#)
        let location = try await connection.location()
        XCTAssertEqual(location.displayName, "robin")
        XCTAssertEqual(location.level, .owner)
        XCTAssertEqual(location.latestPoint?.accuracyMeters, 12.5)
        XCTAssertNotNil(location.latestPoint?.measuredDate)
        let open = try XCTUnwrap(location.openVisit)
        XCTAssertTrue(open.isOpen)
        XCTAssertNil(open.visitDuration)
        XCTAssertEqual(open.place.osmId, 123_456_789_012)
        XCTAssertEqual(open.place.visitCount, 3)
        XCTAssertNotNil(open.place.lastVisitDate)
        XCTAssertFalse(open.place.isOfPersonsOwn)
        XCTAssertEqual(try sentVariables(fake.sent[0]), [:])
    }

    func testAVisitWithBothEndsHasADuration() throws {
        let text = visitAnswer.replacingOccurrences(of: #""departedAt":null"#, with: #""departedAt":"2024-01-02T10:25:00Z""#)
        let visit = try JSONDecoder().decode(PlaceVisit.self, from: Data(text.utf8))
        XCTAssertFalse(visit.isOpen)
        XCTAssertEqual(visit.visitDuration, .seconds(85 * 60))
        let told = visitAnswer.replacingOccurrences(of: #""durationSeconds":null"#, with: #""durationSeconds":60.5"#)
        XCTAssertEqual(try JSONDecoder().decode(PlaceVisit.self, from: Data(told.utf8)).visitDuration, .seconds(60.5))
    }

    func testPagesOfVisitsAskForTheOffsetTheLastOneGave() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"ListLocationVisits":{"placeVisits":[\#(visitAnswer)],"nextOffset":50}}}"#)
        let first = try await connection.locationVisits()
        XCTAssertEqual(first.nextOffset, 50)
        _ = try await connection.locationVisits(
            from: date("2024-01-02T00:00:00Z"), until: date("2024-01-03T00:00:00Z"), placeId: "place-1", offset: 50, limitCount: 50)
        XCTAssertEqual(try sentVariables(fake.sent[0]), [:])
        XCTAssertEqual(
            try sentVariables(fake.sent[1]),
            [
                "from": .string("2024-01-02T00:00:00Z"), "until": .string("2024-01-03T00:00:00Z"), "placeId": .string("place-1"),
                "offset": .number(50), "limitCount": .number(50),
            ])
    }

    func testReadsPlacesAndNearbyPlaces() async throws {
        let (placesConnection, _) = connection(answering: #"{"data":{"ListPlaces":{"places":[\#(placeAnswer)],"nextOffset":null}}}"#)
        let page = try await placesConnection.locationPlaces()
        XCTAssertEqual(page.places.map(\.id), ["place-1"])
        XCTAssertNil(page.nextOffset)

        let (nearbyConnection, fake) = connection(
            answering: #"""
                {"data":{"ListNearbyPlaces":[{"placeName":"Fixture Cafe","osmType":"node","osmId":42,"osmCategory":"amenity=cafe",
                 "addressText":"1 Fixture Street","latitudeDegrees":10.5,"longitudeDegrees":20.5,"distanceMeters":40.5,"placeId":null}]}}
                """#)
        let found = try await nearbyConnection.nearbyPlaces(latitudeDegrees: 10.5, longitudeDegrees: 20.5)
        XCTAssertEqual(found.first?.categoryValue, "cafe")
        XCTAssertEqual(found.first?.id, "node/42")
        // An older server sends neither field.
        XCTAssertEqual(found.first?.placeLabel, "")
        XCTAssertEqual(found.first?.isNamedByPerson, false)
        XCTAssertNil(found.first?.label)

        let (labeledConnection, _) = connection(
            answering: #"""
                {"data":{"ListNearbyPlaces":[{"placeName":"Fixture Park","osmType":"way","osmId":7,"osmCategory":"leisure=park",
                 "addressText":"","latitudeDegrees":10.5,"longitudeDegrees":20.5,"distanceMeters":10,"placeId":"place-1",
                 "placeLabel":"gym","isNamedByPerson":false}]}}
                """#)
        let labeled = try await labeledConnection.nearbyPlaces(latitudeDegrees: 10.5, longitudeDegrees: 20.5)
        XCTAssertEqual(labeled.first?.label, .gym)
        XCTAssertEqual(labeled.first?.isNamedByPerson, false)
        XCTAssertEqual(
            try sentVariables(fake.sent[0]), ["latitudeDegrees": .number(10.5), "longitudeDegrees": .number(20.5)])
    }

    // MARK: Changing

    func testSavesOnlyTheFieldsGivenAndTrimsTheName() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SavePlace":\#(placeAnswer),"ForgetPlace":true}}"#)
        try await connection.savePlace(PlaceChange(placeId: "place-1", placeName: "  Fixture Home ", placeLabel: "home"))
        try await connection.savePlace(PlaceChange(placeId: "place-1", placeLabel: ""))
        try await connection.savePlace(PlaceChange(placeId: "place-1", radiusMeters: 150))
        try await connection.forgetPlace(placeId: "place-1")
        XCTAssertEqual(
            try sentVariables(fake.sent[0]), ["placeId": .string("place-1"), "placeName": .string("Fixture Home"), "placeLabel": .string("home")])
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["placeId": .string("place-1"), "placeLabel": .string("")])
        XCTAssertEqual(try sentVariables(fake.sent[2]), ["placeId": .string("place-1"), "radiusMeters": .number(150)])
        XCTAssertEqual(try sentVariables(fake.sent[3]), ["placeId": .string("place-1")])
    }

    func testChecksInAtAPlaceOrAFoundPlace() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"CheckIn":\#(visitAnswer)}}"#)
        try await connection.checkIn(.place("place-1"))
        let found = FoundPlace(
            placeName: "Fixture Cafe", osmType: "node", osmId: 42, osmCategory: "amenity=cafe", addressText: "1 Fixture Street",
            latitudeDegrees: 10.5, longitudeDegrees: 20.5, distanceMeters: 40)
        try await connection.checkIn(.found(found))
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["placeId": .string("place-1")])
        XCTAssertEqual(
            try sentVariables(fake.sent[1]),
            [
                "latitudeDegrees": .number(10.5), "longitudeDegrees": .number(20.5), "placeName": .string("Fixture Cafe"),
                "osmType": .string("node"), "osmId": .number(42), "osmCategory": .string("amenity=cafe"),
                "addressText": .string("1 Fixture Street"),
            ])
    }

    func testErasesEverythingWithNoRange() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"EraseLocationHistory":{"pointCount":10,"visitCount":2,"placeCount":1}}}"#)
        let result = try await connection.eraseLocationHistory()
        XCTAssertEqual(result.pointCount, 10)
        XCTAssertEqual(try sentVariables(fake.sent[0]), [:])
    }

    func testReportsABatchOfPointsAndVisits() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"ReportLocation":{"newPointCount":1,"newVisitCount":1,"updatedVisitCount":0}}}"#)
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addPoint(
            ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 8, speedMetersPerSecond: 1.5, measuredAt: date("2024-01-02T09:00:00Z")))
        outbox.arrive(placeId: "place-1", latitudeDegrees: 10.5, longitudeDegrees: 20.5, at: date("2024-01-02T09:00:00Z"))
        let result = try await connection.reportLocation(outbox.nextBatch(), agentPhoneId: "phone-1")
        XCTAssertEqual(result.newVisitCount, 1)
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["agentPhoneId"], .string("phone-1"))
        XCTAssertEqual(
            variables["points"],
            .array([
                .object([
                    "latitudeDegrees": .number(10.5), "longitudeDegrees": .number(20.5), "accuracyMeters": .number(8),
                    "speedMetersPerSecond": .number(1.5), "measuredAt": .string("2024-01-02T09:00:00Z"),
                ])
            ]))
        XCTAssertEqual(
            variables["visits"],
            .array([
                .object([
                    "clientVisitKey": .string("region:place-1:a1704186000"), "latitudeDegrees": .number(10.5), "longitudeDegrees": .number(20.5),
                    "accuracyMeters": .number(0), "visitSource": .string("region"), "arrivedAt": .string("2024-01-02T09:00:00Z"),
                    "placeId": .string("place-1"),
                ])
            ]))
    }

    func testEveryLocationDocumentIsRegistered() {
        for name in ["location", "locationVisits", "locationPlaces", "reportLocation", "savePlace", "checkIn", "locationEvents"] {
            XCTAssertNotNil(Documents.locationDocuments[name], name)
        }
    }

    // MARK: Keeping

    func testKeepsAPositionAfter25MetersOrAMinuteOfMoving() {
        var filter = LocationPointFilter()
        let start = date("2024-01-02T09:00:00Z")
        // About 11 meters per 0.0001 degrees of latitude.
        XCTAssertTrue(filter.shouldKeep(latitudeDegrees: 10.5, longitudeDegrees: 20.5, speedMetersPerSecond: nil, measuredAt: start, isStationary: false))
        XCTAssertFalse(
            filter.shouldKeep(
                latitudeDegrees: 10.5001, longitudeDegrees: 20.5, speedMetersPerSecond: 1, measuredAt: start.addingTimeInterval(10), isStationary: false))
        XCTAssertTrue(
            filter.shouldKeep(
                latitudeDegrees: 10.5003, longitudeDegrees: 20.5, speedMetersPerSecond: 1, measuredAt: start.addingTimeInterval(20), isStationary: false))
        // A minute later, 11 meters on and moving: kept. Not moving: not kept.
        XCTAssertFalse(
            filter.shouldKeep(
                latitudeDegrees: 10.5004, longitudeDegrees: 20.5, speedMetersPerSecond: 0.2, measuredAt: start.addingTimeInterval(90), isStationary: false))
        XCTAssertTrue(
            filter.shouldKeep(
                latitudeDegrees: 10.5004, longitudeDegrees: 20.5, speedMetersPerSecond: 0.9, measuredAt: start.addingTimeInterval(100), isStationary: false))
    }

    func testKeepsNothingWhileStationary() {
        var filter = LocationPointFilter()
        XCTAssertFalse(
            filter.shouldKeep(latitudeDegrees: 10.5, longitudeDegrees: 20.5, speedMetersPerSecond: nil, measuredAt: Date(), isStationary: true))
        XCTAssertTrue(
            filter.shouldKeep(latitudeDegrees: 10.5, longitudeDegrees: 20.5, speedMetersPerSecond: nil, measuredAt: Date(), isStationary: false))
        filter.reset()
        XCTAssertTrue(
            filter.shouldKeep(latitudeDegrees: 10.5, longitudeDegrees: 20.5, speedMetersPerSecond: nil, measuredAt: Date(), isStationary: false))
    }

    func testTheDistanceBetweenTwoCoordinates() {
        let meters = LocationGeometry.distanceMeters(
            fromLatitudeDegrees: 10.5, fromLongitudeDegrees: 20.5, toLatitudeDegrees: 10.6, toLongitudeDegrees: 20.5)
        XCTAssertEqual(meters, 11_119, accuracy: 20)
        XCTAssertFalse(LocationGeometry.isOnEarth(latitudeDegrees: 91, longitudeDegrees: 0))
        XCTAssertFalse(LocationGeometry.isOnEarth(latitudeDegrees: .nan, longitudeDegrees: 0))
        XCTAssertTrue(LocationGeometry.isOnEarth(latitudeDegrees: -90, longitudeDegrees: 180))
    }

    func testAVisitIsNamedFromWhereAndWhenItBegan() {
        let arrived = date("2024-01-02T09:00:00Z")
        let departed = date("2024-01-02T10:00:00Z")
        let arrival = LocationVisitKey.visit(latitudeDegrees: 10.5, longitudeDegrees: 20.5, arrivedAt: arrived, departedAt: nil)
        let departure = LocationVisitKey.visit(latitudeDegrees: 10.5, longitudeDegrees: 20.5, arrivedAt: arrived, departedAt: departed)
        XCTAssertEqual(arrival, departure)
        XCTAssertEqual(arrival, "visit:10.500:20.500:a1704186000")
        let unseen = LocationVisitKey.visit(latitudeDegrees: 10.5, longitudeDegrees: 20.5, arrivedAt: nil, departedAt: departed)
        XCTAssertEqual(unseen, "visit:10.500:20.500:d1704189600")
        XCTAssertNotEqual(unseen, arrival)
    }

    // MARK: The outbox

    func testTheOutboxHandsOutBatchesOfAtMost500PointsAnd50Visits() {
        var outbox = LocationOutbox(accountKey: "account")
        let start = date("2024-01-02T00:00:00Z")
        for index in 0..<620 {
            outbox.addPoint(
                ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: start.addingTimeInterval(Double(index))))
        }
        for index in 0..<55 {
            outbox.addVisit(
                ReportedVisit(
                    clientVisitKey: "key-\(index)", latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, arrivedAt: start,
                    departedAt: nil, visitSource: "visit"))
        }
        let first = outbox.nextBatch()
        XCTAssertEqual(first.points.count, 500)
        XCTAssertEqual(first.visits.count, 50)
        XCTAssertEqual(first.points.first?.measuredAt, start)
        outbox.remove(first)
        let second = outbox.nextBatch()
        XCTAssertEqual(second.points.count, 120)
        XCTAssertEqual(second.visits.map(\.clientVisitKey), (50..<55).map { "key-\($0)" })
        outbox.remove(second)
        XCTAssertTrue(outbox.isEmpty)
    }

    func testWhatIsAddedWhileABatchIsInFlightStays() {
        var outbox = LocationOutbox(accountKey: "account")
        let start = date("2024-01-02T00:00:00Z")
        outbox.addPoint(ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: start))
        let inFlight = outbox.nextBatch()
        outbox.addPoint(ReportedPoint(latitudeDegrees: 10.6, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: start.addingTimeInterval(30)))
        outbox.remove(inFlight)
        XCTAssertEqual(outbox.points.count, 1)
        XCTAssertEqual(outbox.points.first?.latitudeDegrees, 10.6)
    }

    func testTheOutboxKeepsTheNewest20000PointsAndEveryVisit() {
        var outbox = LocationOutbox(accountKey: "account")
        let start = date("2024-01-02T00:00:00Z")
        outbox.addVisit(
            ReportedVisit(
                clientVisitKey: "first", latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, arrivedAt: start, departedAt: nil,
                visitSource: "visit"))
        for index in 0..<(LocationOutbox.pointCountMost + 7) {
            outbox.addPoint(
                ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: start.addingTimeInterval(Double(index))))
        }
        XCTAssertEqual(outbox.points.count, LocationOutbox.pointCountMost)
        XCTAssertEqual(outbox.points.first?.measuredAt, start.addingTimeInterval(7))
        XCTAssertEqual(outbox.visits.count, 1)
    }

    func testAPositionThatIsNotOnTheEarthIsNotQueued() {
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addPoint(ReportedPoint(latitudeDegrees: 200, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: Date()))
        outbox.addPoint(ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: -1, measuredAt: Date()))
        XCTAssertTrue(outbox.isEmpty)
    }

    func testWhatTheServerWouldRefuseIsNotQueued() {
        let now = Date()
        func visit(
            key: String = "key", accuracy: Double = 10, arrivedAt: Date? = now.addingTimeInterval(-60), departedAt: Date? = nil, source: String = "visit"
        ) -> ReportedVisit {
            ReportedVisit(
                clientVisitKey: key, latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: accuracy, arrivedAt: arrivedAt,
                departedAt: departedAt, visitSource: source)
        }
        func point(measuredAt: Date) -> ReportedPoint {
            ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: measuredAt)
        }
        XCTAssertTrue(LocationOutbox.isAcceptable(point(measuredAt: now.addingTimeInterval(3600))))
        XCTAssertFalse(LocationOutbox.isAcceptable(point(measuredAt: now.addingTimeInterval(25 * 3600))))
        XCTAssertFalse(
            LocationOutbox.isAcceptable(ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: .nan, measuredAt: now)))

        XCTAssertTrue(LocationOutbox.isAcceptable(visit()))
        XCTAssertTrue(LocationOutbox.isAcceptable(visit(key: String(repeating: "k", count: 128))))
        XCTAssertFalse(LocationOutbox.isAcceptable(visit(key: String(repeating: "k", count: 129))))
        XCTAssertFalse(LocationOutbox.isAcceptable(visit(key: "  ")))
        XCTAssertFalse(LocationOutbox.isAcceptable(visit(accuracy: -1)))
        XCTAssertFalse(LocationOutbox.isAcceptable(visit(source: "check_in")))
        XCTAssertFalse(LocationOutbox.isAcceptable(visit(arrivedAt: nil, departedAt: nil)))
        XCTAssertFalse(LocationOutbox.isAcceptable(visit(arrivedAt: now.addingTimeInterval(25 * 3600))))
        XCTAssertFalse(LocationOutbox.isAcceptable(visit(arrivedAt: nil, departedAt: now.addingTimeInterval(25 * 3600))))
        XCTAssertFalse(LocationOutbox.isAcceptable(visit(arrivedAt: now.addingTimeInterval(-60), departedAt: now.addingTimeInterval(-120))))
        XCTAssertTrue(LocationOutbox.isAcceptable(visit(arrivedAt: now.addingTimeInterval(-120), departedAt: now.addingTimeInterval(-60))))

        var outbox = LocationOutbox(accountKey: "account")
        outbox.addPoint(point(measuredAt: now.addingTimeInterval(25 * 3600)))
        outbox.addVisit(visit(source: "check_in"))
        XCTAssertTrue(outbox.isEmpty)
    }

    func testABatchTheServerCallsInvalidIsSplitUntilTheOneEntryIsFound() throws {
        func point(_ index: Int) -> ReportedPoint {
            ReportedPoint(sequence: index, latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: Date())
        }
        func visit(_ index: Int) -> ReportedVisit {
            ReportedVisit(
                sequence: index, clientVisitKey: "key-\(index)", latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5,
                arrivedAt: Date(), departedAt: nil, visitSource: "visit")
        }
        let batch = LocationBatch(points: (1...5).map(point), visits: (6...8).map(visit))
        XCTAssertEqual(batch.entryCount, 8)

        // Positions and visits go apart first.
        let apart = try XCTUnwrap(batch.split())
        XCTAssertEqual(apart.first.points.count, 5)
        XCTAssertTrue(apart.first.visits.isEmpty)
        XCTAssertEqual(apart.second.visits.count, 3)
        XCTAssertTrue(apart.second.points.isEmpty)

        // Then each is halved, with nothing lost or repeated.
        let halves = try XCTUnwrap(apart.first.split())
        XCTAssertEqual(halves.first.points.map(\.sequence), [1, 2])
        XCTAssertEqual(halves.second.points.map(\.sequence), [3, 4, 5])
        let visitHalves = try XCTUnwrap(apart.second.split())
        XCTAssertEqual(visitHalves.first.visits.map(\.sequence), [6])
        XCTAssertEqual(visitHalves.second.visits.map(\.sequence), [7, 8])

        // One entry cannot be split: it is the one that is refused.
        XCTAssertNil(LocationBatch(points: [point(1)], visits: []).split())
        XCTAssertNil(LocationBatch(points: [], visits: [visit(1)]).split())
        XCTAssertNil(LocationBatch(points: [], visits: []).split())
    }

    func testADepartureFindsItsArrivalAfterTheOutboxIsReadBack() throws {
        var outbox = LocationOutbox(accountKey: "account")
        let arrived = date("2024-01-02T09:00:00Z")
        XCTAssertTrue(outbox.arrive(placeId: "place-1", latitudeDegrees: 10.5, longitudeDegrees: 20.5, at: arrived))
        outbox.remove(outbox.nextBatch())

        var reread = try LocationOutbox.decoded(from: try outbox.encoded())
        XCTAssertEqual(reread, outbox)
        XCTAssertTrue(reread.depart(placeId: "place-1", at: arrived.addingTimeInterval(3600)))
        XCTAssertFalse(reread.depart(placeId: "place-1", at: arrived.addingTimeInterval(3700)))
        let departure = try XCTUnwrap(reread.visits.first)
        XCTAssertEqual(departure.clientVisitKey, LocationVisitKey.region(placeId: "place-1", arrivedAt: arrived))
        XCTAssertNil(departure.arrivedAt)
        XCTAssertEqual(departure.departedAt, arrived.addingTimeInterval(3600))
        XCTAssertEqual(departure.placeId, "place-1")
    }

    func testAnArrivalAtAPlaceWithAnOpenVisitEndsThatVisitFirst() {
        var outbox = LocationOutbox(accountKey: "account")
        let first = date("2024-01-02T09:00:00Z")
        let second = first.addingTimeInterval(3 * 3600)
        outbox.arrive(placeId: "place-1", latitudeDegrees: 10.5, longitudeDegrees: 20.5, at: first)
        XCTAssertTrue(outbox.arrive(placeId: "place-1", latitudeDegrees: 10.5, longitudeDegrees: 20.5, at: second))
        XCTAssertEqual(outbox.visits.count, 3)
        // The first visit is ended at the second arrival, under its own key.
        XCTAssertEqual(outbox.visits[1].clientVisitKey, LocationVisitKey.region(placeId: "place-1", arrivedAt: first))
        XCTAssertEqual(outbox.visits[1].departedAt, second)
        XCTAssertEqual(outbox.visits[2].clientVisitKey, LocationVisitKey.region(placeId: "place-1", arrivedAt: second))
        XCTAssertNotNil(outbox.openRegionVisits["place-1"])
    }

    func testEndingEveryVisitWhenMonitoringStopsLeavesNoneOpen() {
        var outbox = LocationOutbox(accountKey: "account")
        let arrived = date("2024-01-02T09:00:00Z")
        outbox.arrive(placeId: "place-1", latitudeDegrees: 10.5, longitudeDegrees: 20.5, at: arrived)
        outbox.arrive(placeId: "place-2", latitudeDegrees: 10.6, longitudeDegrees: 20.5, at: arrived)
        outbox.departEverywhere(at: arrived.addingTimeInterval(600))
        XCTAssertTrue(outbox.openRegionVisits.isEmpty)
        XCTAssertEqual(outbox.visits.filter { $0.departedAt != nil }.count, 2)
    }

    func testAVisitWithNeitherAnArrivalNorADepartureIsNotQueued() {
        var outbox = LocationOutbox(accountKey: "mail.example.com/robin")
        outbox.addVisit(
            ReportedVisit(
                clientVisitKey: "undated", latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 20, arrivedAt: nil, departedAt: nil,
                visitSource: "visit"))
        XCTAssertTrue(outbox.isEmpty)
        outbox.addVisit(
            ReportedVisit(
                clientVisitKey: "dated", latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 20, arrivedAt: Date(timeIntervalSince1970: 1_000),
                departedAt: nil, visitSource: "visit"))
        XCTAssertEqual(outbox.visits.count, 1)
    }

    func testDiscardingWhatIsUnsentKeepsTheVisitsBegun() {
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addPoint(ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: Date()))
        outbox.arrive(placeId: "place-1", latitudeDegrees: 10.5, longitudeDegrees: 20.5, at: Date())
        outbox.discardUnsent()
        XCTAssertTrue(outbox.isEmpty)
        XCTAssertNotNil(outbox.openRegionVisits["place-1"])
        XCTAssertEqual(outbox.accountKey, "account")
    }

    func testDiscardingARangeKeepsWhatWasNoticedOutsideIt() {
        var outbox = LocationOutbox(accountKey: "account")
        let day = date("2024-01-02T00:00:00Z")
        for hour in [1.0, 30.0] {
            outbox.addPoint(ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: day.addingTimeInterval(hour * 3600)))
        }
        outbox.addVisit(
            ReportedVisit(
                clientVisitKey: "inside", latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, arrivedAt: day.addingTimeInterval(3600),
                departedAt: nil, visitSource: "visit"))
        outbox.addVisit(
            ReportedVisit(
                clientVisitKey: "outside", latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, arrivedAt: nil,
                departedAt: day.addingTimeInterval(30 * 3600), visitSource: "visit"))
        outbox.discardUnsent(from: day, until: day.addingTimeInterval(86_400))
        XCTAssertEqual(outbox.points.count, 1)
        XCTAssertEqual(outbox.visits.map(\.clientVisitKey), ["outside"])
    }

    func testEachAccountHasAnOutboxFileOfItsOwn() {
        let first = LocationOutbox.fileName(accountKey: "mail.example.com/robin")
        let second = LocationOutbox.fileName(accountKey: "mail.example.com/sam")
        XCTAssertNotEqual(first, second)
        XCTAssertEqual(first, LocationOutbox.fileName(accountKey: "mail.example.com/robin"))
        XCTAssertFalse(LocationOutbox.fileName(accountKey: "../../elsewhere").contains("/"))
        XCTAssertFalse(LocationOutbox.fileName(accountKey: "../../elsewhere").contains(".."))
    }

    func testAFailedBatchIsRetriedOnlyForACallThatDidNotGetThrough() {
        XCTAssertEqual(LocationReportFailure.classify(TeaNodeError.refused("api: permission denied")), .refused("api: permission denied"))
        XCTAssertEqual(
            LocationReportFailure.classify(TeaNodeError.refused("api: invalid arguments: bad point")), .batchInvalid("api: invalid arguments: bad point"))
        XCTAssertEqual(LocationReportFailure.classify(TeaNodeError.refused("api: not found")), .phoneNotFound)
        XCTAssertEqual(LocationReportFailure.classify(TeaNodeError.signedOut), .retryLater)
        XCTAssertEqual(LocationReportFailure.classify(URLError(.notConnectedToInternet)), .retryLater)
    }

    func testEventsWaitHalfAMinuteAfterAFailureButNotAfterASuccess() {
        var gate = LocationRetryGate()
        let now = date("2024-01-02T09:00:00Z")
        XCTAssertTrue(gate.canTry(at: now))
        gate.didFail(at: now)
        XCTAssertFalse(gate.canTry(at: now.addingTimeInterval(10)))
        XCTAssertTrue(gate.canTry(at: now.addingTimeInterval(30)))
        gate.didFail(at: now)
        gate.didSucceed()
        XCTAssertTrue(gate.canTry(at: now.addingTimeInterval(1)))
    }

    // MARK: Watching places

    private func place(_ id: String, latitude: Double, label: String = "", isNamed: Bool = false, radius: Double = 75) -> LocationPlace {
        LocationPlace(
            id: id, placeName: id, isNamedByPerson: isNamed, placeLabel: label, latitudeDegrees: latitude, longitudeDegrees: 20.5, radiusMeters: radius)
    }

    func testWatchesTheNearestTwentyOfTheLabeledAndNamedPlaces() {
        var places = (0..<30).map { place("named-\($0)", latitude: 10.5 + Double($0) * 0.01, isNamed: true) }
        places.append(place("only-from-the-map", latitude: 10.5))
        places.append(place("home", latitude: 11.5, label: "home"))
        let watched = LocationPlaceWatch.choose(from: places, nearLatitudeDegrees: 10.5, nearLongitudeDegrees: 20.5)
        XCTAssertEqual(watched.count, 20)
        XCTAssertEqual(watched.first?.placeId, "named-0")
        XCTAssertFalse(watched.contains { $0.placeId == "only-from-the-map" })
        XCTAssertFalse(watched.contains { $0.placeId == "home" })
    }

    func testAWatchedCircleIsAtLeast100MetersWide() {
        let places = [place("small", latitude: 10.5, isNamed: true, radius: 25), place("large", latitude: 10.51, isNamed: true, radius: 400)]
        let watched = LocationPlaceWatch.choose(from: places, nearLatitudeDegrees: nil, nearLongitudeDegrees: nil)
        XCTAssertEqual(watched.map(\.radiusMeters).sorted(), [100, 400])
    }

    // MARK: Showing

    func testVisitsAreGroupedByTheDayTheyBeganNewestFirst() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "UTC"))
        func visit(_ id: String, arrived: String?, departed: String?) -> PlaceVisit {
            PlaceVisit(id: id, place: place("p", latitude: 10.5), arrivedAt: arrived, departedAt: departed, latitudeDegrees: 10.5, longitudeDegrees: 20.5)
        }
        let days = LocationDays.group(
            [
                visit("early", arrived: "2024-01-02T08:00:00Z", departed: "2024-01-02T09:00:00Z"),
                visit("yesterday", arrived: "2024-01-01T23:00:00Z", departed: "2024-01-02T07:00:00Z"),
                visit("late", arrived: "2024-01-02T18:00:00Z", departed: nil),
                visit("unseen arrival", arrived: nil, departed: "2024-01-01T10:00:00Z"),
                visit("no time", arrived: nil, departed: nil),
            ], calendar: calendar)
        XCTAssertEqual(days.count, 2)
        XCTAssertEqual(days[0].visits.map(\.id), ["late", "early"])
        XCTAssertEqual(days[1].visits.map(\.id), ["yesterday", "unseen arrival"])
        let window = LocationDays.window(of: date("2024-01-02T18:00:00Z"), calendar: calendar)
        XCTAssertEqual(window.from, date("2024-01-02T00:00:00Z"))
        XCTAssertEqual(window.until, date("2024-01-03T00:00:00Z"))
    }

    // MARK: Setting Location up

    private func fixturePlace(label: String) -> LocationPlace {
        LocationPlace(id: "place-1", placeName: "Fixture", placeLabel: label, latitudeDegrees: 10.5, longitudeDegrees: 20.5)
    }

    private func fixtureCircle(circleKind: String) -> PeopleCircle {
        PeopleCircle(id: "circle-1", name: "Fixture Home", circleKind: circleKind, createdAt: "2024-01-01T00:00:00Z", memberRole: "admin", members: [])
    }

    private func fixtureShare(circleId: String) -> LocationShare {
        LocationShare(
            id: "share-1", granteeUserId: nil, granteeUsername: nil, granteeName: nil, granteeCircleId: circleId, granteeCircleName: "Fixture Home",
            shareLevel: "history", createdAt: "2024-01-01T00:00:00Z")
    }

    func testTheSetupIsWorkedOutFromTheServerAndThePermission() {
        let begun = LocationSetup(isPermissionDone: false, homePlaces: [], circles: [fixtureCircle(circleKind: "family")], shares: [])
        XCTAssertEqual(begun.familyCircles.map(\.id), ["circle-1"])
        XCTAssertTrue(begun.isShareOffered)
        XCTAssertFalse(begun.isFinished)
        XCTAssertTrue(begun.isShown(isDismissed: false, hasLatestPoint: false))
        XCTAssertFalse(begun.isShown(isDismissed: true, hasLatestPoint: false))
        XCTAssertFalse(begun.isShown(isDismissed: false, hasLatestPoint: true))

        let finished = LocationSetup(
            isPermissionDone: true, homePlaces: [fixturePlace(label: "home")], circles: [fixtureCircle(circleKind: "family")],
            shares: [fixtureShare(circleId: "circle-1")])
        XCTAssertTrue(finished.isFinished)
        XCTAssertFalse(finished.isShown(isDismissed: false, hasLatestPoint: false))
    }

    func testTheShareRowIsOnlyForAFamilyAndAPlaceLabeledWorkIsNotHome() {
        let noFamily = LocationSetup(
            isPermissionDone: true, homePlaces: [fixturePlace(label: "home")], circles: [fixtureCircle(circleKind: "friends")], shares: [])
        XCTAssertFalse(noFamily.isShareOffered)
        XCTAssertTrue(noFamily.isFinished)
        let notHome = LocationSetup(isPermissionDone: true, homePlaces: [fixturePlace(label: "work")], circles: [], shares: [])
        XCTAssertFalse(notHome.isHomeDone)
        let shareElsewhere = LocationSetup(
            isPermissionDone: true, homePlaces: [fixturePlace(label: "home")], circles: [fixtureCircle(circleKind: "family")],
            shares: [fixtureShare(circleId: "circle-other")])
        XCTAssertFalse(shareElsewhere.isShareDone)
    }

    func testTheSetupListsEveryFamilyCircleAndIsDoneOnlyWhenAllAreShared() {
        let second = PeopleCircle(
            id: "circle-2", name: "Fixture Cousins", circleKind: "family", createdAt: "2024-01-01T00:00:00Z", memberRole: "member", members: [])
        let friends = PeopleCircle(
            id: "circle-3", name: "Fixture Friends", circleKind: "friends", createdAt: "2024-01-01T00:00:00Z", memberRole: "member", members: [])
        let circles = [fixtureCircle(circleKind: "family"), second, friends]
        let half = LocationSetup(
            isPermissionDone: true, homePlaces: [fixturePlace(label: "home")], circles: circles, shares: [fixtureShare(circleId: "circle-1")])
        XCTAssertEqual(half.familyCircles.map(\.id), ["circle-1", "circle-2"])
        XCTAssertTrue(half.isShared(with: circles[0]))
        XCTAssertFalse(half.isShared(with: second))
        XCTAssertFalse(half.isShareDone)
        XCTAssertFalse(half.isFinished)
        let all = LocationSetup(
            isPermissionDone: true, homePlaces: [fixturePlace(label: "home")], circles: circles,
            shares: [fixtureShare(circleId: "circle-1"), fixtureShare(circleId: "circle-2")])
        XCTAssertTrue(all.isFinished)
    }

    func testALocationNotificationCarriesTheUserWhoMoved() throws {
        let json = #"{"notificationKind":"location","locationUserId":"user-2","title":"Person B arrived home","body":"","threadId":"location-user-2"}"#
        let content = try JSONDecoder().decode(NotificationContent.self, from: Data(json.utf8))
        XCTAssertEqual(content.locationUserId, "user-2")
        XCTAssertTrue(AgentPhone.allNotificationKinds.contains("location"))
    }
}
