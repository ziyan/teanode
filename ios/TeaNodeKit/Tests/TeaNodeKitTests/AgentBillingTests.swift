import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AgentBillingTests cover the model prices checked against the provider's
/// bill: reading the last check, running one, and telling a provider never
/// checked from one that was.
final class AgentBillingTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "operator")
        return (Connection(account: account, transport: fake), fake)
    }

    private let checkedProvider = """
        {"providerName":"fixture-provider","checkedAt":"2030-01-02T03:04:05.123456789Z","errorMessage":"",
         "compared":[
           {"model":"fixture-model","tokenKind":"cacheWrite","configuredPerMillion":0.25,"billedPerMillion":0.25,"dayCount":6,"isPattern":false},
           {"model":"fixture-*","tokenKind":"output","configuredPerMillion":1,"billedPerMillion":1.2,"dayCount":5,"isPattern":true}],
         "changed":[{"model":"fixture-model","tokenKind":"cacheWrite","beforePerMillion":0,"afterPerMillion":0.25}]}
        """

    func testReadsTheLastCheckOfEachProviderWithABillingKey() async throws {
        let (connection, fake) = connection(
            answering: """
                {"data":{"AgentBillingChecks":[\(checkedProvider),
                  {"providerName":"unchecked-provider","checkedAt":"0001-01-01T00:00:00Z","errorMessage":"","compared":[],"changed":[]}]}}
                """)
        let checks = try await connection.agentBillingChecks()
        XCTAssertEqual(checks.map(\.providerName), ["fixture-provider", "unchecked-provider"])
        XCTAssertNotNil(checks[0].checkedDate)
        XCTAssertNil(checks[1].checkedDate, "the zero time is a provider never checked, not a check in the year 1")
        XCTAssertEqual(checks[0].compared.count, 2)
        XCTAssertTrue(checks[0].compared[1].isPattern)
        XCTAssertTrue(checks[0].compared[1].isChanged)
        XCTAssertFalse(checks[0].compared[0].isChanged)
        XCTAssertEqual(checks[0].changed[0].afterPerMillion, 0.25)
        let sentQuery = try JSONDecoder().decode([String: JSONValue].self, from: fake.sent[0].httpBody ?? Data())["query"]?.stringValue ?? ""
        XCTAssertTrue(sentQuery.contains("AgentBillingChecks"))
        XCTAssertFalse(sentQuery.contains("mutation"), "reading the last check runs none")
    }

    func testRunningACheckSendsTheMutationAndReadsWhatItDid() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"CheckAgentProviderPrices":[\#(checkedProvider)]}}"#)
        let checks = try await connection.checkAgentProviderPrices()
        XCTAssertEqual(checks.count, 1)
        XCTAssertEqual(checks[0].changed.map(\.model), ["fixture-model"])
        let sentQuery = try JSONDecoder().decode([String: JSONValue].self, from: fake.sent[0].httpBody ?? Data())["query"]?.stringValue ?? ""
        XCTAssertTrue(sentQuery.hasPrefix("mutation"))
        XCTAssertTrue(sentQuery.contains("CheckAgentProviderPrices"))
    }

    func testARefusedKeyIsACheckWithAnError() async throws {
        let (connection, _) = connection(
            answering: """
                {"data":{"AgentBillingChecks":[{"providerName":"fixture-provider","checkedAt":"2030-01-02T03:04:05Z",
                  "errorMessage":"the billing key was refused","compared":[],"changed":[]}]}}
                """)
        let checks = try await connection.agentBillingChecks()
        XCTAssertEqual(checks[0].errorMessage, "the billing key was refused")
        XCTAssertNotNil(checks[0].checkedDate)
    }
}
