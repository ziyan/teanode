import Foundation
import XCTest

@testable import TeaNodeKit

final class MailTests: XCTestCase {
    func testFoldersAreInTheRailsOrder() {
        let folders = [
            MailFolder(id: "trash", mailboxId: "m", name: "Trash", kind: "trash"),
            MailFolder(id: "receipts", mailboxId: "m", name: "Receipts"),
            MailFolder(id: "inbox", mailboxId: "m", name: "Inbox", kind: "inbox"),
            MailFolder(id: "travel", mailboxId: "m", name: "Travel"),
            MailFolder(id: "trains", mailboxId: "m", parentId: "travel", name: "Trains"),
            MailFolder(id: "sent", mailboxId: "m", name: "Sent", kind: "sent"),
            MailFolder(id: "lost", mailboxId: "m", parentId: "gone", name: "Archived elsewhere"),
        ]
        let rows = MailFolders.rows(folders)
        XCTAssertEqual(rows.map(\.folder.id), ["inbox", "sent", "trash", "lost", "receipts", "travel", "trains"])
        XCTAssertEqual(rows.map(\.depth), [0, 0, 0, 0, 0, 0, 1])
    }

    func testANotesFolderIsOneHoldingOnlyNotes() {
        XCTAssertTrue(MailFolder(id: "a", mailboxId: "m", name: "Ideas", total: 3, noteCount: 3).isNotes)
        XCTAssertTrue(MailFolder(id: "b", mailboxId: "m", name: "Notes").isNotes)
        XCTAssertFalse(MailFolder(id: "c", mailboxId: "m", name: "Ideas", total: 3, noteCount: 2).isNotes)
        XCTAssertFalse(MailFolder(id: "d", mailboxId: "m", name: "Notes", kind: "inbox").isNotes)
    }

    func testTheShieldSaysWhatTheChecksFound() throws {
        func verdicts(_ json: String) throws -> MailMessage.Verdicts {
            try JSONDecoder().decode(MailMessage.Verdicts.self, from: Data(json.utf8))
        }
        XCTAssertEqual(MailVerdict.of(try verdicts(#"{"dmarc":{"result":"pass"},"dkims":[],"spamFilter":{"score":1}}"#)), .good)
        XCTAssertEqual(MailVerdict.of(try verdicts(#"{"spf":{"result":"pass"},"dkims":[{"result":"pass"}]}"#)), .good)
        XCTAssertEqual(MailVerdict.of(try verdicts(#"{"dmarc":{"result":"fail"},"dkims":[]}"#)), .bad)
        XCTAssertEqual(MailVerdict.of(try verdicts(#"{"dkims":[{"result":"fail"}]}"#)), .bad)
        XCTAssertEqual(MailVerdict.of(try verdicts(#"{"dkims":[{"result":"fail"},{"result":"pass"}],"dmarc":{"result":"pass"}}"#)), .good)
        XCTAssertEqual(MailVerdict.of(try verdicts(#"{"dmarc":{"result":"pass"},"dkims":[],"spamFilter":{"score":7.5}}"#)), .warn)
        XCTAssertEqual(MailVerdict.of(nil), .unknown)
        XCTAssertEqual(MailVerdict.detail(try verdicts(#"{"spf":{"result":"pass"},"dkims":[{"result":"pass"}],"dmarc":{"result":"pass"},"spamFilter":{"score":0.4}}"#)), "SPF pass · DKIM pass · DMARC pass · spam 0.4")
    }

    func testAReplyGoesBackToTheSenderFromTheAddressWrittenTo() throws {
        let message = try JSONDecoder().decode(MailMessage.self, from: Data(#"""
            {"id":"m1","from":"sam@example.org","fromName":"Sam","sender":"","subject":"Lunch","recipients":["Robin <robin@example.com>","pat@example.net","robin+notes@example.com"],"receivedAt":"2026-10-10T09:00:00Z","size":100,"logoDomain":""}
            """#.utf8))
        let own = ["robin@example.com", "robin+notes@example.com"]
        let reply = MailWords.reply(to: message, replyTo: nil, own: own, isReplyAll: false)
        XCTAssertEqual(reply.to, ["sam@example.org"])
        XCTAssertEqual(reply.cc, [])
        XCTAssertEqual(reply.from, "robin@example.com")
        let all = MailWords.reply(to: message, replyTo: "list@example.org", own: own, isReplyAll: true)
        XCTAssertEqual(all.to, ["list@example.org"])
        XCTAssertEqual(all.cc, ["pat@example.net"])
        XCTAssertEqual(MailWords.replySubject("Lunch"), "Re: Lunch")
        XCTAssertEqual(MailWords.replySubject("RE: Lunch"), "RE: Lunch")
        XCTAssertEqual(MailWords.forwardSubject("Lunch"), "Fwd: Lunch")
    }

    func testTypedAddressesAreSplitAndPlainTextBecomesParagraphs() {
        XCTAssertEqual(MailWords.addresses("a@example.com, b@example.com;\nc@example.com ,"), ["a@example.com", "b@example.com", "c@example.com"])
        XCTAssertEqual(MailWords.bareAddress("Sam <sam@example.org>"), "sam@example.org")
        XCTAssertEqual(MailWords.html(ofText: "Hi <you>\nthere\n\nBye"), "<p>Hi &lt;you&gt;<br>there</p><p>Bye</p>")
        XCTAssertEqual(MailWords.quoted("one\ntwo"), "> one\n> two")
    }

    func testRemoteImagesLoadThroughTheServerNeverFromTheSender() {
        let html = #"<p>Hi</p><img data-blocked-src="https://pictures.example/a.png?x=1&amp;y=2" src="data:image/gif;base64,R0lGOD"><img src="cid:logo">"#
        let shown = MailWords.showingRemoteImages(html, mailId: "m1")
        XCTAssertEqual(
            shown,
            #"<p>Hi</p><img src="/api/v1/mail/m1/remote?url=https%3A%2F%2Fpictures.example%2Fa.png%3Fx%3D1%26y%3D2"><img src="cid:logo">"#)
        XCTAssertEqual(MailWords.decodedAttribute("&amp;lt;&lt;"), "&lt;<")
    }

    func testAThreadPageIsRead() throws {
        let page = try JSONDecoder().decode(MailThreadPage.self, from: Data(#"""
            {"total":1,"threads":[{"threadId":"t1","count":2,"unread":1,"flagged":false,"participants":["Sam","Robin"],"itemIds":["i1","i2"],"hasDraft":false,
              "item":{"id":"i1","folderId":"inbox","mailId":"m1","seen":false,"flagged":false,"answered":false,"forwarded":false,"draft":false,"addedAt":"2026-10-10T09:00:00Z","subscriptionId":"",
                "insight":{"category":"personal","priority":"high","needsReply":true,"summary":"Lunch on Friday?","actionItems":[],"notes":"","model":"m","runId":"","proposals":[]},
                "mail":{"id":"m1","from":"sam@example.org","fromName":"Sam","sender":"","subject":"Lunch","recipients":[],"receivedAt":"2026-10-10T09:00:00Z","size":10,"messageId":"<a@b>","listKey":"","listName":"","logoDomain":"",
                  "authenticationResults":{"spf":null,"dkims":[],"dmarc":{"result":"pass"},"spamFilter":null}}}}]}
            """#.utf8))
        XCTAssertEqual(page.threads.first??.item?.insight?.priority, "high")
        XCTAssertEqual(page.threads.first??.id, "i1")
    }

    func testAListOffersTheWayOutItHas() throws {
        func subscription(oneClick: Bool, unsubscribe: [String]) throws -> MailSubscription {
            let addresses = unsubscribe.map { "\"\($0)\"" }.joined(separator: ",")
            return try JSONDecoder().decode(MailSubscription.self, from: Data("""
                {"id":"s","key":"k","name":"News","from":"news@example.org","count":3,"unread":1,"lastAt":"2026-10-10T09:00:00Z","lastItemId":"i","oneClick":\(oneClick),"unsubscribe":[\(addresses)],"logoDomain":"","method":"","failed":false,"error":"","stripped":false}
                """.utf8))
        }
        XCTAssertEqual(try subscription(oneClick: true, unsubscribe: ["https://example.org/leave"]).leaveKind, "oneClick")
        XCTAssertEqual(try subscription(oneClick: false, unsubscribe: ["https://example.org/leave", "mailto:leave@example.org"]).leaveKind, "mail")
        XCTAssertEqual(try subscription(oneClick: false, unsubscribe: ["https://example.org/leave"]).leaveKind, "link")
        XCTAssertEqual(try subscription(oneClick: false, unsubscribe: []).leaveKind, "none")
    }

    func testAnOutgoingMessageSendsOnlyWhatItIs() {
        var message = MailOutgoing()
        message.from = "robin@example.com"
        message.to = ["sam@example.org"]
        message.subject = "Hi"
        guard case .object(let fields) = message.variables else { return XCTFail("not an object") }
        XCTAssertNil(fields["forwardItemId"])
        XCTAssertNil(fields["draftItemId"])
        message.draftItemId = "d1"
        message.keepAttachments = [0, 2]
        guard case .object(let drafted) = message.variables else { return XCTFail("not an object") }
        XCTAssertEqual(drafted["keepAttachments"], .array([.number(0), .number(2)]))
    }
}
