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
        let previews = try await connection.linkPreviews(conversationId: "conversation-1", urls: ["https://www.example.com/shop/chair?gclid=abc&color=blue", "https://example.net/gone"])
        XCTAssertEqual(previews.map(\.isAvailable), [true, false])
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any])
        let query = try XCTUnwrap(sent["query"] as? String)
        XCTAssertTrue(query.contains("ReadLinkPreviews(conversationId: $conversationId, urls: $urls)"))
        let variables = try XCTUnwrap(sent["variables"] as? [String: Any])
        XCTAssertEqual(variables["conversationId"] as? String, "conversation-1")
        XCTAssertEqual(
            variables["urls"] as? [String], ["https://www.example.com/shop/chair?gclid=abc&color=blue", "https://example.net/gone"])
    }

    // MARK: The loader

    private final class Recorder: @unchecked Sendable {
        private let lock = NSLock()
        private var questions: [(conversationId: String, urls: [String])] = []
        var asked: [(conversationId: String, urls: [String])] {
            lock.lock()
            defer { lock.unlock() }
            return questions
        }
        func record(_ conversationId: String, _ urls: [String]) {
            lock.lock()
            defer { lock.unlock() }
            questions.append((conversationId, urls))
        }
    }

    private static func found(_ urls: [String]) -> [LinkPreview] {
        urls.map { LinkPreview(url: $0, cleanedUrl: $0, displayText: $0, isAvailable: true, title: "Title of \($0)") }
    }

    func testTheLinksOfAScreenfulAreAskedForInOneQuestionTenAtATime() async {
        let recorder = Recorder()
        let loader = LinkPreviewLoader(collectingFor: .milliseconds(50)) { conversationId, urls in
            recorder.record(conversationId, urls)
            return Self.found(urls)
        }
        let addresses = (0..<12).map { "https://example.com/page-\($0)" }
        let found = await withTaskGroup(of: (Int, LinkPreview?).self) { group -> [LinkPreview?] in
            for (index, address) in addresses.enumerated() {
                group.addTask { (index, await loader.preview(conversationId: "conversation-1", for: address)) }
            }
            var all = [LinkPreview?](repeating: nil, count: addresses.count)
            for await (index, preview) in group { all[index] = preview }
            return all
        }
        XCTAssertEqual(found.map { $0?.url }, addresses)
        XCTAssertEqual(recorder.asked.map(\.urls.count).reduce(0, +), 12)
        XCTAssertTrue(recorder.asked.allSatisfy { $0.urls.count <= LinkPreview.mostURLs && $0.conversationId == "conversation-1" })
        XCTAssertLessThanOrEqual(recorder.asked.count, 3)
    }

    func testEachConversationIsAskedInAQuestionOfItsOwnWithTheLinksAsWritten() async {
        let recorder = Recorder()
        let loader = LinkPreviewLoader(collectingFor: .milliseconds(50)) { conversationId, urls in
            recorder.record(conversationId, urls)
            return Self.found(urls)
        }
        let tracked = "https://example.com/page?gclid=abc&color=blue"
        async let first = loader.preview(conversationId: "conversation-1", for: tracked)
        async let other = loader.preview(conversationId: "conversation-2", for: "https://example.com/other")
        async let second = loader.preview(conversationId: "conversation-1", for: "https://example.com/second")
        _ = await (first, other, second)
        let byConversation = Dictionary(uniqueKeysWithValues: recorder.asked.map { ($0.conversationId, $0.urls) })
        XCTAssertEqual(recorder.asked.count, 2)
        XCTAssertEqual(Set(byConversation["conversation-1"] ?? []), [tracked, "https://example.com/second"])
        XCTAssertEqual(byConversation["conversation-2"], ["https://example.com/other"])
    }

    func testALinkAskedForAgainIsNotAskedForAgainUnlessItIsInAnotherConversation() async {
        let recorder = Recorder()
        let loader = LinkPreviewLoader(collectingFor: .milliseconds(10)) { conversationId, urls in
            recorder.record(conversationId, urls)
            return Self.found(urls)
        }
        let first = await loader.preview(conversationId: "conversation-1", for: "https://example.com/once")
        let second = await loader.preview(conversationId: "conversation-1", for: "https://example.com/once")
        XCTAssertEqual(first?.title, "Title of https://example.com/once")
        XCTAssertEqual(second?.title, "Title of https://example.com/once")
        XCTAssertEqual(recorder.asked.count, 1)
        _ = await loader.preview(conversationId: "conversation-2", for: "https://example.com/once")
        XCTAssertEqual(recorder.asked.count, 2)
    }

    func testAQuestionThatFailsHasNoPreviewAndIsAskedAgainAfterAWhile() async {
        let recorder = Recorder()
        let loader = LinkPreviewLoader(collectingFor: .milliseconds(10), failedFor: .milliseconds(100)) { conversationId, urls in
            recorder.record(conversationId, urls)
            if recorder.asked.count == 1 { throw TeaNodeError.refused("down") }
            return Self.found(urls)
        }
        let failed = await loader.preview(conversationId: "conversation-1", for: "https://example.com/later")
        XCTAssertNil(failed)
        let soon = await loader.preview(conversationId: "conversation-1", for: "https://example.com/later")
        XCTAssertNil(soon)
        XCTAssertEqual(recorder.asked.count, 1)
        try? await Task.sleep(for: .milliseconds(300))
        let later = await loader.preview(conversationId: "conversation-1", for: "https://example.com/later")
        XCTAssertEqual(later?.isAvailable, true)
        XCTAssertEqual(recorder.asked.count, 2)
    }

    func testTheLinksAheadAreTheNewestFirstOnceAndOnlyOfFinishedMessages() {
        var taken = TranscriptLine(id: "m6", kind: .person, text: "https://example.org/taken-back")
        taken.isDeleted = true
        let lines = [
            TranscriptLine(id: "m1", kind: .person, text: "Look at https://example.com/first"),
            TranscriptLine(id: "m2", kind: .answer, text: "See `https://example.com/code` and [the page](https://example.com/answer)."),
            TranscriptLine(id: "m3", kind: .tool, text: "https://example.com/tool"),
            TranscriptLine(id: "m4", kind: .person, text: "Again https://example.com/first"),
            TranscriptLine(id: "m5", kind: .answer, text: "https://example.com/streaming", isStreaming: true),
            taken,
            TranscriptLine(id: Transcript.sentPrefix + "7", kind: .person, text: "https://example.com/sending"),
        ]
        XCTAssertEqual(LinkPreview.linksAhead(of: lines), ["https://example.com/first", "https://example.com/answer"])
    }
}
