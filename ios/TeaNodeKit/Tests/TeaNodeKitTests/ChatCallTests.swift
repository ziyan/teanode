import Foundation
import XCTest

@testable import TeaNodeKit

/// ChatCallTests cover calls in chats: what the server answers, what each
/// operation sends, the words of a call's line, the order of the signaling,
/// and what the phone reports to CallKit for each VoIP push.
final class ChatCallTests: XCTestCase {
    private func makeConnection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: try ServerAddress(parsing: "mail.example.com"), clientId: "a-client", accessToken: "tnt_a", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "mira")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ fake: FakeServer) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: fake.sent.last?.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private func sentDocument(_ fake: FakeServer) throws -> String {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: fake.sent.last?.httpBody ?? Data())
        return body["query"]?.stringValue ?? ""
    }

    /// A call of Mira's, going, with Tobias on it and Priya still ringing.
    private let goingCall = #"""
        {"id":"call-1","conversationId":"conversation-1","chatTitle":"Weekend plans","isGroup":true,"startedByUserId":"user-mira",
         "startedByName":"Mira","startedAt":"2030-01-01T10:00:00Z","answeredAt":"2030-01-01T10:00:20Z","endedAt":null,"endReason":"",
         "isGoing":true,"durationMs":252000,"askerRingState":"none","isAskerOn":true,"transcriptionAskedByUserId":"user-mira",
         "transcriptState":"","summaryMessageId":null,
         "members":[
           {"userId":"user-mira","name":"Mira","avatarUrl":null,"avatarColorIndex":3,"ringState":"none","isOn":true,"isVideoOn":false,
            "joinedAt":"2030-01-01T10:00:00Z","leftAt":null},
           {"userId":"user-tobias","name":"Tobias","avatarUrl":"/api/v1/avatar/user/user-tobias?v=1","avatarColorIndex":5,"ringState":"answered",
            "isOn":true,"isVideoOn":true,"joinedAt":"2030-01-01T10:00:20Z","leftAt":null},
           {"userId":"user-priya","name":"Priya","avatarUrl":null,"avatarColorIndex":8,"ringState":"ringing","isOn":false,"isVideoOn":false,
            "joinedAt":null,"leftAt":null}]}
        """#

    private func decodedCall(_ json: String) throws -> ChatCallView {
        try JSONDecoder().decode(ChatCallView.self, from: Data(json.utf8))
    }

    // MARK: Reading

    func testACallDecodesWithItsMembers() throws {
        let call = try decodedCall(goingCall)
        XCTAssertEqual(call.id, "call-1")
        XCTAssertTrue(call.isGoing)
        XCTAssertEqual(call.members.map(\.name), ["Mira", "Tobias", "Priya"])
        XCTAssertEqual(call.onCount, 2)
        XCTAssertEqual(call.onMembers.map(\.userId), ["user-mira", "user-tobias"])
        XCTAssertEqual(call.member(userId: "user-tobias")?.isVideoOn, true)
        XCTAssertEqual(call.durationMs, 252_000)
        XCTAssertNotNil(call.startedDate)
        XCTAssertNil(call.endedDate)
    }

    func testACallMissingFieldsStillDecodes() throws {
        let call = try decodedCall(#"{"id":"call-2"}"#)
        XCTAssertEqual(call.id, "call-2")
        XCTAssertFalse(call.isGoing)
        XCTAssertTrue(call.members.isEmpty)
        XCTAssertEqual(call.askerRingState, "")
    }

    func testAMessageThatIsACallLineCarriesTheCall() throws {
        let json = #"""
            {"id":"m1","createdAt":"2030-01-01T10:00:00Z","role":"user","content":"","authorKind":"person","authorUserId":"user-mira",
             "authorName":"Mira","isMine":false,"callId":"call-1",
             "call":{"id":"call-1","startedByUserId":"user-mira","startedAt":"2030-01-01T10:00:00Z","answeredAt":"2030-01-01T10:00:20Z",
                     "endedAt":"2030-01-01T10:04:32Z","endReason":"ended","transcriptionAskedByUserId":null,"transcriptState":"done",
                     "summaryMessageId":null,
                     "members":[{"userId":"user-mira","ringState":"none","joinedAt":"2030-01-01T10:00:00Z","leftAt":"2030-01-01T10:04:32Z",
                                 "isVideoOn":false},
                                {"userId":"user-tobias","ringState":"answered","joinedAt":"2030-01-01T10:00:20Z",
                                 "leftAt":"2030-01-01T10:04:30Z","isVideoOn":true}]}}
            """#
        let message = try JSONDecoder().decode(AgentMessage.self, from: Data(json.utf8))
        XCTAssertEqual(message.callId, "call-1")
        XCTAssertEqual(message.call?.members.count, 2)
        let line = try XCTUnwrap(Transcript(messages: [message], isChat: true).lines.first)
        XCTAssertTrue(line.isCallLine)
        XCTAssertEqual(line.chatCallId, "call-1")
        XCTAssertEqual(line.chatCall?.endReason, "ended")
        let words = ChatCallLine(record: try XCTUnwrap(line.chatCall), ownUserId: "user-tobias")
        XCTAssertEqual(words.kind, .ended(durationText: "4:12"))
        XCTAssertTrue(words.isTranscriptDone)
        XCTAssertFalse(words.isGoing)
    }

    func testAnOrdinaryMessageIsNotACallLine() throws {
        let json = #"{"id":"m2","createdAt":"2030-01-01T10:00:00Z","role":"user","content":"hello","authorKind":"person","authorUserId":"user-mira","authorName":"Mira","isMine":false}"#
        let message = try JSONDecoder().decode(AgentMessage.self, from: Data(json.utf8))
        let line = try XCTUnwrap(Transcript(messages: [message], isChat: true).lines.first)
        XCTAssertFalse(line.isCallLine)
        XCTAssertNil(line.chatCall)
    }

    func testTheMessageFieldsAskForTheCall() {
        XCTAssertTrue(Documents.messageFields.contains("callId call {"))
    }

    // MARK: Operations

    func testStartingACallSendsTheChatAndVideo() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"StartChatCall":\#(goingCall)}}"#)
        let call = try await connection.startChatCall(conversationId: "conversation-1", isVideoOn: true)
        XCTAssertEqual(call.id, "call-1")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["conversationId"], .string("conversation-1"))
        XCTAssertEqual(variables["isVideoOn"], .bool(true))
        XCTAssertTrue(try sentDocument(fake).contains("StartChatCall"))
    }

    func testJoiningAndLeavingNameTheCall() async throws {
        let (joining, joiningFake) = try makeConnection(answering: #"{"data":{"JoinChatCall":\#(goingCall)}}"#)
        _ = try await joining.joinChatCall(callId: "call-1")
        XCTAssertEqual(try sentVariables(joiningFake)["callId"], .string("call-1"))
        XCTAssertTrue(try sentDocument(joiningFake).contains("JoinChatCall("))

        let (leaving, leavingFake) = try makeConnection(answering: #"{"data":{"LeaveChatCall":\#(goingCall)}}"#)
        try await leaving.leaveChatCall(callId: "call-1")
        XCTAssertTrue(try sentDocument(leavingFake).contains("LeaveChatCall"))

        let (declining, decliningFake) = try makeConnection(answering: #"{"data":{"DeclineChatCall":\#(goingCall)}}"#)
        try await declining.declineChatCall(callId: "call-1", agentPhoneId: "phone-1")
        XCTAssertTrue(try sentDocument(decliningFake).contains("DeclineChatCall"))
        // The phone that declines names itself, so it is not told to stop
        // ringing; with no phone, none is named.
        XCTAssertEqual(try sentVariables(decliningFake)["agentPhoneId"], .string("phone-1"))
        XCTAssertNil(try sentVariables(joiningFake)["agentPhoneId"])
    }

    func testJoiningTheMediaSendsTheOfferAndReturnsTheAnswer() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"JoinChatCallMedia":{"answerSdp":"v=0 answer"}}}"#)
        let answer = try await connection.joinChatCallMedia(callId: "call-1", offerSdp: "v=0 offer")
        XCTAssertEqual(answer, "v=0 answer")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["callId"], .string("call-1"))
        XCTAssertEqual(variables["offerSdp"], .string("v=0 offer"))
    }

    func testAnsweringAnOfferSendsTheAnswer() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"AnswerChatCallMedia":true}}"#)
        let isAccepted = try await connection.answerChatCallMedia(callId: "call-1", answerSdp: "v=0 answer")
        XCTAssertTrue(isAccepted)
        XCTAssertEqual(try sentVariables(fake)["answerSdp"], .string("v=0 answer"))
    }

    func testTheCameraIsToldWithTheCall() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"SetChatCallVideo":\#(goingCall)}}"#)
        try await connection.setChatCallVideo(callId: "call-1", isVideoOn: false)
        XCTAssertEqual(try sentVariables(fake)["isVideoOn"], .bool(false))
    }

    func testTranscriptionIsTurnedOn() async throws {
        let (asking, askingFake) = try makeConnection(answering: #"{"data":{"AskChatCallTranscription":\#(goingCall)}}"#)
        try await asking.askChatCallTranscription(callId: "call-1")
        XCTAssertTrue(try sentDocument(askingFake).contains("AskChatCallTranscription"))
        XCTAssertEqual(try sentVariables(askingFake)["callId"], .string("call-1"))
    }

    func testTheCallsGoingAreListed() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"ListGoingChatCalls":[\#(goingCall)]}}"#)
        let calls = try await connection.listGoingChatCalls()
        XCTAssertEqual(calls.map(\.id), ["call-1"])
        XCTAssertTrue(try sentDocument(fake).contains("ListGoingChatCalls"))
    }

    func testAChatWithNoCallGoingReadsNil() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"ReadGoingChatCall":null}}"#)
        let call = try await connection.readGoingChatCall(conversationId: "conversation-1")
        XCTAssertNil(call)
        XCTAssertEqual(try sentVariables(fake)["conversationId"], .string("conversation-1"))
    }

    func testAChatWithACallGoingReadsIt() async throws {
        let (connection, _) = try makeConnection(answering: #"{"data":{"ReadGoingChatCall":\#(goingCall)}}"#)
        let call = try await connection.readGoingChatCall(conversationId: "conversation-1")
        XCTAssertEqual(call?.onCount, 2)
    }

    func testAChatsCallsAreListedAPageAtATime() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"ListChatCalls":[\#(goingCall)]}}"#)
        _ = try await connection.listChatCalls(conversationId: "conversation-1", first: 20, before: Date(timeIntervalSince1970: 1_900_000_000))
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["first"], .number(20))
        XCTAssertNotNil(variables["before"]?.stringValue)

        _ = try await connection.listChatCalls(conversationId: "conversation-1")
        XCTAssertNil(try sentVariables(fake)["first"])
        XCTAssertNil(try sentVariables(fake)["before"])
    }

    func testACallIsReadWithItsTranscript() async throws {
        let body = #"""
            {"data":{"ReadChatCall":{"call":\#(goingCall),"transcript":[
              {"id":"s1","callId":"call-1","userId":"user-mira","speakerName":"Mira","startMs":4000,"endMs":9000,"text":"Shall we leave at nine?"},
              {"id":"s2","callId":"call-1","userId":null,"speakerName":"Tobias","startMs":65000,"endMs":70000,"text":"Nine works."}]}}}
            """#
        let (connection, fake) = try makeConnection(answering: body)
        let detail = try await connection.readChatCall(callId: "call-1")
        XCTAssertEqual(detail.call.id, "call-1")
        XCTAssertEqual(detail.transcript.map(\.speakerName), ["Mira", "Tobias"])
        XCTAssertEqual(detail.transcript.map(\.startText), ["0:04", "1:05"])
        XCTAssertNil(detail.transcript[1].userId)
        XCTAssertEqual(try sentVariables(fake)["callId"], .string("call-1"))
    }

    func testThePhoneIsRegisteredWithItsVoIPToken() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"RegisterAgentPhone":{"id":"phone-1","notificationKinds":[],"canBeNotified":true}}}"#)
        try await connection.registerPhone(
            phoneName: "Mira's iPhone", phoneModel: "iPhone17,1", pushRegistration: "alert-registration", voipPushRegistration: "voip-registration",
            payloadKey: "a-key")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["pushRegistration"], .string("alert-registration"))
        XCTAssertEqual(variables["voipPushRegistration"], .string("voip-registration"))
        XCTAssertTrue(try sentDocument(fake).contains("voipPushRegistration: $voipPushRegistration"))
    }

    func testARegistrationWithoutAVoIPTokenSendsNone() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"RegisterAgentPhone":{"id":"phone-1","notificationKinds":[],"canBeNotified":true}}}"#)
        try await connection.registerPhone(phoneName: "Mira's iPhone", phoneModel: "iPhone17,1")
        XCTAssertNil(try sentVariables(fake)["voipPushRegistration"])
    }

    func testTheVoIPTokenIsUpdatedOnItsOwn() async throws {
        let (connection, fake) = try makeConnection(answering: #"{"data":{"UpdateAgentPhone":{"id":"phone-1","notificationKinds":[],"canBeNotified":true}}}"#)
        try await connection.updatePhoneRinging("phone-1", voipPushRegistration: "voip-registration")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["phoneId"], .string("phone-1"))
        XCTAssertEqual(variables["voipPushRegistration"], .string("voip-registration"))
        XCTAssertNil(variables["notificationKinds"])
    }

    // MARK: Words

    func testADurationIsMinutesAndSeconds() {
        XCTAssertEqual(ChatCallDuration.text(milliseconds: 0), "0:00")
        XCTAssertEqual(ChatCallDuration.text(milliseconds: 9_999), "0:09")
        XCTAssertEqual(ChatCallDuration.text(milliseconds: 252_000), "4:12")
        XCTAssertEqual(ChatCallDuration.text(milliseconds: 3_599_000), "59:59")
        XCTAssertEqual(ChatCallDuration.text(milliseconds: 3_852_000), "1:04:12")
        XCTAssertEqual(ChatCallDuration.text(milliseconds: -5), "0:00")
    }

    func testTheLinesOfACallInEachState() throws {
        func kind(_ reason: String, isGoing: Bool = false, durationMs: Int64 = 0, isStarted: Bool = false, isAnswered: Bool = false)
            -> ChatCallLineKind
        {
            ChatCallLineKind.of(endReason: reason, isGoing: isGoing, durationMs: durationMs, isStartedByPerson: isStarted, isAnswered: isAnswered)
        }
        XCTAssertEqual(kind("", isGoing: true).englishText, "Call going")
        XCTAssertEqual(kind("ended", durationMs: 252_000, isAnswered: true).englishText, "Call, 4:12")
        XCTAssertEqual(kind("missed").englishText, "Missed call")
        XCTAssertEqual(kind("missed", isStarted: true).englishText, "No answer")
        XCTAssertEqual(kind("declined").englishText, "Declined call")
        XCTAssertEqual(kind("declined", isStarted: true).englishText, "Declined call")
        XCTAssertEqual(kind("failed").englishText, "Call failed")
        // Hung up while it rang: nobody answered, whatever the reason says.
        XCTAssertEqual(kind("ended", isAnswered: false).englishText, "Missed call")
        XCTAssertEqual(kind("ended", isStarted: true, isAnswered: false).englishText, "No answer")
    }

    func testTheLineIsWordedForWhoIsReadingIt() throws {
        let missed = try decodedCall(
            #"{"id":"c","startedByUserId":"user-mira","isGoing":false,"endReason":"missed","askerRingState":"missed","startedAt":"2030-01-01T10:00:00Z"}"#)
        XCTAssertEqual(ChatCallLine(view: missed, ownUserId: "user-tobias").kind, .missed)
        XCTAssertEqual(ChatCallLine(view: missed, ownUserId: "user-mira").kind, .noAnswer)
        let going = try decodedCall(goingCall)
        let line = ChatCallLine(view: going, ownUserId: "user-tobias")
        XCTAssertEqual(line.kind, .going)
        XCTAssertEqual(line.onCount, 2)
        XCTAssertTrue(line.isGoing)
        XCTAssertFalse(line.isTranscriptDone)
    }

    func testTheLineOfACallStillGoingReadsFromTheMessage() throws {
        let record = try JSONDecoder().decode(
            ChatCallRecord.self,
            from: Data(
                #"{"id":"c","startedByUserId":"user-mira","startedAt":"2030-01-01T10:00:00Z","answeredAt":null,"endedAt":null,"endReason":"","members":[{"userId":"user-mira","ringState":"none","joinedAt":"2030-01-01T10:00:00Z","leftAt":null}]}"#
                    .utf8))
        let line = ChatCallLine(record: record, ownUserId: "user-mira")
        XCTAssertEqual(line.kind, .going)
        XCTAssertEqual(line.onCount, 1)
        XCTAssertEqual(record.durationMs(), 0)
    }

    func testTheTranscriptionNoticeSaysWhoTurnedItOn() throws {
        let call = try decodedCall(goingCall)
        // Tobias is told Mira turned it on; nobody asks him.
        XCTAssertEqual(ChatCallTranscriptionStatus(view: call, ownUserId: "user-tobias"), .transcribing(askerName: "Mira", isAskedByPerson: false))
        // Mira turned it on herself.
        XCTAssertEqual(ChatCallTranscriptionStatus(view: call, ownUserId: "user-mira"), .transcribing(askerName: "Mira", isAskedByPerson: true))
        var nobodyAsked = call
        nobodyAsked.transcriptionAskedByUserId = nil
        XCTAssertEqual(ChatCallTranscriptionStatus(view: nobodyAsked, ownUserId: "user-tobias"), .notAsked)
        XCTAssertTrue(ChatCallTranscriptionStatus.notAsked.canAsk)
        XCTAssertFalse(ChatCallTranscriptionStatus.transcribing(askerName: "Mira", isAskedByPerson: false).canAsk)
        var ended = call
        ended.isGoing = false
        XCTAssertEqual(ChatCallTranscriptionStatus(view: ended, ownUserId: "user-tobias"), .notAsked)
    }

    func testThePeopleOnACallAreLaidOutInColumns() {
        XCTAssertEqual((0...8).map { ChatCallGrid.columnCount(tileCount: $0) }, [1, 1, 1, 2, 2, 2, 2, 3, 3])
        XCTAssertEqual((0...8).map { ChatCallGrid.rowCount(tileCount: $0) }, [0, 1, 2, 2, 2, 3, 3, 3, 3])
    }

    // MARK: The signaling

    func testTheDeviceJoinsFirst() {
        var signaling = ChatCallSignaling()
        XCTAssertEqual(signaling.start(), [.join])
        XCTAssertEqual(signaling.phase, .joining)
        XCTAssertEqual(signaling.start(), [])
    }

    func testAnOfferBeforeTheJoinsAnswerWaitsForIt() {
        var signaling = ChatCallSignaling()
        _ = signaling.start()
        XCTAssertEqual(signaling.receive(offer: "offer-1"), [])
        XCTAssertEqual(signaling.receive(offer: "offer-2"), [])
        XCTAssertEqual(signaling.joinAnswered(), [.answer(offerSdp: "offer-1")])
        XCTAssertEqual(signaling.phase, .answering)
        XCTAssertEqual(signaling.waitingOffers, ["offer-2"])
    }

    func testOffersAreAnsweredOneAtATimeInOrder() {
        var signaling = ChatCallSignaling()
        _ = signaling.start()
        _ = signaling.joinAnswered()
        XCTAssertEqual(signaling.receive(offer: "offer-1"), [.answer(offerSdp: "offer-1")])
        XCTAssertEqual(signaling.receive(offer: "offer-2"), [])
        XCTAssertEqual(signaling.receive(offer: "offer-3"), [])
        XCTAssertEqual(signaling.answerSent(), [.answer(offerSdp: "offer-2")])
        XCTAssertEqual(signaling.answerSent(), [.answer(offerSdp: "offer-3")])
        XCTAssertEqual(signaling.answerSent(), [])
        XCTAssertEqual(signaling.phase, .ready)
        XCTAssertEqual(signaling.receive(offer: "offer-4"), [.answer(offerSdp: "offer-4")])
    }

    func testAnAnswerIsOnlyCountedWhenOneIsBeingMade() {
        var signaling = ChatCallSignaling()
        XCTAssertEqual(signaling.answerSent(), [])
        XCTAssertEqual(signaling.joinAnswered(), [])
        _ = signaling.start()
        XCTAssertEqual(signaling.answerSent(), [])
        XCTAssertEqual(signaling.phase, .joining)
    }

    func testAFailureStopsEverything() {
        var signaling = ChatCallSignaling()
        _ = signaling.start()
        _ = signaling.receive(offer: "offer-1")
        signaling.fail()
        XCTAssertEqual(signaling.phase, .failed)
        XCTAssertEqual(signaling.joinAnswered(), [])
        XCTAssertEqual(signaling.receive(offer: "offer-2"), [])
        XCTAssertTrue(signaling.waitingOffers.isEmpty)
    }

    func testTheSectionsOfAnOfferTellWhoseTrackEachIs() {
        let offer = [
            "v=0", "o=- 1 2 IN IP4 127.0.0.1", "s=-", "t=0 0",
            "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:0", "a=sendonly", "a=msid:user-tobias track-a",
            "m=video 9 UDP/TLS/RTP/SAVPF 96", "a=mid:1", "a=sendonly", "a=msid:user-tobias track-v",
            "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:2", "a=recvonly",
            "m=video 9 UDP/TLS/RTP/SAVPF 96", "a=mid:3", "a=inactive", "a=msid:- -",
        ].joined(separator: "\r\n") + "\r\n"
        let sections = ChatCallSDP.sections(in: offer)
        XCTAssertEqual(sections.map(\.mid), ["0", "1", "2", "3"])
        XCTAssertEqual(sections.map(\.mediaKind), ["audio", "video", "audio", "video"])
        XCTAssertEqual(sections.map(\.streamId), ["user-tobias", "user-tobias", nil, nil])
        XCTAssertEqual(sections.map(\.isSending), [true, true, false, false])
    }

    // MARK: Ringing

    private func content(_ kind: String, callId: String? = "call-1", title: String = "Mira") throws -> NotificationContent {
        let callText = callId.map { #""callId":"\#($0)","# } ?? ""
        let json = #"{"notificationKind":"\#(kind)","conversationId":"conversation-1",\#(callText)"title":"\#(title)","body":"\#(title) is calling"}"#
        return try JSONDecoder().decode(NotificationContent.self, from: Data(json.utf8))
    }

    func testACallRingsOnce() throws {
        var ledger = ChatCallLedger()
        let first = UUID(), second = UUID()
        let action = VoIPPush.decide(content: try content("call"), accountKey: "account-1", ledger: &ledger, uuid: first)
        XCTAssertEqual(
            action, .ring(uuid: first, callId: "call-1", conversationId: "conversation-1", callerName: "Mira", accountKey: "account-1"))
        XCTAssertEqual(ledger.ringingEntries.count, 1)
        // The same call again is reported and ended at once, not rung twice.
        XCTAssertEqual(VoIPPush.decide(content: try content("call"), accountKey: "account-1", ledger: &ledger, uuid: second), .reportAndEnd(uuid: second))
        XCTAssertEqual(ledger.ringingEntries.count, 1)
    }

    func testAnEndedPushStopsTheRingForACallItRings() throws {
        var ledger = ChatCallLedger()
        let ringing = UUID()
        _ = VoIPPush.decide(content: try content("call"), accountKey: "account-1", ledger: &ledger, uuid: ringing)
        let action = VoIPPush.decide(content: try content("call_ended"), accountKey: "account-1", ledger: &ledger)
        XCTAssertEqual(action, .stopRinging(uuid: ringing))
        XCTAssertTrue(ledger.entries.isEmpty)
    }

    func testAnEndedPushForACallItDoesNotKnowIsReportedAndEnded() throws {
        var ledger = ChatCallLedger()
        let fresh = UUID()
        XCTAssertEqual(
            VoIPPush.decide(content: try content("call_ended", callId: "call-9"), accountKey: "account-1", ledger: &ledger, uuid: fresh),
            .reportAndEnd(uuid: fresh))
        // And a ring that comes late for that call is not shown.
        let late = UUID()
        XCTAssertEqual(
            VoIPPush.decide(content: try content("call", callId: "call-9"), accountKey: "account-1", ledger: &ledger, uuid: late),
            .reportAndEnd(uuid: late))
    }

    func testAnEndedPushForTheCallThePhoneIsOnLeavesItAlone() throws {
        var ledger = ChatCallLedger()
        let ringing = UUID()
        _ = VoIPPush.decide(content: try content("call"), accountKey: "account-1", ledger: &ledger, uuid: ringing)
        XCTAssertNotNil(ledger.answer(uuid: ringing))
        // The server tells the phones of the person that it was answered, this one too.
        let fresh = UUID()
        XCTAssertEqual(
            VoIPPush.decide(content: try content("call_ended"), accountKey: "account-1", ledger: &ledger, uuid: fresh), .reportAndEnd(uuid: fresh))
        XCTAssertEqual(ledger.entry(uuid: ringing)?.phase, .joining)
    }

    func testAPushThatCannotBeReadIsStillReported() throws {
        var ledger = ChatCallLedger()
        let fresh = UUID()
        XCTAssertEqual(VoIPPush.decide(content: nil, accountKey: nil, ledger: &ledger, uuid: fresh), .reportAndEnd(uuid: fresh))
        XCTAssertEqual(
            VoIPPush.decide(content: try content("call", callId: nil), accountKey: "account-1", ledger: &ledger, uuid: fresh), .reportAndEnd(uuid: fresh))
        XCTAssertEqual(
            VoIPPush.decide(content: try content("chat"), accountKey: "account-1", ledger: &ledger, uuid: fresh), .reportAndEnd(uuid: fresh))
        XCTAssertTrue(ledger.entries.isEmpty)
    }

    func testACallAnsweredElsewhereStopsRinging() throws {
        var ledger = ChatCallLedger()
        let ringing = UUID()
        _ = ledger.ringIncoming(callId: "call-1", conversationId: "conversation-1", title: "Mira", accountKey: "account-1", uuid: ringing)
        var view = try decodedCall(goingCall)
        view.askerRingState = "ringing"
        XCTAssertEqual(ledger.apply(view: view), .nothing)
        view.askerRingState = "answered"
        XCTAssertEqual(ledger.apply(view: view), .end(uuid: ringing, reason: .answeredElsewhere))
        XCTAssertTrue(ledger.entries.isEmpty)
    }

    func testACallDeclinedElsewhereOrMissedStopsRinging() throws {
        var ledger = ChatCallLedger()
        let declined = UUID(), missed = UUID()
        _ = ledger.ringIncoming(callId: "call-1", conversationId: "conversation-1", title: "Mira", accountKey: "account-1", uuid: declined)
        var view = try decodedCall(goingCall)
        view.askerRingState = "declined"
        XCTAssertEqual(ledger.apply(view: view), .end(uuid: declined, reason: .declinedElsewhere))
        _ = ledger.ringIncoming(callId: "call-2", conversationId: "conversation-1", title: "Mira", accountKey: "account-1", uuid: missed)
        var ended = try decodedCall(goingCall)
        ended.id = "call-2"
        ended.isGoing = false
        ended.endReason = "missed"
        XCTAssertEqual(ledger.apply(view: ended), .end(uuid: missed, reason: .unanswered))
    }

    func testACallTheServerEndsEndsForThePhoneOnIt() throws {
        var ledger = ChatCallLedger()
        let uuid = UUID()
        _ = ledger.ringIncoming(callId: "call-1", conversationId: "conversation-1", title: "Mira", accountKey: "account-1", uuid: uuid)
        _ = ledger.answer(uuid: uuid)
        ledger.connected(uuid: uuid)
        var view = try decodedCall(goingCall)
        XCTAssertEqual(ledger.apply(view: view), .nothing)
        view.isGoing = false
        view.endReason = "ended"
        XCTAssertEqual(ledger.apply(view: view), .end(uuid: uuid, reason: .remoteEnded))
        XCTAssertNil(ledger.activeEntry)
    }

    func testACallAPersonStartsIsKnownOnceTheServerMakesIt() throws {
        var ledger = ChatCallLedger()
        let uuid = UUID()
        ledger.place(conversationId: "conversation-1", title: "Weekend plans", accountKey: "account-1", uuid: uuid)
        XCTAssertEqual(ledger.activeEntry?.phase, .placing)
        XCTAssertNil(ledger.entry(uuid: uuid)?.callId)
        XCTAssertNil(ledger.attach(callId: "call-1", to: uuid))
        XCTAssertEqual(ledger.entry(callId: "call-1")?.uuid, uuid)
    }

    func testStartingACallAlreadyRingingHereTakesOverTheRing() {
        var ledger = ChatCallLedger()
        let ringing = UUID(), placed = UUID()
        _ = ledger.ringIncoming(callId: "call-1", conversationId: "conversation-1", title: "Mira", accountKey: "account-1", uuid: ringing)
        ledger.place(conversationId: "conversation-1", title: "Weekend plans", accountKey: "account-1", uuid: placed)
        XCTAssertEqual(ledger.attach(callId: "call-1", to: placed), ringing)
        XCTAssertEqual(ledger.entries.map(\.uuid), [placed])
    }

    func testReadingTheCallsGoingEndsTheRingsThatAreOver() throws {
        var ledger = ChatCallLedger()
        let kept = UUID(), gone = UUID()
        _ = ledger.ringIncoming(callId: "call-1", conversationId: "conversation-1", title: "Mira", accountKey: "account-1", uuid: kept)
        _ = ledger.ringIncoming(callId: "call-2", conversationId: "conversation-1", title: "Mira", accountKey: "account-1", uuid: gone)
        var stillRinging = try decodedCall(goingCall)
        stillRinging.askerRingState = "ringing"
        let reactions = ledger.reconcile(going: [stillRinging])
        XCTAssertEqual(reactions, [.end(uuid: gone, reason: .remoteEnded)])
        XCTAssertEqual(ledger.entries.map(\.uuid), [kept])
    }
}
