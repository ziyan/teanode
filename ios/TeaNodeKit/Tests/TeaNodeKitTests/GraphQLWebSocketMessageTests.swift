import Foundation
import XCTest

@testable import TeaNodeKit

final class GraphQLWebSocketMessageTests: XCTestCase {
    func testTheAppsMessagesAreWhatTheServerReads() throws {
        XCTAssertEqual(
            try GraphQLWebSocketMessage.connectionInit(authorization: "Bearer a-token").encoded(),
            #"{"payload":{"Authorization":"Bearer a-token"},"type":"connection_init"}"#)
        XCTAssertEqual(
            try GraphQLWebSocketMessage.start(
                id: "1", query: "subscription($conversationId: String!) { AgentConversationEvents(conversationId: $conversationId) { kind } }",
                variables: ["conversationId": .string("")]
            ).encoded(),
            #"{"id":"1","payload":{"query":"subscription($conversationId: String!) { AgentConversationEvents(conversationId: $conversationId) { kind } }","variables":{"conversationId":""}},"type":"start"}"#
        )
        XCTAssertEqual(try GraphQLWebSocketMessage.stop(id: "1").encoded(), #"{"id":"1","type":"stop"}"#)
    }

    func testTheServersMessagesAreRead() throws {
        XCTAssertEqual(try GraphQLWebSocketMessage(decoding: #"{"type":"connection_ack"}"#), .connectionAck)
        XCTAssertEqual(try GraphQLWebSocketMessage(decoding: #"{"type":"ka"}"#), .keepAlive)
        XCTAssertEqual(try GraphQLWebSocketMessage(decoding: #"{"id":"1","type":"complete"}"#), .complete(id: "1"))

        let data = try GraphQLWebSocketMessage(
            decoding: #"{"id":"1","type":"data","payload":{"data":{"AgentConversationEvents":{"kind":"text","text":"Hel"}}}}"#)
        guard case .data(let id, let answer, let errors) = data else {
            return XCTFail("not data: \(data)")
        }
        XCTAssertEqual(id, "1")
        XCTAssertEqual(answer?["AgentConversationEvents"]?["text"]?.stringValue, "Hel")
        XCTAssertEqual(errors, [])

        // The server sends one error as an object, not a list.
        XCTAssertEqual(
            try GraphQLWebSocketMessage(decoding: #"{"id":"2","type":"error","payload":{"message":"no such conversation"}}"#),
            .error(id: "2", errors: [GraphQLError(message: "no such conversation")]))
    }

    func testAMessageItCannotReadSaysWhy() {
        XCTAssertThrowsError(try GraphQLWebSocketMessage(decoding: #"{"type":"next","id":"1"}"#)) { error in
            XCTAssertEqual(error as? GraphQLWebSocketMessageError, .isUnknown(type: "next"))
        }
        XCTAssertThrowsError(try GraphQLWebSocketMessage(decoding: #"{"type":"complete"}"#)) { error in
            XCTAssertEqual(error as? GraphQLWebSocketMessageError, .hasNoID(type: "complete"))
        }
    }

    func testWhatItSendsItCanReadBack() throws {
        let answer = GraphQLWebSocketMessage.data(
            id: "7", data: .object(["count": .number(3), "isDone": .bool(true), "note": .null]), errors: [])
        XCTAssertEqual(try GraphQLWebSocketMessage(decoding: try answer.encoded()), answer)
    }
}
