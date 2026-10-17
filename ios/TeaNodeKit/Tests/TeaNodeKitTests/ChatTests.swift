import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// ChatTests cover chats in the conversation screens: what the server's
/// answers decode to, how a chat's messages become lines (who said each, which
/// begin a run, who has read as far as which), how its events change them,
/// and what each operation sends.
final class ChatTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "person-a")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    /// A direct chat of person-a with person-b, as the list of conversations
    /// answers it, who has read nothing yet.
    private let chatAnswer = #"""
        {"id":"conversation-1","kind":"chat","jobKind":null,"title":"","summary":"","lastAt":"2030-01-01T10:00:00Z","archivedAt":null,
         "goal":null,"goalState":null,"goalNote":null,
         "chat":{"chatId":"chat-1","chatKind":"direct","circleId":null,"title":"","isCircleChat":false,"unreadCount":2,"isMuted":false,
           "mutedUntil":null,"isRemoved":false,
           "members":[
             {"userId":"user-a","username":"person-a","name":"Person A","avatarUrl":null,"avatarColorIndex":4,"memberRole":"member",
              "isDeletedAccount":false,"isRemoved":false,"readThroughMessageId":"","isAgentReading":false},
             {"userId":"user-b","username":"person-b","name":"","avatarUrl":"/api/v1/avatar/user/user-b?v=2","avatarColorIndex":7,"memberRole":"member",
              "isDeletedAccount":false,"isRemoved":false,"readThroughMessageId":"01J0000000000000000000002","isAgentReading":false}]}}
        """#

    private func message(
        _ id: String, _ content: String, authorUserId: String, authorName: String, isMine: Bool, at: String = "2030-01-01T10:00:00Z",
        authorKind: String = "person", extra: String = ""
    ) -> String {
        #"""
        {"id":"\#(id)","createdAt":"\#(at)","role":"\#(isMine ? "user" : "assistant")","content":"\#(content)","authorKind":"\#(authorKind)",
         "authorUserId":"\#(authorUserId)","authorName":"\#(authorName)","authorAvatarUrl":null,"authorAvatarColorIndex":4,
         "authorAgentId":"","isMine":\#(isMine),"editedAt":null,"isDeleted":false\#(extra)}
        """#
    }

    private func messages(_ bodies: [String]) throws -> [AgentMessage] {
        try JSONDecoder().decode([AgentMessage].self, from: Data("[\(bodies.joined(separator: ","))]".utf8))
    }

    // MARK: Reading

    func testAChatConversationDecodesWithItsPeople() async throws {
        let (connection, _) = connection(answering: #"{"data":{"ListAgentConversations":[\#(chatAnswer)]}}"#)
        let conversations = try await connection.conversations()
        let conversation = try XCTUnwrap(conversations.first)
        XCTAssertTrue(conversation.isChat)
        let chat = try XCTUnwrap(conversation.chat)
        XCTAssertEqual(chat.unreadCount, 2)
        XCTAssertEqual(chat.members.map(\.displayName), ["Person A", "person-b"])
        XCTAssertEqual(ChatUnread.total(of: conversations), 2)
        XCTAssertFalse(chat.isGroup)
    }

    func testAConversationWithAnAgentHasNoChat() throws {
        let json = #"{"id":"c1","kind":"main","title":"","summary":"","lastAt":null,"chat":null}"#
        let conversation = try JSONDecoder().decode(AgentConversation.self, from: Data(json.utf8))
        XCTAssertNil(conversation.chat)
        XCTAssertFalse(conversation.isChat)
    }

    func testTheCountOnTheEntryLeavesOutMutedChats() throws {
        var muted = try JSONDecoder().decode(AgentConversation.self, from: Data(chatAnswer.utf8))
        XCTAssertEqual(ChatUnread.total(of: [muted]), 2)
        muted.chat?.isMuted = true
        XCTAssertEqual(ChatUnread.total(of: [muted]), 0)
    }

    func testADirectChatIsNamedByTheOtherPersonAndAGroupByItsTitle() throws {
        let conversation = try JSONDecoder().decode(AgentConversation.self, from: Data(chatAnswer.utf8))
        let chat = try XCTUnwrap(conversation.chat)
        XCTAssertEqual(chat.name(ownUserId: "user-a", deletedAccountNote: "(deleted account)", fallback: "Chat"), "person-b")
        let group = ChatSummary(chatId: "chat-2", chatKind: "group", title: "Fixture Home", members: chat.members)
        XCTAssertEqual(group.name(ownUserId: "user-a", deletedAccountNote: "(deleted account)", fallback: "Chat"), "Fixture Home")
        let untitled = ChatSummary(chatId: "chat-3", chatKind: "group", members: chat.members)
        XCTAssertEqual(untitled.name(ownUserId: "user-b", deletedAccountNote: "(deleted account)", fallback: "Chat"), "Person A")
    }

    func testAPersonWhoseAccountIsGoneKeepsTheirNameWithANote() {
        let gone = ChatMember(userId: "", username: "", name: "Person C", isDeletedAccount: true)
        XCTAssertEqual(ChatSummary.name(of: gone, deletedAccountNote: "(deleted account)"), "Person C (deleted account)")
        let chat = ChatSummary(
            chatId: "chat-1", chatKind: "direct", members: [ChatMember(userId: "user-a", username: "person-a"), gone])
        XCTAssertEqual(chat.name(ownUserId: "user-a", deletedAccountNote: "(deleted account)", fallback: "Chat"), "Person C (deleted account)")
    }

    func testAGroupShowsOnlyThoseStillInItAndADirectChatKeepsTheOtherPerson() {
        let left = ChatMember(userId: "user-b", username: "person-b", isRemoved: true)
        let staying = ChatMember(userId: "user-c", username: "person-c")
        let me = ChatMember(userId: "user-a", username: "person-a", memberRole: "admin")
        let group = ChatSummary(chatId: "g", chatKind: "group", members: [me, staying, left])
        XCTAssertEqual(group.otherMembers(ownUserId: "user-a").map(\.userId), ["user-c"])
        let direct = ChatSummary(chatId: "d", chatKind: "direct", members: [me, left])
        XCTAssertEqual(direct.otherMembers(ownUserId: "user-a").map(\.userId), ["user-b"])
    }

    func testOnlyAnAdminOfAGroupThatIsNotACirclesChangesWhoIsInIt() {
        let admin = ChatMember(userId: "user-a", username: "person-a", memberRole: "admin")
        let member = ChatMember(userId: "user-b", username: "person-b")
        let group = ChatSummary(chatId: "g", chatKind: "group", members: [admin, member])
        XCTAssertTrue(group.canChangeMembers(ownUserId: "user-a"))
        XCTAssertFalse(group.canChangeMembers(ownUserId: "user-b"))
        XCTAssertTrue(group.canBeLeft)
        let circles = ChatSummary(chatId: "c", chatKind: "group", isCircleChat: true, members: [admin, member])
        XCTAssertFalse(circles.canChangeMembers(ownUserId: "user-a"))
        XCTAssertFalse(circles.canBeLeft)
        XCTAssertFalse(circles.canBeRenamed)
        let direct = ChatSummary(chatId: "d", chatKind: "direct", members: [admin, member])
        XCTAssertFalse(direct.canChangeMembers(ownUserId: "user-a"))
        XCTAssertFalse(direct.canBeLeft)
    }

    // MARK: The transcript of a chat

    func testAChatsMessagesAreLinesWithTheirAuthors() throws {
        let stored = try messages([
            message("01J0000000000000000000001", "Dinner at seven?", authorUserId: "user-b", authorName: "Person B", isMine: false),
            message("01J0000000000000000000002", "Sounds good", authorUserId: "user-a", authorName: "Person A", isMine: true),
        ])
        let transcript = Transcript(messages: stored, isChat: true)
        XCTAssertEqual(transcript.lines.map(\.kind), [.person, .person])
        XCTAssertEqual(transcript.lines.map(\.text), ["Dinner at seven?", "Sounds good"])
        XCTAssertEqual(transcript.lines.map { $0.author?.isOwnWords }, [false, true])
        XCTAssertEqual(transcript.lines.first?.author?.authorName, "Person B")
    }

    func testTheSameMessagesInAConversationWithAnAgentAreDrawnAsTheyAlwaysWere() throws {
        let stored = try messages([
            message("01J0000000000000000000001", "Dinner at seven?", authorUserId: "user-a", authorName: "Person A", isMine: true),
            message("01J0000000000000000000002", "Booked.", authorUserId: "", authorName: "Robin", isMine: false, authorKind: "agent"),
        ])
        let transcript = Transcript(messages: stored)
        XCTAssertEqual(transcript.lines.map(\.kind), [.person, .answer])
        XCTAssertNil(transcript.lines.first?.author)
    }

    func testAnAgentsWordsInAChatAreAnAnswerUnderItsNameEvenWhenTheyAreTheirOwners() throws {
        let stored = try messages([
            message("01J0000000000000000000001", "Tell them I will be late", authorUserId: "user-a", authorName: "Robin", isMine: true, authorKind: "agent")
        ])
        let line = try XCTUnwrap(Transcript(messages: stored, isChat: true).lines.first)
        XCTAssertEqual(line.kind, .answer)
        XCTAssertTrue(line.author?.isAgent ?? false)
        XCTAssertFalse(line.author?.isOwnWords ?? true)
    }

    func testARowOfTheMembersOwnAgentsWorkInAChatIsNotAMessageOfTheChat() throws {
        let stored = try messages([
            message("01J0000000000000000000001", "Hello", authorUserId: "user-b", authorName: "Person B", isMine: false),
            #"{"id":"01J0000000000000000000002","createdAt":"2030-01-01T10:00:01Z","role":"note","content":"looked it up","name":"depth"}"#,
        ])
        let transcript = Transcript(messages: stored, isChat: true)
        XCTAssertEqual(transcript.lines.map(\.kind), [.person, .note])
    }

    func testAnAuthorIsNamedOnceForARun() throws {
        let stored = try messages([
            message("01J0000000000000000000001", "One", authorUserId: "user-b", authorName: "Person B", isMine: false, at: "2030-01-01T10:00:00Z"),
            message("01J0000000000000000000002", "Two", authorUserId: "user-b", authorName: "Person B", isMine: false, at: "2030-01-01T10:00:30Z"),
            message("01J0000000000000000000003", "Three", authorUserId: "user-a", authorName: "Person A", isMine: true, at: "2030-01-01T10:01:00Z"),
            message("01J0000000000000000000004", "Four", authorUserId: "user-b", authorName: "Person B", isMine: false, at: "2030-01-01T10:01:30Z"),
            message("01J0000000000000000000005", "Five", authorUserId: "user-b", authorName: "Person B", isMine: false, at: "2030-01-01T11:30:00Z"),
        ])
        let lines = Transcript(messages: stored, isChat: true).lines
        let begins = lines.indices.map { lines[$0].beginsRun(after: $0 == 0 ? nil : lines[$0 - 1]) }
        // The second of Person B's two in a row continues the run; the one
        // after the reply begins another; the one an hour and a half later
        // is named again.
        XCTAssertEqual(begins, [true, false, true, true, true])
    }

    func testTheFirstMessageUnderADateDividerIsNamedAgainBySamePerson() throws {
        var utc = Calendar(identifier: .gregorian)
        utc.timeZone = TimeZone(identifier: "UTC")!
        let stored = try messages([
            message("01J0000000000000000000001", "Late", authorUserId: "user-b", authorName: "Person B", isMine: false, at: "2030-01-01T23:59:00Z"),
            message("01J0000000000000000000002", "Early", authorUserId: "user-b", authorName: "Person B", isMine: false, at: "2030-01-02T00:01:00Z"),
            message("01J0000000000000000000003", "Soon after", authorUserId: "user-b", authorName: "Person B", isMine: false, at: "2030-01-02T00:02:00Z"),
        ])
        let lines = Transcript(messages: stored, isChat: true).lines
        let begins = lines.indices.map { lines[$0].beginsRun(after: $0 == 0 ? nil : lines[$0 - 1], calendar: utc) }
        // Two minutes apart across midnight: the divider between them starts a
        // new run. The third is the same day as the second and continues it.
        XCTAssertEqual(begins, [true, true, false])
    }

    func testEachOtherMemberIsDrawnUnderTheNewestMessageBySomebodyElseTheyHaveRead() throws {
        let stored = try messages([
            message("01J0000000000000000000001", "One", authorUserId: "user-b", authorName: "Person B", isMine: false),
            message("01J0000000000000000000002", "Two", authorUserId: "user-b", authorName: "Person B", isMine: false),
            message("01J0000000000000000000003", "Three", authorUserId: "user-a", authorName: "Person A", isMine: true),
        ])
        let transcript = Transcript(messages: stored, isChat: true)
        let members = [
            ChatMember(userId: "user-a", username: "person-a", readThroughMessageId: "01J0000000000000000000003"),
            ChatMember(userId: "user-b", username: "person-b", readThroughMessageId: "01J0000000000000000000003"),
            ChatMember(userId: "user-c", username: "person-c", readThroughMessageId: "01J0000000000000000000002"),
            ChatMember(userId: "user-d", username: "person-d", readThroughMessageId: ""),
            ChatMember(userId: "user-e", username: "person-e", isRemoved: true, readThroughMessageId: "01J0000000000000000000003"),
        ]
        let readers = transcript.readers(of: members, ownUserId: "user-a")
        // Person B read through the signed-in person's message, so is under
        // it; Person C read through Person B's second, so is under it; and
        // neither is under one of their own.
        XCTAssertEqual(readers["01J0000000000000000000003"]?.map(\.userId), ["user-b"])
        XCTAssertEqual(readers["01J0000000000000000000002"]?.map(\.userId), ["user-c"])
        XCTAssertNil(readers["01J0000000000000000000001"])
        XCTAssertEqual(transcript.newestChatMessageId, "01J0000000000000000000003")
    }

    func testAMemberWhoHasReadOnlyTheirOwnMessagesIsUnderNone() throws {
        let stored = try messages([
            message("01J0000000000000000000001", "One", authorUserId: "user-b", authorName: "Person B", isMine: false),
            message("01J0000000000000000000002", "Two", authorUserId: "user-b", authorName: "Person B", isMine: false),
        ])
        let transcript = Transcript(messages: stored, isChat: true)
        let members = [ChatMember(userId: "user-b", username: "person-b", readThroughMessageId: "01J0000000000000000000002")]
        XCTAssertTrue(transcript.readers(of: members, ownUserId: "user-a").isEmpty)
    }

    // MARK: Events

    private func event(_ json: String) throws -> AgentEvent {
        try JSONDecoder().decode(AgentEvent.self, from: Data(json.utf8))
    }

    func testASaidEventDecodesAndAddsTheMessageOnce() throws {
        var transcript = Transcript(messages: [], isChat: true)
        let said = try event(
            #"{"kind":"said","runId":"","sequence":0,"message":\#(message("01J0000000000000000000004", "See you there", authorUserId: "user-b", authorName: "Person B", isMine: false))}"#
        )
        XCTAssertEqual(said.message?.authorName, "Person B")
        transcript.apply(said)
        transcript.apply(said)
        XCTAssertEqual(transcript.lines.map(\.text), ["See you there"])
    }

    func testTheChatsCopyOfAMessageJustSentReplacesTheLineDrawnWhileItWasOnItsWay() throws {
        var transcript = Transcript(messages: [], isChat: true)
        let me = ChatAuthor(authorKind: "person", authorUserId: "user-a", authorName: "Person A", isMine: true)
        let sentId = transcript.addSent("On my way", author: me)
        XCTAssertEqual(transcript.lines.map(\.id), [sentId])
        transcript.apply(
            try event(
                #"{"kind":"said","runId":"","sequence":0,"message":\#(message("01J0000000000000000000005", "On my way", authorUserId: "user-a", authorName: "Person A", isMine: true))}"#
            ))
        XCTAssertEqual(transcript.lines.map(\.id), ["01J0000000000000000000005"])
        XCTAssertEqual(transcript.lines.first?.messageId, "01J0000000000000000000005")
    }

    func testAMessageEditedShowsItsNewWordsAndTheMark() throws {
        var transcript = Transcript(
            messages: try messages([message("01J0000000000000000000001", "Dinner at seven?", authorUserId: "user-b", authorName: "Person B", isMine: false)]),
            isChat: true)
        transcript.apply(
            try event(
                #"{"kind":"edited","runId":"","sequence":0,"message":\#(message("01J0000000000000000000001", "Dinner at eight?", authorUserId: "user-b", authorName: "Person B", isMine: false).replacingOccurrences(of: #""editedAt":null"#, with: #""editedAt":"2030-01-01T10:05:00Z""#))}"#
            ))
        XCTAssertEqual(transcript.lines.first?.text, "Dinner at eight?")
        XCTAssertEqual(transcript.lines.first?.editedAt, "2030-01-01T10:05:00Z")
    }

    func testAMessageTakenBackIsAMarkThatItWas() throws {
        var transcript = Transcript(
            messages: try messages([
                message(
                    "01J0000000000000000000001", "Dinner at seven?", authorUserId: "user-b", authorName: "Person B", isMine: false,
                    extra: #","reactions":[{"emoji":"👍","reactorKind":"person","reactedAt":null,"reactorUserId":"user-a","reactorName":"Person A"}]"#)
            ]),
            isChat: true)
        XCTAssertEqual(transcript.lines.first?.reactions.count, 1)
        transcript.apply(try event(#"{"kind":"unsaid","runId":"","sequence":0,"messageId":"01J0000000000000000000001"}"#))
        XCTAssertEqual(transcript.lines.count, 1)
        XCTAssertEqual(transcript.lines.first?.text, "")
        XCTAssertTrue(transcript.lines.first?.isDeleted ?? false)
        XCTAssertTrue(transcript.lines.first?.reactions.isEmpty ?? false)
    }

    func testWordsStreamedByATurnThatStoppedAreTakenOffTheScreen() throws {
        var transcript = Transcript(messages: [], isChat: true)
        transcript.apply(
            try event(
                #"{"kind":"text","runId":"run-1","sequence":0,"text":"Thursday at","name":"Leaf","authorOwnerName":"Person A","authorAgentId":"agent-a"}"#
            ))
        XCTAssertEqual(transcript.lines.count, 1)
        transcript.apply(try event(#"{"kind":"unsaid","runId":"","sequence":0,"messageId":"streaming-run-1"}"#))
        XCTAssertTrue(transcript.lines.isEmpty)
    }

    func testEachPersonsReactionInAChatStaysAndReplacesOnlyTheirOwn() throws {
        var transcript = Transcript(
            messages: try messages([message("01J0000000000000000000001", "Dinner?", authorUserId: "user-b", authorName: "Person B", isMine: false)]),
            isChat: true)
        let reacted = { (userId: String, name: String, emoji: String) throws -> AgentEvent in
            try self.event(
                #"{"kind":"reacted","runId":"","sequence":0,"messageId":"01J0000000000000000000001","emoji":"\#(emoji)","reactorKind":"person","reactorUserId":"\#(userId)","reactorName":"\#(name)"}"#
            )
        }
        transcript.apply(try reacted("user-a", "Person A", "👍"))
        transcript.apply(try reacted("user-b", "Person B", "❤️"))
        XCTAssertEqual(transcript.lines.first?.reactions.map(\.emoji), ["👍", "❤️"])
        transcript.apply(try reacted("user-a", "Person A", "🎉"))
        XCTAssertEqual(transcript.lines.first?.reactions.map(\.emoji).sorted(), ["❤️", "🎉"].sorted())
        XCTAssertEqual(transcript.lines.first?.reaction(of: "person", userId: "user-a"), "🎉")
        XCTAssertEqual(transcript.lines.first?.reaction(of: "person", userId: "user-b"), "❤️")
        transcript.apply(try reacted("user-a", "Person A", ""))
        XCTAssertEqual(transcript.lines.first?.reactions.map(\.emoji), ["❤️"])
        XCTAssertNil(transcript.lines.first?.reaction(of: "person", userId: "user-a"))
    }

    func testAReadEventMovesAMembersMarkForwardAndNeverBack() throws {
        let conversation = try JSONDecoder().decode(AgentConversation.self, from: Data(chatAnswer.utf8))
        var chat = try XCTUnwrap(conversation.chat)
        chat = chat.withRead(userId: "user-b", messageId: "01J0000000000000000000009")
        XCTAssertEqual(chat.member("user-b")?.readThroughMessageId, "01J0000000000000000000009")
        chat = chat.withRead(userId: "user-b", messageId: "01J0000000000000000000003")
        XCTAssertEqual(chat.member("user-b")?.readThroughMessageId, "01J0000000000000000000009")
    }

    func testTypingShowsForAFewSecondsAndEndsWithTheMessage() {
        var typing = ChatTyping()
        let now = Date(timeIntervalSince1970: 1_000_000)
        typing.note(userId: "user-b", name: "Person B", at: now)
        XCTAssertEqual(typing.names(at: now.addingTimeInterval(2)), ["Person B"])
        XCTAssertTrue(typing.names(at: now.addingTimeInterval(6)).isEmpty)
        typing.prune(at: now.addingTimeInterval(6))
        XCTAssertTrue(typing.isEmpty(at: now))
        typing.note(userId: "user-b", name: "Person B", at: now)
        typing.clear(userId: "user-b")
        XCTAssertTrue(typing.isEmpty(at: now))
    }

    // MARK: Operations

    func testOpeningADirectChatAsksForThePersonAndAnswersTheConversation() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"OpenDirectChat":{"id":"conversation-9"}}}"#)
        let opened = try await connection.openDirectChat(with: "user-b")
        XCTAssertEqual(opened, "conversation-9")
        XCTAssertEqual(try sentVariables(try XCTUnwrap(fake.sent.last)), ["userId": .string("user-b")])
    }

    func testMakingAGroupSendsItsTrimmedNameAndPeople() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"CreateGroupChat":{"id":"conversation-10"}}}"#)
        var draft = GroupChatDraft()
        XCTAssertFalse(draft.canSubmit)
        draft.title = "  Weekend plans "
        XCTAssertFalse(draft.canSubmit)
        draft.userIds = ["user-c", "user-b"]
        XCTAssertTrue(draft.canSubmit)
        let created = try await connection.createGroupChat(draft)
        XCTAssertEqual(created, "conversation-10")
        XCTAssertEqual(
            try sentVariables(try XCTUnwrap(fake.sent.last)),
            ["title": .string("Weekend plans"), "userIds": .array([.string("user-b"), .string("user-c")])])
    }

    func testChangingAChatSendsWhatTheServerAsksFor() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"RenameChat":{"id":"c"},"MarkChatRead":true,"MuteChat":{"id":"c"},"SetChatTyping":true}}"#)
        try await connection.renameChat("c", title: " Dinner ")
        XCTAssertEqual(try sentVariables(try XCTUnwrap(fake.sent.last)), ["conversationId": .string("c"), "title": .string("Dinner")])
        try await connection.markChatRead("c", through: "01J0000000000000000000003")
        XCTAssertEqual(
            try sentVariables(try XCTUnwrap(fake.sent.last)),
            ["conversationId": .string("c"), "throughMessageId": .string("01J0000000000000000000003")])
        try await connection.muteChat("c", until: nil)
        XCTAssertEqual(try sentVariables(try XCTUnwrap(fake.sent.last)), ["conversationId": .string("c"), "mutedUntil": .null])
        try await connection.muteChat("c", until: Date(timeIntervalSince1970: 1_900_000_000))
        XCTAssertEqual(
            try sentVariables(try XCTUnwrap(fake.sent.last)),
            ["conversationId": .string("c"), "mutedUntil": .string("2030-03-17T17:46:40Z")])
        try await connection.setChatTyping("c")
        XCTAssertEqual(try sentVariables(try XCTUnwrap(fake.sent.last)), ["conversationId": .string("c")])
    }

    func testTheNotificationKindsIncludeChat() {
        XCTAssertTrue(AgentPhone.allNotificationKinds.contains("chat"))
    }

    // MARK: Agents in a chat

    /// A chat of person-a, whose agent is on, with person-b, who let the others ask theirs, person-c, who did not, and
    /// person-d, who did but whose agent is off.
    private func roomOfAgents() -> ChatSummary {
        func member(_ userId: String, agentName: String, isAskable: Bool, isOn: Bool = true) -> ChatMember {
            ChatMember(
                userId: userId, username: userId, name: userId.uppercased(), isAgentAskableByOthers: isAskable, agentId: "agent-\(userId)",
                agentName: agentName, isAgentActive: isOn)
        }
        return ChatSummary(
            chatId: "chat-1", chatKind: "group", title: "Trip",
            members: [
                member("user-a", agentName: "Annie", isAskable: false), member("user-b", agentName: "Benny", isAskable: true),
                member("user-c", agentName: "Cyrus", isAskable: false), member("user-d", agentName: "Dee", isAskable: true, isOn: false),
                ChatMember(userId: "user-e", username: "user-e", isRemoved: true, isAgentAskableByOthers: true, agentId: "agent-user-e", agentName: "Evie", isAgentActive: true),
            ])
    }

    func testTheAtOffersTheOwnAgentFirstAndThenThoseTheirOwnersLetTheOthersAsk() {
        let agents = roomOfAgents().addressableAgents(ownUserId: "user-a", deletedAccountNote: "(deleted)")
        XCTAssertEqual(agents.map(\.name), ["Annie", "Benny"])
        XCTAssertEqual(agents.map(\.isOwn), [true, false])
        // Somebody else sees their own, and Benny's, who let the others ask.
        XCTAssertEqual(roomOfAgents().addressableAgents(ownUserId: "user-c", deletedAccountNote: "").map(\.name), ["Cyrus", "Benny"])
    }

    func testAMemberWhoseAgentIsNotToldIsNotOffered() throws {
        let members = try JSONDecoder().decode(
            [ChatMember].self,
            from: Data(
                #"[{"userId":"user-b","username":"b","name":"B","avatarUrl":null,"avatarColorIndex":1,"memberRole":"member","isDeletedAccount":false,"isRemoved":false,"readThroughMessageId":"","isAgentReading":false,"isAgentAskableByOthers":false,"agentId":"","agentName":"","isAgentActive":false}]"#
                    .utf8))
        let chat = ChatSummary(chatId: "c", chatKind: "direct", members: members)
        XCTAssertTrue(chat.addressableAgents(ownUserId: "user-a", deletedAccountNote: "").isEmpty)
    }

    func testTheMentionBeingTypedIsTheAtAtTheEndThatStartsAWord() {
        XCTAssertEqual(ChatMention.query(in: "@"), "")
        XCTAssertEqual(ChatMention.query(in: "hello @Be"), "Be")
        XCTAssertNil(ChatMention.query(in: "mail me@example.com"))
        XCTAssertNil(ChatMention.query(in: "hello @Benny and"))
        XCTAssertNil(ChatMention.query(in: "no at sign"))
    }

    func testTheLettersNarrowTheAgentsAndChoosingPutsTheNameInTheWords() {
        let agents = roomOfAgents().addressableAgents(ownUserId: "user-a", deletedAccountNote: "")
        XCTAssertEqual(ChatMention.matching(agents, query: "").map(\.name), ["Annie", "Benny"])
        XCTAssertEqual(ChatMention.matching(agents, query: "be").map(\.name), ["Benny"])
        XCTAssertTrue(ChatMention.matching(agents, query: "x").isEmpty)
        XCTAssertEqual(ChatMention.choosing(agents[1], in: "so @Be"), "so @Benny ")
    }

    func testAMessageIsForTheAgentsChosenWhoseNamesAreStillInItAndTheOneRepliedTo() {
        let agents = roomOfAgents().addressableAgents(ownUserId: "user-a", deletedAccountNote: "")
        XCTAssertEqual(ChatMention.addressed(in: "@Annie and @Benny, hello", chosen: agents), ["agent-user-a", "agent-user-b"])
        // Taken out of the words, it is taken out of the message.
        XCTAssertEqual(ChatMention.addressed(in: "@Benny, hello", chosen: agents), ["agent-user-b"])
        // A name that only begins with it is another word, and one typed by hand addresses nothing.
        XCTAssertTrue(ChatMention.addressed(in: "@Anniefied", chosen: agents).isEmpty)
        XCTAssertTrue(ChatMention.addressed(in: "@Annie", chosen: []).isEmpty)
        XCTAssertEqual(ChatMention.addressed(in: "thanks", chosen: [], repliedToAgentId: "agent-user-b"), ["agent-user-b"])
        XCTAssertEqual(ChatMention.addressed(in: "@Benny thanks", chosen: agents, repliedToAgentId: "agent-user-b"), ["agent-user-b"])
    }

    func testAnAgentThinkingIsToldApartFromAPersonTyping() {
        var typing = ChatTyping()
        let now = Date(timeIntervalSince1970: 1_000_000)
        typing.note(agentId: "agent-1", name: "Robin", at: now)
        typing.note(userId: "user-b", name: "Person B", at: now)
        XCTAssertEqual(typing.agentSpeakers(at: now.addingTimeInterval(2), ownedWords: ownedWords), ["Robin"])
        XCTAssertEqual(typing.names(at: now.addingTimeInterval(2)), ["Person B"])
        XCTAssertFalse(typing.isEmpty(at: now))
        typing.clear(userId: "user-b")
        XCTAssertEqual(typing.agentSpeakers(at: now, ownedWords: ownedWords), ["Robin"])
        typing.clear(agentId: "agent-1")
        XCTAssertTrue(typing.isEmpty(at: now))
        typing.note(agentId: "agent-1", name: "Robin", at: now)
        typing.prune(at: now.addingTimeInterval(6))
        XCTAssertTrue(typing.agentSpeakers(at: now, ownedWords: ownedWords).isEmpty)
    }

    /// "Ada's agent", as the app words it in the person's language.
    private func ownedWords(_ ownerName: String) -> String { "\(ownerName)'s agent" }

    func testAnAgentIsNamedByWhoseItIsWhenTwoAreBothCalledAgent() {
        XCTAssertEqual(AgentSpeaker.name(agentName: "Bertie", ownerName: "Ada", ownedWords: ownedWords), "Bertie · Ada's agent")
        // The default name would only repeat the word.
        XCTAssertEqual(AgentSpeaker.name(agentName: "Agent", ownerName: "Ada", ownedWords: ownedWords), "Ada's agent")
        XCTAssertEqual(AgentSpeaker.name(agentName: "Agent", ownerName: "Ben", ownedWords: ownedWords), "Ben's agent")
        XCTAssertEqual(AgentSpeaker.name(agentName: "", ownerName: "Ben", ownedWords: ownedWords), "Ben's agent")
        // An agent whose owner is not known is called by its own name.
        XCTAssertEqual(AgentSpeaker.name(agentName: "Bertie", ownerName: " ", ownedWords: ownedWords), "Bertie")
    }

    func testTheThinkingLineNamesEachAgentByWhoseItIs() {
        var typing = ChatTyping()
        let now = Date(timeIntervalSince1970: 1_000_000)
        typing.note(agentId: "agent-1", name: "Agent", ownerName: "Ada", at: now)
        typing.note(agentId: "agent-2", name: "Bertie", ownerName: "Ben", at: now.addingTimeInterval(1))
        XCTAssertEqual(
            typing.agentSpeakers(at: now.addingTimeInterval(2), ownedWords: ownedWords), ["Ada's agent", "Bertie · Ben's agent"])
        XCTAssertFalse(typing.isEmpty(at: now))
        typing.clear(agentId: "agent-1")
        XCTAssertEqual(typing.agentSpeakers(at: now.addingTimeInterval(2), ownedWords: ownedWords), ["Bertie · Ben's agent"])
    }

    func testAnAgentsMessageAndItsWordsBeingWrittenCarryWhoseAgentItIs() throws {
        let stored = try JSONDecoder().decode(
            AgentMessage.self,
            from: Data(
                #"""
                {"id":"01J0000000000000000000030","createdAt":"2030-01-01T10:00:00Z","role":"assistant","content":"Free on Friday.",
                 "authorKind":"agent","authorUserId":"user-b","authorName":"Agent","authorOwnerName":"Person B","authorAgentId":"agent-b",
                 "agentRunId":"run-1","isMine":false,"isAgentWork":false}
                """#.utf8))
        var transcript = Transcript(messages: [], isChat: true)
        transcript.apply(
            AgentEvent(
                kind: "text", runId: "run-2", sequence: 0, text: "Busy", name: "Bertie", authorAgentId: "agent-c", authorOwnerName: "Person C"))
        transcript.apply(AgentEvent(kind: "said", runId: "", sequence: 0, message: stored))
        let names = transcript.lines.compactMap { $0.author?.speakerName(ownedWords: ownedWords) }
        XCTAssertEqual(names, ["Bertie · Person C's agent", "Person B's agent"])
        // A person is named as they are, and a message from before the owner was recorded still has a name.
        XCTAssertEqual(ChatAuthor(authorKind: "person", authorUserId: "u", authorName: "Ada").speakerName(ownedWords: ownedWords), "Ada")
        XCTAssertEqual(ChatAuthor(authorKind: "agent", authorName: "Agent").speakerName(ownedWords: ownedWords), "Agent")
    }

    func testWhatTheAgentDidToAnswerIsNotSaidInTheChat() throws {
        // The words of a round are the chat's; the call it made and what it found are the person's own agent's work.
        let stored = try JSONDecoder().decode(
            [AgentMessage].self,
            from: Data(
                #"""
                [{"id":"01J0000000000000000000010","createdAt":"2030-01-01T10:00:00Z","role":"assistant","content":"Let me look.",
                  "authorKind":"agent","authorUserId":"user-a","authorName":"Robin","authorAgentId":"agent-1","agentRunId":"run-1",
                  "isMine":true,"isAgentWork":false},
                 {"id":"01J0000000000000000000011","createdAt":"2030-01-01T10:00:01Z","role":"assistant","content":"",
                  "toolCalls":[{"id":"call-1","name":"calendar","arguments":"{}"}],
                  "authorKind":"agent","authorName":"Robin","authorAgentId":"agent-1","isMine":true,"isAgentWork":true},
                 {"id":"01J0000000000000000000012","createdAt":"2030-01-01T10:00:02Z","role":"tool","content":"{}","name":"calendar",
                  "toolCallId":"call-1","authorKind":"agent","authorName":"Robin","isMine":true,"isAgentWork":true}]
                """#.utf8))
        let transcript = Transcript(messages: stored, isChat: true)
        XCTAssertEqual(transcript.lines.map(\.kind), [.answer, .tool])
        XCTAssertEqual(transcript.lines.first?.author?.isAgent, true)
        XCTAssertEqual(transcript.lines.first?.runId, "run-1")
        XCTAssertEqual(transcript.lines.last?.detail, "calendar")
    }

    func testAnAgentsAnswerIsDrawnAsItIsWrittenAndReplacedByTheStoredMessage() throws {
        var transcript = Transcript(messages: [], isChat: true)
        func text(_ words: String) -> AgentEvent {
            AgentEvent(kind: "text", runId: "run-1", sequence: 0, text: words, name: "Robin", authorAgentId: "agent-1")
        }
        transcript.apply(text("Thurs"))
        transcript.apply(text("day at 3"))
        XCTAssertEqual(transcript.lines.count, 1)
        XCTAssertEqual(transcript.lines.first?.text, "Thursday at 3")
        XCTAssertEqual(transcript.lines.first?.isStreaming, true)
        XCTAssertEqual(transcript.lines.first?.author?.authorName, "Robin")
        // The turn's own end-of-answer event adds nothing in a chat; the chat says it.
        transcript.apply(AgentEvent(kind: "message", runId: "run-1", sequence: 3, text: "Thursday at 3."))
        XCTAssertEqual(transcript.lines.count, 1)
        // And the stored message takes the place of the words, where they stood.
        let stored = try JSONDecoder().decode(
            AgentMessage.self,
            from: Data(
                #"""
                {"id":"01J0000000000000000000020","createdAt":"2030-01-01T10:00:00Z","role":"assistant","content":"Thursday at 3.",
                 "authorKind":"agent","authorUserId":"user-a","authorName":"Robin","authorAgentId":"agent-1","agentRunId":"run-1",
                 "isMine":true,"isAgentWork":false}
                """#.utf8))
        transcript.apply(AgentEvent(kind: "said", runId: "", sequence: 0, message: stored))
        XCTAssertEqual(transcript.lines.count, 1)
        XCTAssertEqual(transcript.lines.first?.text, "Thursday at 3.")
        XCTAssertEqual(transcript.lines.first?.isStreaming, false)
        XCTAssertEqual(transcript.lines.first?.messageId, "01J0000000000000000000020")
    }

    func testTheTurnOfAMessageInAChatDrawsNoLineOfItsOwn() {
        var transcript = Transcript(messages: [], isChat: true)
        transcript.apply(AgentEvent(kind: "asked", runId: "run-2", sequence: 0, text: "what time?", note: "chat"))
        XCTAssertTrue(transcript.lines.isEmpty)
    }

    func testAskingSendsTheAgentsAddressed() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"AskAgent":{"runId":"run-1","conversationId":"c"}}}"#)
        _ = try await connection.ask("@Annie hi", in: "c", drawerId: "d", addressedAgentIds: ["agent-user-a"])
        XCTAssertEqual(try sentVariables(try XCTUnwrap(fake.sent.last))["addressedAgentIds"], .array([.string("agent-user-a")]))
        _ = try await connection.ask("hi", in: "c", drawerId: "d")
        XCTAssertNil(try sentVariables(try XCTUnwrap(fake.sent.last))["addressedAgentIds"])
    }

    func testTheTwoSwitchesSendTheirOwnDocuments() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SetChatAgentReading":{"id":"c"},"SetChatAgentAskableByOthers":{"id":"c"}}}"#)
        try await connection.setChatAgentReading("c", isReading: true)
        XCTAssertEqual(
            try sentVariables(try XCTUnwrap(fake.sent.last)), ["conversationId": .string("c"), "isAgentReading": .bool(true)])
        try await connection.setChatAgentAskableByOthers("c", isAskable: false)
        XCTAssertEqual(
            try sentVariables(try XCTUnwrap(fake.sent.last)), ["conversationId": .string("c"), "isAgentAskableByOthers": .bool(false)])
    }

    // MARK: Reading a chat again

    func testReadingAChatAgainKeepsTheWordsOfAnAnswerStillBeingWritten() throws {
        var earlier = Transcript(messages: [], isChat: true)
        earlier.apply(AgentEvent(kind: "text", runId: "run-1", sequence: 0, text: "Thursday at", name: "Robin", authorAgentId: "agent-1"))
        var reread = Transcript(
            messages: try messages([message("01J0000000000000000000001", "Dinner?", authorUserId: "user-b", authorName: "Person B", isMine: false)]),
            isChat: true)
        reread.keepStreaming(from: earlier)
        XCTAssertEqual(reread.lines.map(\.text), ["Dinner?", "Thursday at"])
        XCTAssertEqual(reread.lines.last?.isStreaming, true)
        // The stored message later takes the place of the words, as it does when nothing was read again.
        let storedMessage = message(
            "01J0000000000000000000002", "Thursday at 3.", authorUserId: "user-a", authorName: "Robin", isMine: false, authorKind: "agent",
            extra: #","agentRunId":"run-1""#)
        let stored = try event(#"{"kind":"said","runId":"","sequence":0,"message":\#(storedMessage)}"#)
        reread.apply(stored)
        XCTAssertEqual(reread.lines.map(\.text), ["Dinner?", "Thursday at 3."])
        XCTAssertEqual(reread.lines.last?.isStreaming, false)
    }

    func testWordsThatAreAlreadyStoredAreNotKeptBesideTheirMessage() throws {
        var earlier = Transcript(messages: [], isChat: true)
        earlier.apply(AgentEvent(kind: "text", runId: "run-1", sequence: 0, text: "Thursday at", name: "Robin", authorAgentId: "agent-1"))
        var reread = Transcript(
            messages: try messages([
                message(
                    "01J0000000000000000000002", "Thursday at 3.", authorUserId: "user-a", authorName: "Robin", isMine: false, authorKind: "agent",
                    extra: #","agentRunId":"run-1""#)
            ]),
            isChat: true)
        reread.keepStreaming(from: earlier)
        XCTAssertEqual(reread.lines.map(\.text), ["Thursday at 3."])
    }

    func testAReactionThatNamesNobodyInAChatIsNeverTheReadersOwn() throws {
        var transcript = Transcript(
            messages: try messages([
                message(
                    "01J0000000000000000000001", "Dinner?", authorUserId: "user-b", authorName: "Person B", isMine: false,
                    extra:
                        #","reactions":[{"emoji":"👍","reactorKind":"person","reactedAt":"2030-01-01T10:01:00Z","reactorUserId":null,"reactorName":null},{"emoji":"👍","reactorKind":"person","reactedAt":"2030-01-01T10:02:00Z","reactorUserId":null,"reactorName":null}]"#
                )
            ]),
            isChat: true)
        let line = try XCTUnwrap(transcript.lines.first)
        XCTAssertEqual(line.reactions.count, 2)
        // Two accounts that are gone do not share one identity in a list.
        XCTAssertEqual(Set(line.reactions.map(\.id)).count, 2)
        XCTAssertNil(line.reaction(of: "person", userId: "user-a"))
        XCTAssertNil(line.reaction(of: "person", userId: nil))
        // The reader reacting replaces nothing of theirs and removes nobody else's.
        transcript.apply(
            try event(
                #"{"kind":"reacted","runId":"","sequence":0,"messageId":"01J0000000000000000000001","emoji":"🎉","reactorKind":"person","reactorUserId":"user-a","reactorName":"Person A"}"#
            ))
        XCTAssertEqual(transcript.lines.first?.reactions.map(\.emoji), ["👍", "👍", "🎉"])
        XCTAssertEqual(transcript.lines.first?.reaction(of: "person", userId: "user-a"), "🎉")
    }

    func testAReactionThatNamesNobodyInAConversationWithTheAgentIsTheReaders() {
        var line = TranscriptLine(id: "m", kind: .answer, text: "Hello")
        line.reactions = [AgentMessageReaction(emoji: "👍", reactorKind: "person")]
        XCTAssertEqual(line.reaction(of: "person", userId: "user-a"), "👍")
    }

    func testTheAgentSwitchesOfOneMemberMoveAndNobodyElses() {
        let chat = ChatSummary(
            chatId: "chat-1", chatKind: "group",
            members: [
                ChatMember(userId: "user-a", username: "person-a", isAgentReading: false, isAgentAskableByOthers: false),
                ChatMember(userId: "user-b", username: "person-b", isAgentReading: false, isAgentAskableByOthers: false),
            ])
        let changed = chat.withAgentSwitches(userId: "user-a", isAgentReading: true)
        XCTAssertEqual(changed.members.map(\.isAgentReading), [true, false])
        XCTAssertEqual(changed.members.map(\.isAgentAskableByOthers), [false, false])
        let askable = changed.withAgentSwitches(userId: "user-a", isAgentAskableByOthers: true)
        XCTAssertEqual(askable.members.map(\.isAgentReading), [true, false])
        XCTAssertEqual(askable.members.map(\.isAgentAskableByOthers), [true, false])
    }

    func testAPersonWithNoAgentIsToldApartFromOneWhoseAgentIsOff() async throws {
        let (none, _) = connection(answering: #"{"data":{"ReadAgent":{"agent":null}}}"#)
        let noAgent = try await none.agentIdentity()
        XCTAssertFalse(noAgent.hasAgent)
        XCTAssertFalse(noAgent.isOn)
        let (off, _) = connection(answering: #"{"data":{"ReadAgent":{"agent":{"name":"Robin","enabled":false}}}}"#)
        let agentOff = try await off.agentIdentity()
        XCTAssertTrue(agentOff.hasAgent)
        XCTAssertFalse(agentOff.isOn)
        XCTAssertEqual(agentOff.name, "Robin")
    }
}
