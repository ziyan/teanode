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
            server.endpoint("/api/v1/mail/m1/remote?url=https%3A%2F%2Fpictures.example%2Fa.png").absoluteString,
            "https://mail.example.com:10443/api/v1/mail/m1/remote?url=https%3A%2F%2Fpictures.example%2Fa.png",
            "a query stays a query, not an escaped part of the path")
        XCTAssertEqual(
            server.websocketEndpoint("/api/v1/graphql").absoluteString, "wss://mail.example.com:10443/api/v1/graphql")
        XCTAssertEqual(
            try ServerAddress(parsing: "http://localhost:10081").websocketEndpoint("/api/v1/agent/voice").absoluteString,
            "ws://localhost:10081/api/v1/agent/voice")
    }

    func testAConnectLinkCarriesItsServerAndCode() throws {
        let server = try ServerAddress(parsing: "mail.example.com:10443")
        let link = ConnectLink(server: server, connectCode: "abc123")
        XCTAssertEqual(link.url.host, "teanode.com")
        XCTAssertNil(link.url.query, "the server and code belong after the #, not in the query")
        XCTAssertEqual(ConnectLink(link.url), link)
    }

    func testTheServersLinkIsRead() throws {
        // As the server writes it: Go's url.Values, sorted, percent-encoded.
        let written = URL(string: "https://teanode.com/app/connect#code=abc123&server=https%3A%2F%2Fmail.example.com%3A10443")!
        let link = try XCTUnwrap(ConnectLink(written))
        XCTAssertEqual(link.server.description, "https://mail.example.com:10443")
        XCTAssertEqual(link.connectCode, "abc123")
    }

    func testOtherLinksAreNotConnectLinks() throws {
        for written in [
            "https://example.com/app/connect#server=mail.example.com",
            "https://teanode.com/app/other#server=mail.example.com",
            "http://teanode.com/app/connect#server=mail.example.com",
            "https://teanode.com/app/connect",
            "https://teanode.com/app/connect?server=mail.example.com",
            "https://teanode.com/app/connect#server=http://mail.example.com",
        ] {
            XCTAssertNil(ConnectLink(URL(string: written)!), "for \(written)")
        }
    }
}
