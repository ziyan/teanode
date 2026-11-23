import Foundation
import XCTest

@testable import TeaNodeKit

final class PhotoWindowTests: XCTestCase {
    private func photo(_ id: String, isWaiting: Bool = false) throws -> Photo {
        let state = isWaiting ? "waiting" : "ready"
        let json =
            #"{"id":"\#(id)","createdAt":"2026-03-01T00:00:00Z","photoKind":"picture","takenAt":null,"takenAtOffsetMinutes":null,"widthPixels":400,"heightPixels":300,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"a.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"\#(state)","trashedAt":null,"smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":""}"#
        return try JSONDecoder().decode(Photo.self, from: Data(json.utf8))
    }

    /// Photos named p0, p1, ... for the places from `first` up to but not including `end`.
    private func photos(_ first: Int, _ end: Int) throws -> [Photo] {
        try (first..<end).map { try photo("p\($0)") }
    }

    func testAPhotoIsFoundByItsPlaceOnTheHeldPage() throws {
        var window = PhotoWindow(totalCount: 25, pageSize: 10)
        window.setPage(try photos(10, 20), at: 1)
        XCTAssertEqual(window.photo(at: 10)?.id, "p10")
        XCTAssertEqual(window.photo(at: 19)?.id, "p19")
        XCTAssertNil(window.photo(at: 9), "page 0 is not held")
        XCTAssertNil(window.photo(at: 20), "page 2 is not held")
        XCTAssertNil(window.photo(at: -1))
        XCTAssertNil(window.photo(at: 25), "there is no place after the last")
        XCTAssertEqual(window.pageIndex(of: 19), 1)
        XCTAssertEqual(window.pageIndex(of: 20), 2)
        XCTAssertEqual(window.pageCount, 3)
    }

    func testAPageAfterTheLastPhotoIsNotHeld() throws {
        var window = PhotoWindow(totalCount: 20, pageSize: 10)
        window.setPage(try photos(0, 10), at: 5)
        XCTAssertNil(window.pages[5])
    }

    func testThePagesToReadAreTheMissingOnesNearThePlaceOnScreen() throws {
        var window = PhotoWindow(totalCount: 100, pageSize: 10)
        XCTAssertEqual(window.missingPages(around: 55, reach: 2), [5, 4, 6, 3, 7])
        window.setPage(try photos(50, 60), at: 5)
        window.setPage(try photos(60, 70), at: 6)
        XCTAssertEqual(window.missingPages(around: 55, reach: 2), [4, 3, 7])
        XCTAssertEqual(window.missingPages(around: 3, reach: 2), [0, 1, 2], "the pages before the first are not asked for")
    }

    func testThePagesToReadStopAtTheEndsOfTheList() {
        let window = PhotoWindow(totalCount: 25, pageSize: 10)
        XCTAssertEqual(window.missingPages(around: 0, reach: 2), [0, 1, 2])
        XCTAssertEqual(window.missingPages(around: 24, reach: 1), [2, 1])
        XCTAssertEqual(window.missingPages(around: 9_999, reach: 1), [2, 1], "a place past the end counts as the last page")
        XCTAssertEqual(PhotoWindow(totalCount: 0, pageSize: 10).missingPages(around: 0), [])
    }

    func testPagesFarFromThePlaceOnScreenAreLetGo() throws {
        var window = PhotoWindow(totalCount: 200, pageSize: 10)
        for pageIndex in [0, 1, 7, 8, 9, 10, 11, 19] { window.setPage(try photos(pageIndex * 10, pageIndex * 10 + 10), at: pageIndex) }
        XCTAssertEqual(window.pagesToRelease(around: 95, keep: 2), [0, 1, 7, 19])
        XCTAssertEqual(window.pagesToRelease(around: 95, keep: 4), [0, 1, 19])
        window.release([0, 1])
        XCTAssertNil(window.photo(at: 5))
        XCTAssertEqual(window.photo(at: 85)?.id, "p85")
    }

    func testAWindowThatHoldsTheWholeListNeverLetsGoOfIt() throws {
        let window = PhotoWindow(whole: try photos(0, 30))
        XCTAssertEqual(window.totalCount, 30)
        XCTAssertEqual(window.photo(at: 29)?.id, "p29")
        XCTAssertEqual(window.pagesToRelease(around: 0, keep: 0), [])
        XCTAssertEqual(window.missingPages(around: 15), [])
        XCTAssertEqual(PhotoWindow(whole: []).totalCount, 0)
        XCTAssertNil(PhotoWindow(whole: []).photo(at: 0))
    }

    func testAPhotoIsReplacedWhereItIsHeld() throws {
        var window = PhotoWindow(totalCount: 20, pageSize: 10)
        window.setPage(try photos(0, 10), at: 0)
        XCTAssertTrue(window.replace(try photo("p3", isWaiting: true)))
        XCTAssertTrue(window.photo(at: 3)?.isWaiting == true)
        XCTAssertFalse(window.replace(try photo("p15")), "a photo on a page that is not held is not added")
        XCTAssertEqual(window.pageIndexesWithWaitingPhotos, [0])
    }

    func testTakingPhotosOutLowersTheCountAndMakesTheLaterPagesStale() throws {
        var window = PhotoWindow(totalCount: 40, pageSize: 10)
        for pageIndex in 0..<4 { window.setPage(try photos(pageIndex * 10, pageIndex * 10 + 10), at: pageIndex) }
        window.remove(["p12", "p13"])
        XCTAssertEqual(window.totalCount, 38)
        XCTAssertEqual(window.stalePageIndexes, [1, 2, 3])
        XCTAssertEqual(window.pages[1]?.count, 8)
        // Until the page is read again, the photos after the two taken out sit where they were on it.
        XCTAssertEqual(window.photo(at: 12)?.id, "p14")
        // The stale pages are the ones to read again, nearest first.
        XCTAssertEqual(window.missingPages(around: 15, reach: 2), [1, 2, 3])
        window.setPage(try photos(12, 22), at: 1)
        XCTAssertEqual(window.stalePageIndexes, [2, 3])
        XCTAssertEqual(window.missingPages(around: 15, reach: 2), [2, 3])
    }

    func testTakingOutThePhotosOfTheLastPageLetsGoOfIt() throws {
        var window = PhotoWindow(totalCount: 12, pageSize: 10)
        window.setPage(try photos(0, 10), at: 0)
        window.setPage(try photos(10, 12), at: 1)
        window.remove(["p10", "p11"])
        XCTAssertEqual(window.totalCount, 10)
        XCTAssertEqual(window.pageCount, 1)
        XCTAssertNil(window.pages[1])
        XCTAssertTrue(window.stalePageIndexes.isEmpty)
    }

    func testPhotosThatAreNotHeldChangeNothing() throws {
        var window = PhotoWindow(totalCount: 20, pageSize: 10)
        window.setPage(try photos(0, 10), at: 0)
        window.remove(["p15"])
        XCTAssertEqual(window.totalCount, 20)
        XCTAssertTrue(window.stalePageIndexes.isEmpty)
        XCTAssertEqual(window.heldPhotoIds, Set((0..<10).map { "p\($0)" }))
    }

    func testTakingPhotosOutOfAWholeListKeepsItWhole() throws {
        var window = PhotoWindow(whole: try photos(0, 5))
        window.remove(["p1"])
        XCTAssertEqual(window.totalCount, 4)
        XCTAssertTrue(window.stalePageIndexes.isEmpty)
        XCTAssertEqual(window.heldPhotos.map(\.id), ["p0", "p2", "p3", "p4"])
        window.remove(["p0", "p2", "p3", "p4"])
        XCTAssertEqual(window.totalCount, 0)
        XCTAssertTrue(window.heldPhotos.isEmpty)
    }

    func testANewCountMakesWhatIsHeldStaleAndDropsPagesPastTheEnd() throws {
        var window = PhotoWindow(totalCount: 40, pageSize: 10)
        for pageIndex in 0..<4 { window.setPage(try photos(pageIndex * 10, pageIndex * 10 + 10), at: pageIndex) }
        window.setTotalCount(40)
        XCTAssertTrue(window.stalePageIndexes.isEmpty, "the same count says nothing changed")
        window.setTotalCount(23)
        XCTAssertEqual(window.stalePageIndexes, [0, 1, 2])
        XCTAssertNil(window.pages[3])
        window.setTotalCount(0)
        XCTAssertTrue(window.pages.isEmpty)
    }

    func testThePlaceOfAPhotoIsFoundAmongTheHeldPages() throws {
        var window = PhotoWindow(totalCount: 100, pageSize: 10)
        window.setPage(try photos(30, 40), at: 3)
        XCTAssertEqual(window.place(of: "p37"), 37)
        XCTAssertNil(window.place(of: "p5"))
    }

    func testTheHeldPhotosComeInTheOrderOfTheList() throws {
        var window = PhotoWindow(totalCount: 30, pageSize: 10)
        window.setPage(try photos(20, 30), at: 2)
        window.setPage(try photos(0, 10), at: 0)
        XCTAssertEqual(window.heldPhotos.map(\.id).first, "p0")
        XCTAssertEqual(window.heldPhotos.map(\.id).last, "p29")
        XCTAssertEqual(window.heldPhotos.count, 20)
    }
}
