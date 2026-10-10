import Foundation
import XCTest

@testable import TeaNodeKit

final class ServerAddressTests: XCTestCase {
    func testWhatPeopleTypeBecomesTheServersOrigin() throws {
        let written: [(String, String)] = [
            ("mail.example.com", "https://mail.example.com"),
            ("  Mail.Example.COM \n", "https://mail.example.com"),
            ("https://mail.example.com/agent?tab=main#top", "https://mail.example.com"),
            ("mail.example.com:10443", "https://mail.example.com:10443"),
            ("https://mail.example.com:443", "https://mail.example.com"),
            ("http://localhost:10081", "http://localhost:10081"),
            ("http://127.0.0.1:10081/", "http://127.0.0.1:10081"),
            ("http://teanode.localhost", "http://teanode.localhost"),
        ]
        for (typed, origin) in written {
            XCTAssertEqual(try ServerAddress(parsing: typed).description, origin, "for \(typed)")
        }
    }

    func testWhatIsNotAServerIsRefusedWithWhy() {
        let refused: [(String, ServerAddressError)] = [
            ("", .isEmpty),
            ("   ", .isEmpty),
            ("http://mail.example.com", .isNotEncrypted),
            ("https://someone:secret@mail.example.com", .hasCredentials),
            ("ftp://mail.example.com", .isNotAnAddress),
            ("javascript:alert(1)", .isNotAnAddress),
            ("https://", .isNotAnAddress),
        ]
        for (typed, reason) in refused {
            XCTAssertThrowsError(try ServerAddress(parsing: typed), "for \(typed)") { error in
                XCTAssertEqual(error as? ServerAddressError, reason, "for \(typed)")
            }
        }
    }

    func testEndpointsAreOnTheServer() throws {
        let server = try ServerAddress(parsing: "mail.example.com:10443")
        XCTAssertEqual(server.endpoint("/api/v1/graphql").absoluteString, "https://mail.example.com:10443/api/v1/graphql")
        XCTAssertEqual(server.endpoint("oauth/token").absoluteString, "https://mail.example.com:10443/oauth/token")
        XCTAssertEqual(
            server.websocketEndpoint("/api/v1/graphql").absoluteString, "wss://mail.example.com:10443/api/v1/graphql")
        XCTAssertEqual(
            try ServerAddress(parsing: "http://localhost:10081").websocketEndpoint("/api/v1/agent/voice").absoluteString,
            "ws://localhost:10081/api/v1/agent/voice")
    }

    func testAConnectLinkNamesItsServer() throws {
        let server = try ServerAddress(parsing: "mail.example.com:10443")
        let link = ConnectLink.link(for: server)
        XCTAssertEqual(link.host, "teanode.com")
        XCTAssertEqual(ConnectLink.server(in: link), server)
    }

    func testOtherLinksAreNotConnectLinks() throws {
        for written in [
            "https://example.com/app/connect?server=mail.example.com",
            "https://teanode.com/app/other?server=mail.example.com",
            "http://teanode.com/app/connect?server=mail.example.com",
            "https://teanode.com/app/connect",
            "https://teanode.com/app/connect?server=http://mail.example.com",
        ] {
            XCTAssertNil(ConnectLink.server(in: URL(string: written)!), "for \(written)")
        }
    }
}
