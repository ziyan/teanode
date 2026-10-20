import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// LinkPreviewTests cover what a preview is read from, what is asked for it,
/// and the one question the loader makes for the links of a screenful.
final class LinkPreviewTests: XCTestCase {
    private let availableJSON =
        #"{"url":"https://www.example.com/shop/chair?gclid=abc&color=blue","cleanedUrl":"https://www.example.com/shop/chair?color=blue","displayText":"example.com/shop/chair?color=blue","isAvailable":true,"title":"Garden Chair","description":"A chair for the garden.","siteName":"Example Shop","imageUrl":"/api/v1/link-preview/image/link-preview-1","imageWidthPixels":600,"imageHeightPixels":400}"#

    private let unavailableJSON =
        #"{"url":"https://example.net/gone","cleanedUrl":"https://example.net/gone","displayText":"example.net/gone","isAvailable":false,"title":"","description":"","siteName":"","imageUrl":null,"imageWidthPixels":null,"imageHeightPixels":null}"#

    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    func testAnAvailablePreviewDecodesAndHasACard() throws {
        let preview = try JSONDecoder().decode(LinkPreview.self, from: Data(availableJSON.utf8))
        XCTAssertEqual(preview.cleanedUrl, "https://www.example.com/shop/chair?color=blue")
        XCTAssertEqual(preview.title, "Garden Chair")
        XCTAssertEqual(preview.siteName, "Example Shop")
        XCTAssertEqual(preview.imageUrl, "/api/v1/link-preview/image/link-preview-1")
        XCTAssertEqual(preview.imageWidthPixels, 600)
        XCTAssertTrue(preview.hasCard)
    }

    func testAnUnavailablePreviewHasNoCard() throws {
        let preview = try JSONDecoder().decode(LinkPreview.self, from: Data(unavailableJSON.utf8))
        XCTAssertFalse(preview.hasCard)
        XCTAssertNil(preview.imageUrl)
        XCTAssertEqual(preview.displayText, "example.net/gone")
    }

    func testAPreviewWhoseAddressIsNotAWebAddressHasNoCard() {
        let preview = LinkPreview(url: "x", cleanedUrl: "javascript:alert(1)", displayText: "x", isAvailable: true, title: "Odd")
        XCTAssertFalse(preview.hasCard)
    }

    func testAMissingFieldIsEmptyAndNotAFailure() throws {
        let preview = try JSONDecoder().decode(LinkPreview.self, from: Data(#"{"url":"https://example.com/a"}"#.utf8))
        XCTAssertEqual(preview.cleanedUrl, "https://example.com/a")
        XCTAssertFalse(preview.isAvailable)
        XCTAssertEqual(preview.title, "")
    }

    func testTheLinksAreSentAsTheyWereWritten() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"ReadLinkPreviews":[\#(availableJSON),\#(unavailableJSON)]}}"#)
        let previews = try await connection.linkPreviews(urls: ["https://www.example.com/shop/chair?gclid=abc&color=blue", "https://example.net/gone"])
        XCTAssertEqual(previews.map(\.isAvailable), [true, false])
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any])
        let query = try XCTUnwrap(sent["query"] as? String)
        XCTAssertTrue(query.contains("ReadLinkPreviews(urls: $urls)"))
        let variables = try XCTUnwrap(sent["variables"] as? [String: Any])
        XCTAssertEqual(
            variables["urls"] as? [String], ["https://www.example.com/shop/chair?gclid=abc&color=blue", "https://example.net/gone"])
    }

    // MARK: The loader

    private final class Recorder: @unchecked Sendable {
        private let lock = NSLock()
        private var questions: [[String]] = []
        var asked: [[String]] {
            lock.lock()
            defer { lock.unlock() }
            return questions
        }
        func record(_ urls: [String]) {
            lock.lock()
            defer { lock.unlock() }
            questions.append(urls)
        }
    }

    private func preview(of url: String) -> LinkPreview {
        LinkPreview(url: url, cleanedUrl: url, displayText: url, isAvailable: true, title: "Title of \(url)")
    }

    func testTheLinksOfAScreenfulAreAskedForInOneQuestionTenAtATime() async {
        let recorder = Recorder()
        let loader = LinkPreviewLoader(collectingFor: .milliseconds(50)) { urls in
            recorder.record(urls)
            return urls.map { LinkPreview(url: $0, cleanedUrl: $0, displayText: $0, isAvailable: true, title: "Title of \($0)") }
        }
        let addresses = (0..<12).map { "https://example.com/page-\($0)" }
        let found = await withTaskGroup(of: (Int, LinkPreview?).self) { group -> [LinkPreview?] in
            for (index, address) in addresses.enumerated() {
                group.addTask { (index, await loader.preview(for: address)) }
            }
            var all = [LinkPreview?](repeating: nil, count: addresses.count)
            for await (index, preview) in group { all[index] = preview }
            return all
        }
        XCTAssertEqual(found.map { $0?.url }, addresses)
        XCTAssertEqual(recorder.asked.map(\.count).reduce(0, +), 12)
        XCTAssertTrue(recorder.asked.allSatisfy { $0.count <= LinkPreview.mostURLs })
        XCTAssertLessThanOrEqual(recorder.asked.count, 3)
    }

    func testALinkAskedForAgainIsNotAskedForAgain() async {
        let recorder = Recorder()
        let loader = LinkPreviewLoader(collectingFor: .milliseconds(10)) { urls in
            recorder.record(urls)
            return urls.map { LinkPreview(url: $0, cleanedUrl: $0, displayText: $0, isAvailable: true, title: "T") }
        }
        let first = await loader.preview(for: "https://example.com/once")
        let second = await loader.preview(for: "https://example.com/once")
        XCTAssertEqual(first?.title, "T")
        XCTAssertEqual(second?.title, "T")
        XCTAssertEqual(recorder.asked.count, 1)
    }

    func testAQuestionThatFailsHasNoPreviewAndIsAskedAgainAfterAWhile() async {
        let recorder = Recorder()
        let loader = LinkPreviewLoader(collectingFor: .milliseconds(10), failedFor: .milliseconds(100)) { urls in
            recorder.record(urls)
            if recorder.asked.count == 1 { throw TeaNodeError.refused("down") }
            return urls.map { LinkPreview(url: $0, cleanedUrl: $0, displayText: $0, isAvailable: true, title: "T") }
        }
        let failed = await loader.preview(for: "https://example.com/later")
        XCTAssertNil(failed)
        let soon = await loader.preview(for: "https://example.com/later")
        XCTAssertNil(soon)
        XCTAssertEqual(recorder.asked.count, 1)
        try? await Task.sleep(for: .milliseconds(300))
        let later = await loader.preview(for: "https://example.com/later")
        XCTAssertEqual(later?.title, "T")
        XCTAssertEqual(recorder.asked.count, 2)
    }
}
