import Foundation
import XCTest

@testable import TeaNodeKit

/// LinkOpeningTests cover which links the app's browser opens and which it
/// leaves to the system.
final class LinkOpeningTests: XCTestCase {
    private func way(_ link: String) throws -> LinkOpening.Way {
        LinkOpening.way(for: try XCTUnwrap(URL(string: link)))
    }

    func testWebPagesOpenInTheAppsBrowser() throws {
        XCTAssertEqual(try way("https://www.example.com/shop/chair?color=blue"), .inApp)
        XCTAssertEqual(try way("http://example.org/"), .inApp)
        XCTAssertEqual(try way("HTTPS://Example.com/Path"), .inApp)
    }

    func testOtherSchemesStayWithTheSystem() throws {
        for link in [
            "mailto:someone@example.com", "tel:+15555550142", "sms:+15555550142", "teanode://open", "facetime:someone@example.com",
            "app-settings:", "ftp://example.com/file",
        ] {
            XCTAssertEqual(try way(link), .system, link)
        }
    }

    func testAppleMapsOpensTheMapsApp() throws {
        XCTAssertEqual(try way("https://maps.apple.com/?daddr=1%20Example%20Road&dirflg=d"), .system)
        XCTAssertEqual(try way("https://maps.apple/p/example"), .system)
    }

    func testTheAppsOwnUniversalLinksOpenTheApp() throws {
        XCTAssertEqual(try way("https://teanode.com/app/connect#server=example.com&code=abc"), .system)
        XCTAssertEqual(try way("https://teanode.com/app/anything/else"), .system)
        // The rest of the site is a web page like any other.
        XCTAssertEqual(try way("https://teanode.com/"), .inApp)
        XCTAssertEqual(try way("https://teanode.com/application-notes"), .inApp)
        XCTAssertEqual(try way("https://www.teanode.com/app/connect"), .inApp)
    }

    func testAConfigurationProfileIsSafarisToInstall() throws {
        XCTAssertEqual(try way("https://mail.example.com/api/v1/mailbox/profile/token.mobileconfig"), .system)
    }

    func testALinkWithNoHostIsTheSystems() throws {
        XCTAssertEqual(try way("https:///nothing"), .system)
    }
}
