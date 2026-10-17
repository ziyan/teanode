import Foundation
import XCTest

@testable import TeaNodeKit

final class PresenceTests: XCTestCase {
    func testTheReportNamesTheConversationOnScreen() {
        let variables = Connection.presenceVariables(isVisible: true, conversationId: "a-chat-conversation")
        XCTAssertEqual(variables["conversationId"], .string("a-chat-conversation"))
        XCTAssertEqual(variables["isVisible"], .bool(true))
        XCTAssertEqual(variables["reporterId"], .string(Connection.presenceReporterId))
    }

    func testTheReportOfAnotherScreenNamesNoConversation() {
        let variables = Connection.presenceVariables(isVisible: true, conversationId: "")
        XCTAssertEqual(variables["conversationId"], .string(""))
    }

    func testTheDocumentAsksForTheConversation() {
        XCTAssertTrue(Documents.reportPresence.contains("$conversationId: String"))
        XCTAssertTrue(Documents.reportPresence.contains("conversationId: $conversationId"))
    }
}
