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

    func testPhotosWithNoDateAtAllHaveNoDateRange() throws {
        let undated = try photo("x", takenAt: nil, offsetMinutes: nil, createdAt: "not a time")
        XCTAssertNil(PhotoOverview.dateRangeText(of: [undated], phoneTimeZone: utc, locale: english))
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
