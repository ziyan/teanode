import XCTest

@testable import TeaNodeKit

final class SearchRecentsTests: XCTestCase {
    private func defaults() -> UserDefaults {
        let name = "search-recents-\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: name)!
        defaults.removePersistentDomain(forName: name)
        return defaults
    }

    func testTheNewestComesFirstAndAnEarlierCopyGoes() {
        let recents = SearchRecents(defaults: defaults())
        recents.remember(.place("finance-imports"), accountKey: "account-1")
        recents.remember(.found(SearchResult(searchArea: "contacts", targetId: "contact-1", titleText: "Robin"), words: "rob"), accountKey: "account-1")
        let shown = recents.remember(.place("finance-imports"), accountKey: "account-1")
        XCTAssertEqual(shown.map(\.id), ["place:finance-imports", "found:contacts/contact-1"])
        XCTAssertEqual(recents.load(accountKey: "account-1").last?.words, "rob")
    }

    func testTheListKeepsItsCapacityAndEachAccountItsOwn() {
        let recents = SearchRecents(defaults: defaults())
        for index in 0..<(SearchRecents.capacity + 3) {
            recents.remember(.place("place-\(index)"), accountKey: "account-1")
        }
        XCTAssertEqual(recents.load(accountKey: "account-1").count, SearchRecents.capacity)
        XCTAssertEqual(recents.load(accountKey: "account-1").first?.placeId, "place-\(SearchRecents.capacity + 2)")
        XCTAssertTrue(recents.load(accountKey: "account-2").isEmpty)
        recents.clear(accountKey: "account-1")
        XCTAssertTrue(recents.load(accountKey: "account-1").isEmpty)
    }
}
