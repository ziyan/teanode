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
}
