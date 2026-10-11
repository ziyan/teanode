import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

final class RunAnswerTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(_ pages: [String]) -> (Connection, FakeServer) {
        let lock = NSLock()
        nonisolated(unsafe) var index = 0
        let fake = FakeServer { _ in
            lock.lock()
            defer { lock.unlock() }
            let page = pages[min(index, pages.count - 1)]
            index += 1
            return (200, page)
        }
        let account = Account(server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400), username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func event(_ kind: String, _ sequence: Int, text: String = "") -> String {
        #"{"kind":"\#(kind)","runId":"r1","sequence":\#(sequence),"text":"\#(text)"}"#
    }

    func testTheLastWholeMessageIsTheAnswer() async throws {
        let (connection, fake) = connection([
            #"{"data":{"ReadAgentRun":{"done":false,"events":[\#(event("asked", 0)),\#(event("message", 1, text: "Let me look.")),\#(event("tool_call", 2))]}}}"#,
            #"{"data":{"ReadAgentRun":{"done":true,"events":[\#(event("tool_result", 3)),\#(event("message", 4, text: "It is sunny.")),\#(event("done", 5))]}}}"#,
        ])
        let answer = try await connection.answer(to: "r1", until: Date().addingTimeInterval(60))
        XCTAssertEqual(answer, .answered("It is sunny."))
        // The second read starts after the last event the first one saw.
        let body = String(decoding: fake.sent[1].httpBody ?? Data(), as: UTF8.self)
        XCTAssertTrue(body.contains(#""after":3"#), body)
    }

    func testAnApprovalWaitsForThePerson() async throws {
        let (connection, _) = connection([
            #"{"data":{"ReadAgentRun":{"done":false,"events":[\#(event("asked", 0)),\#(event("confirmation", 1))]}}}"#
        ])
        let answer = try await connection.answer(to: "r1", until: Date().addingTimeInterval(60))
        XCTAssertEqual(answer, .waitingForPerson)
    }

    func testAPassedDeadlineIsStillWorking() async throws {
        let (connection, fake) = connection([#"{"data":{"ReadAgentRun":{"done":false,"events":[]}}}"#])
        let answer = try await connection.answer(to: "r1", until: Date().addingTimeInterval(-1))
        XCTAssertEqual(answer, .stillWorking)
        XCTAssertTrue(fake.sent.isEmpty)
    }
}
