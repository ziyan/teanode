import Foundation
import XCTest

@testable import TeaNodeKit

final class PhotoOverviewTests: XCTestCase {
    private let utc = TimeZone(secondsFromGMT: 0)!
    private let english = Locale(identifier: "en_US")

    private func photo(_ id: String, takenAt: String?, offsetMinutes: Int? = 0, smallPreviewUrl: String = "", createdAt: String = "2026-01-01T00:00:00Z") throws -> Photo {
        let takenAtField = takenAt.map { #""\#($0)""# } ?? "null"
        let offsetField = offsetMinutes.map { String($0) } ?? "null"
        let json =
            #"{"id":"\#(id)","createdAt":"\#(createdAt)","photoKind":"picture","takenAt":\#(takenAtField),"takenAtOffsetMinutes":\#(offsetField),"widthPixels":400,"heightPixels":300,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"a.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"\#(smallPreviewUrl)","largePreviewUrl":"","videoPreviewUrl":""}"#
        return try JSONDecoder().decode(Photo.self, from: Data(json.utf8))
    }

    func testPhotosAreGroupedInMonthsInTheOrderOfTheList() throws {
        let photos = [
            try photo("a", takenAt: "2026-10-15T10:00:00Z"),
            try photo("b", takenAt: "2026-10-02T10:00:00Z"),
            try photo("c", takenAt: "2026-09-30T10:00:00Z"),
            try photo("d", takenAt: "2025-10-30T10:00:00Z"),
        ]
        let months = PhotoOverview.months(photos, phoneTimeZone: utc)
        XCTAssertEqual(months.map(\.id), ["2026-10", "2026-09", "2025-10"])
        XCTAssertEqual(months.map { $0.photos.map(\.id) }, [["a", "b"], ["c"], ["d"]])
        XCTAssertEqual(months.map(\.year), [2026, 2026, 2025])
        XCTAssertEqual(months.map(\.month), [10, 9, 10])
    }

    func testAMonthIsReadOnTheWallClockWhereThePhotoWasTaken() throws {
        // 23:30 UTC on the last day of September is already October in Tokyo.
        let inTokyo = try photo("tokyo", takenAt: "2026-09-30T23:30:00Z", offsetMinutes: 540)
        let inLosAngeles = try photo("la", takenAt: "2026-09-30T23:30:00Z", offsetMinutes: -480)
        let months = PhotoOverview.months([inTokyo, inLosAngeles], phoneTimeZone: utc)
        XCTAssertEqual(months.map(\.id), ["2026-10", "2026-09"])
        XCTAssertEqual(months.map { $0.photos.map(\.id) }, [["tokyo"], ["la"]])
    }

    func testPhotosAreGroupedInYears() throws {
        let photos = [
            try photo("a", takenAt: "2026-10-15T10:00:00Z"),
            try photo("b", takenAt: "2026-01-02T10:00:00Z"),
            try photo("c", takenAt: "2025-12-31T10:00:00Z"),
        ]
        let years = PhotoOverview.years(photos, phoneTimeZone: utc)
        XCTAssertEqual(years.map(\.id), ["2026", "2025"])
        XCTAssertEqual(years.map { $0.photos.map(\.id) }, [["a", "b"], ["c"]])
        XCTAssertEqual(years.map(\.year), [2026, 2025])
    }

    func testPhotosWithNoDateAtAllAreAGroupOfTheirOwn() throws {
        let undated = try photo("x", takenAt: nil, offsetMinutes: nil, createdAt: "not a time")
        let months = PhotoOverview.months([try photo("a", takenAt: "2026-10-15T10:00:00Z"), undated], phoneTimeZone: utc)
        XCTAssertEqual(months.map(\.id), ["2026-10", "unknown"])
        XCTAssertNil(months[1].titleText(locale: english))
        XCTAssertNil(PhotoOverview.years([undated], phoneTimeZone: utc).first?.titleText(locale: english))
        XCTAssertNil(PhotoOverview.dateRangeText(of: [undated], phoneTimeZone: utc, locale: english))
    }

    func testTheCoverIsTheNewestPhotoThatHasAPreview() throws {
        let photos = [
            try photo("a", takenAt: "2026-10-15T10:00:00Z"),
            try photo("b", takenAt: "2026-10-14T10:00:00Z", smallPreviewUrl: "/small/b"),
            try photo("c", takenAt: "2026-10-13T10:00:00Z", smallPreviewUrl: "/small/c"),
        ]
        XCTAssertEqual(PhotoOverview.months(photos, phoneTimeZone: utc).first?.coverPhoto?.id, "b")
        XCTAssertEqual(PhotoOverview.years(photos, phoneTimeZone: utc).first?.coverPhoto?.id, "b")
        let waiting = [try photo("a", takenAt: "2026-10-15T10:00:00Z")]
        XCTAssertEqual(PhotoOverview.months(waiting, phoneTimeZone: utc).first?.coverPhoto?.id, "a")
    }

    func testMonthsAndYearsAreTitledByTheirName() throws {
        let photos = [try photo("a", takenAt: "2026-10-15T10:00:00Z")]
        XCTAssertEqual(PhotoOverview.months(photos, phoneTimeZone: utc).first?.titleText(locale: english), "October 2026")
        XCTAssertEqual(PhotoOverview.years(photos, phoneTimeZone: utc).first?.titleText(locale: english), "2026")
    }

    func testTheDateRangeNamesTheFirstAndLastDay() throws {
        let photos = [
            try photo("a", takenAt: "2026-10-06T10:00:00Z"),
            try photo("b", takenAt: "2026-10-05T10:00:00Z"),
            try photo("c", takenAt: "2026-10-03T10:00:00Z"),
        ]
        let text = try XCTUnwrap(PhotoOverview.dateRangeText(of: photos, phoneTimeZone: utc, locale: english))
        XCTAssertTrue(text.hasPrefix("Oct 3"), text)
        XCTAssertTrue(text.hasSuffix("6, 2026"), text)
        // The order of the photos given does not matter.
        XCTAssertEqual(PhotoOverview.dateRangeText(of: photos.reversed(), phoneTimeZone: utc, locale: english), text)
    }

    func testTheDateRangeOfOneDayIsThatDay() throws {
        let photos = [try photo("a", takenAt: "2026-10-03T22:00:00Z"), try photo("b", takenAt: "2026-10-03T01:00:00Z")]
        XCTAssertEqual(PhotoOverview.dateRangeText(of: photos, phoneTimeZone: utc, locale: english), "Oct 3, 2026")
    }

    func testTheDateRangeSpansMonthsAndYears() throws {
        let photos = [try photo("a", takenAt: "2026-01-02T10:00:00Z"), try photo("b", takenAt: "2025-12-30T10:00:00Z")]
        let text = try XCTUnwrap(PhotoOverview.dateRangeText(of: photos, phoneTimeZone: utc, locale: english))
        XCTAssertTrue(text.contains("Dec 30, 2025"), text)
        XCTAssertTrue(text.contains("Jan 2, 2026"), text)
    }

    func testThereIsNoDateRangeForNoPhotos() {
        XCTAssertNil(PhotoOverview.dateRangeText(of: [], phoneTimeZone: utc, locale: english))
    }

    func testTheViewerTitleIsTheDayAndTheTimeWhereThePhotoWasTaken() throws {
        // 23:52 UTC on the 4th is 08:52 on the 5th in Tokyo.
        let inTokyo = try photo("tokyo", takenAt: "2026-10-04T23:52:00Z", offsetMinutes: 540)
        let now = try XCTUnwrap(ServerTime.date("2026-10-17T10:00:00Z"))
        XCTAssertEqual(PhotoTimeline.viewerDayText(of: inTokyo, now: now, phoneTimeZone: utc, locale: english), "October 5")
        let time = try XCTUnwrap(PhotoTimeline.viewerTimeText(of: inTokyo, phoneTimeZone: utc, locale: english))
        XCTAssertTrue(time.hasPrefix("8:52"), time)
    }

    func testTheViewerTitleHasTheYearOnlyWhenItIsNotThisYear() throws {
        let older = try photo("older", takenAt: "2025-10-05T10:00:00Z")
        let now = try XCTUnwrap(ServerTime.date("2026-10-17T10:00:00Z"))
        XCTAssertEqual(PhotoTimeline.viewerDayText(of: older, now: now, phoneTimeZone: utc, locale: english), "October 5, 2025")
    }
}
