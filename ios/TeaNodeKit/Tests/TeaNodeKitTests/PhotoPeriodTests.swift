import Foundation
import XCTest

@testable import TeaNodeKit

final class PhotoPeriodTests: XCTestCase {
    private let utc = TimeZone(secondsFromGMT: 0)!
    private let tokyo = TimeZone(identifier: "Asia/Tokyo")!
    private let english = Locale(identifier: "en_US")

    private func photo(_ id: String, takenAt: String?, createdAt: String = "2026-01-01T00:00:00Z") throws -> Photo {
        let takenAtField = takenAt.map { #""\#($0)""# } ?? "null"
        let json =
            #"{"id":"\#(id)","createdAt":"\#(createdAt)","photoKind":"picture","takenAt":\#(takenAtField),"takenAtOffsetMinutes":null,"widthPixels":400,"heightPixels":300,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"a.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":""}"#
        return try JSONDecoder().decode(Photo.self, from: Data(json.utf8))
    }

    private func moment(_ text: String) -> Date { ServerTime.date(text)! }

    // MARK: Periods

    func testAYearRunsFromItsFirstMomentToTheFirstMomentOfTheNext() throws {
        let year = try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: utc))
        XCTAssertEqual(year.id, "2030")
        XCTAssertEqual(year.takenAfter, moment("2030-01-01T00:00:00Z"))
        XCTAssertEqual(year.takenBefore, moment("2031-01-01T00:00:00Z"))
    }

    func testAMonthEndsWhereTheNextBeginsAndDecemberEndsTheYear() throws {
        let february = try XCTUnwrap(PhotoPeriod(year: 2028, month: 2, timeZone: utc))
        XCTAssertEqual(february.id, "2028-02")
        XCTAssertEqual(february.takenBefore, moment("2028-03-01T00:00:00Z"), "2028 has a 29th of February")
        let december = try XCTUnwrap(PhotoPeriod(year: 2030, month: 12, timeZone: utc))
        XCTAssertEqual(december.takenAfter, moment("2030-12-01T00:00:00Z"))
        XCTAssertEqual(december.takenBefore, moment("2031-01-01T00:00:00Z"))
        XCTAssertNil(PhotoPeriod(year: 2030, month: 13, timeZone: utc))
        XCTAssertNil(PhotoPeriod(year: 2030, month: 0, timeZone: utc))
    }

    func testAPeriodIsReadOnTheCalendarOfThePhone() throws {
        // Midnight in Tokyo is 15:00 the day before in UTC.
        let year = try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: tokyo))
        XCTAssertEqual(year.takenAfter, moment("2029-12-31T15:00:00Z"))
        XCTAssertEqual(year.takenBefore, moment("2030-12-31T15:00:00Z"))
    }

    func testTheYearsRunFromTheNewestToTheOldestAndTheMonthsFromDecemberBack() {
        XCTAssertEqual(PhotoPeriod.years(newestYear: 2031, oldestYear: 2029), [2031, 2030, 2029])
        XCTAssertEqual(PhotoPeriod.years(newestYear: 2030, oldestYear: 2030), [2030])
        XCTAssertEqual(PhotoPeriod.monthsNewestFirst.first, 12)
        XCTAssertEqual(PhotoPeriod.monthsNewestFirst.last, 1)
        let months = PhotoPeriod.months(newestYear: 2031, oldestYear: 2030, timeZone: utc)
        XCTAssertEqual(months.count, 24)
        XCTAssertEqual(months.first?.id, "2031-12")
        XCTAssertEqual(months[12].id, "2030-12")
        XCTAssertEqual(months.last?.id, "2030-01")
    }

    func testAPeriodHasANameThatIsNeverThousandsSeparated() throws {
        XCTAssertEqual(try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: utc)).titleText(locale: english), "2030")
        XCTAssertEqual(try XCTUnwrap(PhotoPeriod(year: 2030, month: 7, timeZone: utc)).titleText(locale: english), "July 2030")
    }

    func testThePlaceOfAPhotoInTheTimelineIsWhenItWasTakenOrElseWhenItArrived() throws {
        XCTAssertEqual(PhotoTimeline.timelineMoment(of: try photo("a", takenAt: "2030-05-06T07:08:09Z")), moment("2030-05-06T07:08:09Z"))
        XCTAssertEqual(PhotoTimeline.timelineMoment(of: try photo("b", takenAt: nil, createdAt: "2029-02-03T04:05:06Z")), moment("2029-02-03T04:05:06Z"))
        XCTAssertNil(PhotoTimeline.timelineMoment(of: try photo("c", takenAt: nil, createdAt: "not a time")))
        // The year is the one the phone's calendar says for the moment.
        let lateNewYearsEve = moment("2029-12-31T16:00:00Z")
        XCTAssertEqual(PhotoPeriod.year(of: lateNewYearsEve, timeZone: utc), 2029)
        XCTAssertEqual(PhotoPeriod.year(of: lateNewYearsEve, timeZone: tokyo), 2030)
    }

    // MARK: Narrowing a list to a period

    func testAListKeptToAPeriodAsksForItsTwoEnds() throws {
        let list = PhotoListQuery(collection: .favorites, filter: PhotoFilter(photoKindFilter: .video))
        let period = try XCTUnwrap(PhotoPeriod(year: 2030, month: 7, timeZone: utc))
        let narrowed = try XCTUnwrap(list.narrowed(to: period, calendar: calendar))
        let sent = narrowed.variables(calendar: calendar)
        XCTAssertEqual(sent["takenAfter"], .string("2030-07-01T00:00:00Z"))
        XCTAssertEqual(sent["takenBefore"], .string("2030-08-01T00:00:00Z"))
        XCTAssertEqual(sent["isFavorite"], .bool(true))
        XCTAssertEqual(sent["photoKind"], .string("video"))
    }

    func testAPeriodOutsideTheDaysOfTheListHasNothingInIt() throws {
        let day = moment("2030-03-10T12:00:00Z")
        let list = PhotoListQuery(filter: PhotoFilter(firstDay: day, lastDay: day))
        XCTAssertNil(list.narrowed(to: try XCTUnwrap(PhotoPeriod(year: 2031, timeZone: utc)), calendar: calendar))
        // A period that holds the days is cut to them.
        let narrowed = try XCTUnwrap(list.narrowed(to: try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: utc)), calendar: calendar))
        let sent = narrowed.variables(calendar: calendar)
        XCTAssertEqual(sent["takenAfter"], .string("2030-03-10T00:00:00Z"))
        XCTAssertEqual(sent["takenBefore"], .string("2030-03-11T00:00:00Z"))
    }

    func testThePhotosNewerThanAPeriodAreThoseAfterItsEnd() throws {
        let period = try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: utc))
        let list = PhotoListQuery()
        let newer = try XCTUnwrap(list.newer(than: period, calendar: calendar))
        XCTAssertEqual(newer.variables(calendar: calendar)["takenAfter"], .string("2031-01-01T00:00:00Z"))
        XCTAssertNil(newer.variables(calendar: calendar)["takenBefore"])
    }

    func testThePhotosNewerThanAPeriodKeepTheLaterStartOfTheList() throws {
        let period = try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: utc))
        let start = moment("2032-06-01T00:00:00Z")
        let list = PhotoListQuery(filter: PhotoFilter(takenAfter: start))
        XCTAssertEqual(try XCTUnwrap(list.newer(than: period, calendar: calendar)).variables(calendar: calendar)["takenAfter"], .string("2032-06-01T00:00:00Z"))
    }

    func testNothingIsNewerWhenTheListEndsBeforeThePeriodDoes() throws {
        let period = try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: utc))
        let list = PhotoListQuery(filter: PhotoFilter(takenBefore: moment("2030-09-01T00:00:00Z")))
        XCTAssertNil(list.newer(than: period, calendar: calendar))
        // A list that ends later keeps its end, so photos after it are not counted.
        let later = PhotoListQuery(filter: PhotoFilter(takenBefore: moment("2033-01-01T00:00:00Z")))
        XCTAssertEqual(try XCTUnwrap(later.newer(than: period, calendar: calendar)).variables(calendar: calendar)["takenBefore"], .string("2033-01-01T00:00:00Z"))
    }

    private var calendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = utc
        return calendar
    }

    // MARK: Asking the server

    private func connection(answering answer: @escaping @Sendable (_ variables: [String: Any], _ query: String) -> String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { request in
            let sent = (try? JSONSerialization.jsonObject(with: request.httpBody ?? Data())) as? [String: Any] ?? [:]
            return (200, answer(sent["variables"] as? [String: Any] ?? [:], sent["query"] as? String ?? ""))
        }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    fileprivate static func photoObject(_ id: String, takenAt: String) -> String {
        #"{"id":"\#(id)","createdAt":"2026-01-01T00:00:00Z","photoKind":"picture","takenAt":"\#(takenAt)","takenAtOffsetMinutes":null,"widthPixels":400,"heightPixels":300,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"a.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":""}"#
    }

    func testAPageIsReadByItsPlaceAndAListIsCounted() async throws {
        let (connection, fake) = try connection { _, query in
            query.contains("CountPhotos") ? #"{"data":{"CountPhotos":4321}}"# : #"{"data":{"ListPhotos":[]}}"#
        }
        let list = PhotoListQuery(collection: .favorites)
        _ = try await connection.photos(in: list, offset: 400, pageSize: 200)
        XCTAssertEqual(try await connection.countPhotos(in: list), 4321)
        let variables = try fake.sent.map { request -> [String: Any] in
            let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
            return try XCTUnwrap(sent["variables"] as? [String: Any])
        }
        XCTAssertEqual(variables[0]["offset"] as? Int, 400)
        XCTAssertEqual(variables[0]["pageSize"] as? Int, 200)
        XCTAssertEqual(variables[0]["isFavorite"] as? Bool, true)
        XCTAssertNil(variables[0]["beforePhotoId"])
        XCTAssertEqual(variables[1]["isFavorite"] as? Bool, true)
        XCTAssertNil(variables[1]["pageSize"], "a count has no page")
    }

    func testTheYearsOfAListAreThoseOfItsNewestAndOldestPhotos() async throws {
        let (connection, _) = try connection { variables, query in
            if query.contains("CountPhotos") { return #"{"data":{"CountPhotos":900}}"# }
            let offset = variables["offset"] as? Int ?? 0
            return #"{"data":{"ListPhotos":["# + (offset == 0 ? Self.photoObject("new", takenAt: "2031-04-01T00:00:00Z") : Self.photoObject("old", takenAt: "2027-09-01T00:00:00Z")) + "]}}"
        }
        let span = try await connection.photoYearSpan(of: PhotoListQuery(), timeZone: utc)
        XCTAssertEqual(span, PhotoYearSpan(newestYear: 2031, oldestYear: 2027))
        XCTAssertEqual(span.map { PhotoPeriod.years(newestYear: $0.newestYear, oldestYear: $0.oldestYear) }, [2031, 2030, 2029, 2028, 2027])
    }

    func testAnEmptyListHasNoYears() async throws {
        let (connection, fake) = try connection { _, _ in #"{"data":{"CountPhotos":0}}"# }
        let span = try await connection.photoYearSpan(of: PhotoListQuery(), timeZone: utc)
        XCTAssertNil(span)
        XCTAssertEqual(fake.sent.count, 1, "nothing else is asked once the count is zero")
    }

    func testAPeriodIsShownByItsNewestPhotoAndCounted() async throws {
        let (connection, fake) = try connection { _, query in
            if query.contains("CountPhotos") { return #"{"data":{"CountPhotos":37}}"# }
            return #"{"data":{"ListPhotos":["# + Self.photoObject("cover", takenAt: "2030-07-30T00:00:00Z") + "]}}"
        }
        let period = try XCTUnwrap(PhotoPeriod(year: 2030, month: 7, timeZone: utc))
        let summary = try await connection.photoPeriodSummary(of: PhotoListQuery(), period: period)
        XCTAssertEqual(summary?.coverPhoto.id, "cover")
        XCTAssertEqual(summary?.photoCount, 37)
        XCTAssertEqual(summary?.period, period)
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.first?.httpBody)) as? [String: Any])
        let variables = try XCTUnwrap(sent["variables"] as? [String: Any])
        XCTAssertNotNil(variables["takenAfter"] as? String)
        XCTAssertNotNil(variables["takenBefore"] as? String)
    }

    func testAPeriodWithNoPhotoHasNoSummary() async throws {
        let (connection, _) = try connection { _, query in
            query.contains("CountPhotos") ? #"{"data":{"CountPhotos":0}}"# : #"{"data":{"ListPhotos":[]}}"#
        }
        let period = try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: utc))
        let summary = try await connection.photoPeriodSummary(of: PhotoListQuery(), period: period)
        XCTAssertNil(summary)
    }

    func testThePlaceOfAPeriodIsTheCountOfThePhotosNewerThanIt() async throws {
        let (connection, fake) = try connection { _, _ in #"{"data":{"CountPhotos":812}}"# }
        let period = try XCTUnwrap(PhotoPeriod(year: 2030, timeZone: utc))
        let place = try await connection.countPhotos(in: PhotoListQuery(), newerThan: period)
        XCTAssertEqual(place, 812)
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.first?.httpBody)) as? [String: Any])
        let variables = try XCTUnwrap(sent["variables"] as? [String: Any])
        XCTAssertEqual(variables["takenAfter"] as? String, "2031-01-01T00:00:00Z")
        // A list that ends before the period has nothing newer, and is not asked.
        let none = try await connection.countPhotos(in: PhotoListQuery(filter: PhotoFilter(takenBefore: moment("2030-02-01T00:00:00Z"))), newerThan: period)
        XCTAssertEqual(none, 0)
        XCTAssertEqual(fake.sent.count, 1)
    }
}
