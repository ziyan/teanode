import XCTest

@testable import TeaNodeKit

final class AppUpdateTests: XCTestCase {
    private func beta(_ version: String, _ build: String) -> AppUpdate {
        AppUpdate(updateChannel: "testflight", version: version, buildNumber: build, installUrl: "itms-beta://beta.itunes.apple.com/v1/app/1")
    }

    func testANewerBuildOfTheSameVersionIsNewer() {
        XCTAssertTrue(beta("1.2.0", "101").isNewer(thanVersion: "1.2.0", build: "100"))
        XCTAssertFalse(beta("1.2.0", "100").isNewer(thanVersion: "1.2.0", build: "100"))
        XCTAssertFalse(beta("1.2.0", "99").isNewer(thanVersion: "1.2.0", build: "100"))
    }

    func testTheVersionComesBeforeTheBuild() {
        XCTAssertTrue(beta("1.10.0", "5").isNewer(thanVersion: "1.9.0", build: "100"))
        XCTAssertFalse(beta("1.9.0", "500").isNewer(thanVersion: "1.10.0", build: "100"))
        XCTAssertFalse(beta("1.2", "100").isNewer(thanVersion: "1.2.0", build: "100"))
    }

    func testTheAppStoreIsComparedByVersionAlone() {
        let store = AppUpdate(updateChannel: "appstore", version: "1.2.0", buildNumber: "", installUrl: "https://apps.apple.com/app/id1")
        XCTAssertTrue(store.isNewer(thanVersion: "1.1.0", build: "900"))
        XCTAssertFalse(store.isNewer(thanVersion: "1.2.0", build: "1"))
        XCTAssertEqual(store.versionText, "1.2.0")
    }

    func testADismissalIsOfThatUpdateAlone() {
        XCTAssertEqual(beta("1.2.0", "101").versionText, "1.2.0 (101)")
        XCTAssertNotEqual(beta("1.2.0", "101").dismissalKey, beta("1.2.0", "102").dismissalKey)
    }
}
