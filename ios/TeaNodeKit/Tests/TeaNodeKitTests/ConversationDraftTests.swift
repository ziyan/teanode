import XCTest

@testable import TeaNodeKit

final class ConversationDraftTests: XCTestCase {
    func testDraftsBelongToOneAccountAndConversation() {
        let firstMainKey = ConversationDraft.storageKey(accountKey: "first-account", conversationId: nil)
        XCTAssertNotEqual(firstMainKey, ConversationDraft.storageKey(accountKey: "second-account", conversationId: nil))
        XCTAssertNotEqual(firstMainKey, ConversationDraft.storageKey(accountKey: "first-account", conversationId: "main"))
        XCTAssertEqual(firstMainKey, ConversationDraft.storageKey(accountKey: "first-account", conversationId: nil))
        XCTAssertNotEqual(
            ConversationDraft.storageKey(accountKey: "first-account", conversationId: "side-chat"),
            ConversationDraft.storageKey(accountKey: "second-account", conversationId: "side-chat"))
    }

    func testSigningOutForgetsOnlyThatAccountsDrafts() throws {
        let suiteName = "ConversationDraftTests-\(UUID().uuidString)"
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suiteName))
        defer { defaults.removePersistentDomain(forName: suiteName) }
        let firstMain = ConversationDraft.storageKey(accountKey: "first-account", conversationId: nil)
        let firstSide = ConversationDraft.storageKey(accountKey: "first-account", conversationId: "side-chat")
        // An account whose key starts with the other's is not caught by it.
        let longer = ConversationDraft.storageKey(accountKey: "first-account-two", conversationId: nil)
        for key in [firstMain, firstSide, longer] { defaults.set("A draft", forKey: key) }

        ConversationDraft.forget(accountKey: "first-account", in: defaults)
        XCTAssertNil(defaults.string(forKey: firstMain))
        XCTAssertNil(defaults.string(forKey: firstSide))
        XCTAssertEqual(defaults.string(forKey: longer), "A draft")
    }
}
