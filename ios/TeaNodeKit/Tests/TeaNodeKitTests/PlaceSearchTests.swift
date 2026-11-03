import XCTest

@testable import TeaNodeKit

final class PlaceSearchTests: XCTestCase {
    private let settings = ["Settings", "Settings"]
    private let manage = ["Manage", "Manage"]

    private var places: [PlaceName] {
        [
            PlaceName(placeId: "chats", names: ["Chats", "Chats"]),
            PlaceName(placeId: "settings", names: settings),
            PlaceName(placeId: "location", names: ["Location", "Location"]),
            PlaceName(placeId: "passkeys", names: ["Passkeys", "Passkeys"], parentNames: [settings]),
            PlaceName(placeId: "location-settings", names: ["Location settings", "Location settings"], parentNames: [["Location", "Location"]]),
            PlaceName(placeId: "domains", names: ["Domains", "Domains"], parentNames: [settings, manage]),
            PlaceName(placeId: "people-in-photos", names: ["People in Photos", "People in Photos"], parentNames: [settings]),
            PlaceName(placeId: "people", names: ["People", "People"], parentNames: [settings, manage]),
            PlaceName(placeId: "receipts", names: ["收据", "Receipts"], parentNames: [["财务", "Finance"]]),
        ]
    }

    func testAPartComesBeforeThePlacesInsideOthersThatNameTheWords() {
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "settings"), ["settings", "location-settings"])
    }

    func testAPlaceIsFoundByItsOwnName() {
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "passk"), ["passkeys"])
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "Domains"), ["domains"])
    }

    func testTheStartOfANameMatchesBeforeAWordInsideIt() {
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "people"), ["people-in-photos", "people"])
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "photos"), ["people-in-photos"])
    }

    func testThePlaceItIsInAndItsOwnNameCanBeSaidTogether() {
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "manage dom"), ["domains"])
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "set pass"), ["passkeys"])
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "manage peo"), ["people"])
    }

    func testLettersInsideAWordAreNoMatch() {
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "ains"), [])
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "chat dom"), [])
    }

    func testANameMatchesInEnglishWhateverTheLanguage() {
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "receipts"), ["receipts"])
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "收据"), ["receipts"])
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "finance rec"), ["receipts"])
    }

    func testWordsWithNothingInThemFindNothing() {
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(places, words: "  "), [])
    }

    func testNoMoreThanTheLimitIsOffered() {
        let many = (0..<20).map { PlaceName(placeId: "place-\($0)", names: ["Folder \($0)"]) }
        XCTAssertEqual(PlaceSearch.matchingPlaceIds(many, words: "folder").count, PlaceSearch.placeLimit)
    }
}
