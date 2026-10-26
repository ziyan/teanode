import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// LocationTravelTests cover the trips between visits, the ways of travel,
/// the totals, and the motion activity the phone queues for the server.
final class LocationTravelTests: XCTestCase {
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

    private func date(_ text: String) -> Date { ServerTime.date(text)! }

    private let legAnswer = #"""
        {"id":"leg-1","startedAt":"2024-01-02T08:05:00Z","endedAt":"2024-01-02T08:15:00Z","durationSeconds":600,"distanceMeters":640,
         "travelMode":"bus","detectedTravelMode":"car","isTravelModeCorrected":true,"detectionSource":"motion"}
        """#

    private var placeAnswer: String {
        #"""
        {"id":"place-1","userId":"user-1","placeName":"Fixture Park","isNamedByPerson":false,"placeLabel":"","latitudeDegrees":10.5,
         "longitudeDegrees":20.5,"radiusMeters":75,"osmType":"way","osmId":123456789012,"osmName":"Fixture Park","osmCategory":"leisure=park",
         "addressText":"","isAddressTyped":false,"visitCount":3,"lastVisitAt":"2024-01-02T10:00:00Z","namingErrorMessage":"",
         "createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-02T10:00:00Z"}
        """#
    }

    private func activity(_ offsetSeconds: Double, kind: String = "walking", confidence: String = "high") -> ReportedActivity {
        ReportedActivity(
            startedAt: date("2024-01-02T00:00:00Z").addingTimeInterval(offsetSeconds), activityKind: kind, activityConfidence: confidence)
    }

    // MARK: Ways of travel

    func testEachWayOfTravelHasItsSymbol() {
        XCTAssertEqual(
            TravelMode.allCases.map(\.rawValue), ["walk", "run", "bike", "car", "bus", "train", "boat", "plane"])
        XCTAssertEqual(
            TravelMode.allCases.map(\.symbolName),
            ["figure.walk", "figure.run", "bicycle", "car.fill", "bus.fill", "tram.fill", "ferry.fill", "airplane"])
        XCTAssertEqual(TravelMode.symbolName(forTravelMode: "bike"), "bicycle")
        XCTAssertFalse(TravelMode.symbolName(forTravelMode: "hovercraft").isEmpty)
    }

    // MARK: Reading

    func testReadsTripsWithTheirPlacesAndLegs() async throws {
        let body = #"""
            {"data":{"ListTrips":{"trips":[{"id":"trip-1","startedAt":"2024-01-02T08:05:00Z","endedAt":"2024-01-02T08:40:00Z",
             "durationSeconds":2100,"distanceMeters":12400,"fromPlace":\#(placeAnswer),"toPlace":null,"legs":[\#(legAnswer)]}],"nextOffset":null}}}
            """#
        let (connection, fake) = connection(answering: body)
        let page = try await connection.trips(from: date("2024-01-02T00:00:00Z"), until: date("2024-01-03T00:00:00Z"))
        XCTAssertEqual(page.trips.count, 1)
        let trip = page.trips[0]
        XCTAssertEqual(trip.fromPlace?.placeName, "Fixture Park")
        XCTAssertNil(trip.toPlace)
        XCTAssertEqual(trip.legs[0].mode, .bus)
        XCTAssertEqual(trip.legs[0].detectedTravelMode, "car")
        XCTAssertTrue(trip.legs[0].isTravelModeCorrected)
        XCTAssertEqual(trip.startedDate, date("2024-01-02T08:05:00Z"))
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["from"], .string("2024-01-02T00:00:00Z"))
        XCTAssertEqual(variables["until"], .string("2024-01-03T00:00:00Z"))
        XCTAssertNil(variables["userId"])
        XCTAssertNil(variables["offset"])
    }

    func testTripsOfAnotherPersonAskForThemAndPage() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"ListTrips":{"trips":[],"nextOffset":null}}}"#)
        _ = try await connection.trips(
            userId: "user-2", from: date("2024-01-02T00:00:00Z"), until: date("2024-01-03T00:00:00Z"), offset: 20, limitCount: 20)
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["userId"], .string("user-2"))
        XCTAssertEqual(variables["offset"], .number(20))
        XCTAssertEqual(variables["limitCount"], .number(20))
    }

    func testCorrectingALegSendsTheWayAndClearingSendsNull() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SetTripLegTravelMode":\#(legAnswer)}}"#)
        let leg = try await connection.setTripLegTravelMode(tripLegId: "leg-1", travelMode: .bus)
        XCTAssertEqual(leg.travelMode, "bus")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["tripLegId": .string("leg-1"), "travelMode": .string("bus")])
        _ = try await connection.setTripLegTravelMode(tripLegId: "leg-1", travelMode: nil)
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["tripLegId": .string("leg-1"), "travelMode": .null])
    }

    func testReadsTravelTotalsForAPeriod() async throws {
        let body = #"""
            {"data":{"ListTravelTotals":[{"periodStartDay":"2024-01-01","travelMode":"car","distanceMeters":11800,"durationSeconds":1080,"legCount":1},
             {"periodStartDay":"2024-01-01","travelMode":"walk","distanceMeters":600,"durationSeconds":480,"legCount":2}]}}
            """#
        let (connection, fake) = connection(answering: body)
        let totals = try await connection.travelTotals(period: .week, from: date("2024-01-01T00:00:00Z"), until: date("2024-01-08T00:00:00Z"))
        XCTAssertEqual(totals.count, 2)
        XCTAssertEqual(totals[1].legCount, 2)
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["period"], .string("week"))
        XCTAssertEqual(variables["from"], .string("2024-01-01T00:00:00Z"))
        XCTAssertNil(variables["userId"])
        _ = try await connection.travelTotals(userId: "user-2", period: .year, from: date("2023-01-01T00:00:00Z"), until: date("2024-01-01T00:00:00Z"))
        XCTAssertEqual(try sentVariables(fake.sent[1])["userId"], .string("user-2"))
    }

    func testTotalsAreGatheredByPeriodNewestFirstWithTheFarthestWayFirst() {
        let totals = [
            TravelTotal(periodStartDay: "2024-01-01", travelMode: "walk", distanceMeters: 600, durationSeconds: 480, legCount: 2),
            TravelTotal(periodStartDay: "2024-01-08", travelMode: "bike", distanceMeters: 5000, durationSeconds: 1200, legCount: 1),
            TravelTotal(periodStartDay: "2024-01-01", travelMode: "car", distanceMeters: 11800, durationSeconds: 1080, legCount: 1),
            TravelTotal(periodStartDay: "2024-01-08", travelMode: "bus", distanceMeters: 0, durationSeconds: 0, legCount: 0),
        ]
        let periods = TravelPeriods.group(totals)
        XCTAssertEqual(periods.map(\.periodStartDay), ["2024-01-08", "2024-01-01"])
        XCTAssertEqual(periods[0].totals.map(\.travelMode), ["bike"])
        XCTAssertEqual(periods[1].totals.map(\.travelMode), ["car", "walk"])
        XCTAssertEqual(periods[1].distanceMeters, 12400)
        XCTAssertEqual(periods[1].durationSeconds, 1560)
    }

    func testAPeriodStartsAtMidnightOfItsFirstDay() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "UTC"))
        let period = TravelPeriod(periodStartDay: "2024-01-08", totals: [])
        XCTAssertEqual(period.startDate(calendar: calendar), date("2024-01-08T00:00:00Z"))
        XCTAssertNil(TravelPeriod(periodStartDay: "soon", totals: []).startDate(calendar: calendar))
    }

    func testPagesOfPeriodsEndWithTheCurrentOneAndMoveBackWholePages() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "UTC"))
        // A Wednesday.
        let now = date("2024-01-10T15:00:00Z")
        let week = TravelPeriodKind.week.pageRange(pageBack: 0, now: now, calendar: calendar)
        XCTAssertEqual(week.until, date("2024-01-15T00:00:00Z"))
        XCTAssertEqual(week.from, date("2023-10-23T00:00:00Z"))
        let earlier = TravelPeriodKind.week.pageRange(pageBack: 1, now: now, calendar: calendar)
        XCTAssertEqual(earlier.until, week.from)
        let day = TravelPeriodKind.day.pageRange(pageBack: 0, now: now, calendar: calendar)
        XCTAssertEqual(day.until, date("2024-01-11T00:00:00Z"))
        XCTAssertEqual(day.from, date("2023-12-28T00:00:00Z"))
        let month = TravelPeriodKind.month.pageRange(pageBack: 0, now: now, calendar: calendar)
        XCTAssertEqual(month.until, date("2024-02-01T00:00:00Z"))
        XCTAssertEqual(month.from, date("2023-02-01T00:00:00Z"))
        let year = TravelPeriodKind.year.pageRange(pageBack: 0, now: now, calendar: calendar)
        XCTAssertEqual(year.until, date("2025-01-01T00:00:00Z"))
        XCTAssertEqual(year.from, date("2020-01-01T00:00:00Z"))
    }

    func testTheTravelDocumentsAreRegistered() {
        for name in ["trips", "setTripLegTravelMode", "travelTotals"] {
            XCTAssertNotNil(Documents.travelDocuments[name], name)
        }
        XCTAssertTrue(Documents.reportLocation.contains("activities: $activities"))
    }

    // MARK: The outbox

    func testAnActivityIsQueuedOncePerStartAndKeptInOrder() {
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addActivities([activity(600, kind: "automotive"), activity(0), activity(300, kind: "running")])
        XCTAssertEqual(outbox.activities.map(\.activityKind), ["walking", "running", "automotive"])
        XCTAssertFalse(outbox.isEmpty)
        // Asking again from where it last asked brings the same ones back, and
        // iOS times have fractions the server and the file drop.
        outbox.addActivities([activity(300.7, kind: "running"), activity(900, kind: "stationary")])
        XCTAssertEqual(outbox.activities.map(\.activityKind), ["walking", "running", "automotive", "stationary"])
    }

    func testAnActivityTheServerWouldRefuseIsNotQueued() {
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addActivities([
            activity(0, kind: "flying"), activity(10, confidence: "certain"),
            ReportedActivity(startedAt: Date().addingTimeInterval(2 * 86_400), activityKind: "walking", activityConfidence: "low"),
        ])
        XCTAssertTrue(outbox.activities.isEmpty)
        XCTAssertTrue(outbox.isEmpty)
    }

    func testTheOutboxKeepsTheNewest5000Activities() {
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addActivities((0..<(LocationOutbox.activityCountMost + 7)).map { activity(Double($0) * 10) })
        XCTAssertEqual(outbox.activities.count, LocationOutbox.activityCountMost)
        XCTAssertEqual(outbox.activities.first?.startedAt, date("2024-01-02T00:00:00Z").addingTimeInterval(70))
    }

    func testActivitiesLeaveInBatchesOf500AndStayUntilTheServerAnswered() {
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addActivities((0..<620).map { activity(Double($0) * 10) })
        outbox.addPoint(ReportedPoint(latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: date("2024-01-02T00:00:00Z")))
        let first = outbox.nextBatch()
        XCTAssertEqual(first.activities.count, 500)
        XCTAssertEqual(first.points.count, 1)
        // What is added while the batch is on its way stays.
        outbox.addActivities([activity(99_000)])
        outbox.remove(first)
        XCTAssertTrue(outbox.points.isEmpty)
        let second = outbox.nextBatch()
        XCTAssertEqual(second.activities.count, 121)
        XCTAssertEqual(second.activities.first?.startedAt, date("2024-01-02T00:00:00Z").addingTimeInterval(5_000))
        outbox.remove(second)
        XCTAssertTrue(outbox.isEmpty)
    }

    func testReportingActivitiesSendsThemWithTheirTimes() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"ReportLocation":{"newPointCount":0,"newVisitCount":0,"updatedVisitCount":0}}}"#)
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addActivities([activity(0, kind: "cycling", confidence: "medium")])
        _ = try await connection.reportLocation(outbox.nextBatch())
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(
            variables["activities"],
            .array([
                .object([
                    "startedAt": .string("2024-01-02T00:00:00Z"), "activityKind": .string("cycling"), "activityConfidence": .string("medium"),
                ])
            ]))
        XCTAssertEqual(variables["points"], .array([]))
        XCTAssertEqual(variables["visits"], .array([]))
    }

    func testDiscardingWhatIsUnsentDiscardsActivitiesToo() {
        var outbox = LocationOutbox(accountKey: "account")
        outbox.addActivities([activity(3600), activity(30 * 3600)])
        outbox.discardUnsent(from: date("2024-01-02T00:00:00Z"), until: date("2024-01-03T00:00:00Z"))
        XCTAssertEqual(outbox.activities.count, 1)
        outbox.discardUnsent()
        XCTAssertTrue(outbox.activities.isEmpty)
    }

    func testAnOutboxKeptBeforeActivitiesStillReadsBack() throws {
        let old = #"{"accountKey":"account","nextSequence":3,"points":[],"visits":[],"openRegionVisits":{}}"#
        let outbox = try LocationOutbox.decoded(from: Data(old.utf8))
        XCTAssertEqual(outbox.accountKey, "account")
        XCTAssertTrue(outbox.activities.isEmpty)
        var next = outbox
        next.addActivities([activity(0)])
        let readBack = try LocationOutbox.decoded(from: next.encoded())
        XCTAssertEqual(readBack.activities, next.activities)
        XCTAssertEqual(readBack, next)
    }

    func testABatchWithActivitiesIsSplitUntilTheOneEntryIsFound() throws {
        func sequenced(_ index: Int) -> ReportedActivity {
            ReportedActivity(sequence: index, startedAt: Date(timeIntervalSince1970: Double(index)), activityKind: "walking", activityConfidence: "low")
        }
        let point = ReportedPoint(sequence: 1, latitudeDegrees: 10.5, longitudeDegrees: 20.5, accuracyMeters: 5, measuredAt: Date())
        let batch = LocationBatch(points: [point], visits: [], activities: (2...5).map(sequenced))
        XCTAssertEqual(batch.entryCount, 5)
        let apart = try XCTUnwrap(batch.split())
        XCTAssertEqual(apart.first.points.count, 1)
        XCTAssertTrue(apart.first.activities.isEmpty)
        XCTAssertEqual(apart.second.activities.count, 4)
        let halves = try XCTUnwrap(apart.second.split())
        XCTAssertEqual(halves.first.activities.map(\.sequence), [2, 3])
        XCTAssertEqual(halves.second.activities.map(\.sequence), [4, 5])
        XCTAssertNil(LocationBatch(points: [], visits: [], activities: [sequenced(1)]).split())
    }

    // MARK: The timeline

    private func visit(_ id: String, from arrivedAt: String, to departedAt: String?) -> PlaceVisit {
        let place = LocationPlace(id: "place-1", placeName: "Fixture Park", latitudeDegrees: 10.5, longitudeDegrees: 20.5)
        return PlaceVisit(id: id, place: place, arrivedAt: arrivedAt, departedAt: departedAt, latitudeDegrees: 10.5, longitudeDegrees: 20.5)
    }

    private func trip(_ id: String, from startedAt: String, to endedAt: String) -> Trip {
        Trip(
            id: id, startedAt: startedAt, endedAt: endedAt, durationSeconds: 600, distanceMeters: 1200,
            legs: [TripLeg(id: "\(id)-leg", startedAt: startedAt, endedAt: endedAt, durationSeconds: 600, distanceMeters: 1200, travelMode: "walk")])
    }

    func testATripSitsBetweenTheTwoVisitsItJoinsOnTheDayItBegan() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "UTC"))
        let visits = [
            visit("visit-1", from: "2024-01-02T09:00:00Z", to: "2024-01-02T10:00:00Z"),
            visit("visit-2", from: "2024-01-02T10:20:00Z", to: nil),
        ]
        let trips = [trip("trip-1", from: "2024-01-02T10:00:00Z", to: "2024-01-02T10:20:00Z")]
        let days = LocationDays.group(visits, trips: trips, calendar: calendar)
        XCTAssertEqual(days.count, 1)
        XCTAssertEqual(days[0].entries.map(\.id), ["visit:visit-2", "trip:trip-1", "visit:visit-1"])
        XCTAssertEqual(days[0].visits.map(\.id), ["visit-2", "visit-1"])
        let stretch = LocationDays.entries(visits: visits, pauses: [], trips: trips)
        XCTAssertEqual(stretch.map(\.id), ["visit:visit-2", "trip:trip-1", "visit:visit-1"])
    }

    func testATripOfADayWithNoVisitStillMakesTheDay() throws {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = try XCTUnwrap(TimeZone(identifier: "UTC"))
        let days = LocationDays.group(
            [visit("visit-1", from: "2024-01-01T20:00:00Z", to: "2024-01-02T07:00:00Z")],
            trips: [trip("trip-1", from: "2024-01-02T07:00:00Z", to: "2024-01-02T07:20:00Z")], calendar: calendar)
        XCTAssertEqual(days.map { $0.entries.map(\.id) }, [["trip:trip-1"], ["visit:visit-1"]])
    }

    func testTripsOfARangeAreThoseThatBeganInItNewestFirst() {
        let trips = [
            trip("early", from: "2024-01-02T08:00:00Z", to: "2024-01-02T08:10:00Z"),
            trip("late", from: "2024-01-02T17:00:00Z", to: "2024-01-02T17:10:00Z"),
            trip("next-day", from: "2024-01-03T00:00:00Z", to: "2024-01-03T00:10:00Z"),
        ]
        let kept = TravelPeriods.trips(trips, from: date("2024-01-02T00:00:00Z"), until: date("2024-01-03T00:00:00Z"))
        XCTAssertEqual(kept.map(\.id), ["late", "early"])
    }
}
