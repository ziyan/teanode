package com.teanode.kit.chats

import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.ZoneId
import com.teanode.kit.chats.TranscriptLine.Kind

private fun conversationOf(json: String): AgentConversation = TeaNodeJson.decodeFromString(AgentConversation.serializer(), json)

private fun lineOf(id: String, kind: Kind, text: String, reactions: List<AgentMessageReaction> = emptyList()) =
    TranscriptLine(id = id, kind = kind, text = text, reactions = reactions)

/** "Ada's agent", as the app words it in the person's language. */
private fun ownedWords(ownerName: String): String = "$ownerName's agent"

/**
 * ChatTest covers chats in the conversation screens: what the server's answers decode to, how a chat's messages become
 * lines (who said each, which begin a run, who has read as far as which), how its events change them, and what each
 * operation sends.
 */
class ChatTest {
    @Test
    fun aChatConversationDecodesWithItsPeople() = runBlocking {
        val (connection, _) = connectionAnswering("""{"data":{"ListAgentConversations":[$chatAnswer]}}""")
        val conversations = connection.conversations()
        val conversation = conversations.first()
        assertTrue(conversation.isChat)
        val chat = conversation.chat!!
        assertEquals(2, chat.unreadCount)
        assertEquals(listOf("Person A", "person-b"), chat.members.map { member -> member.displayName })
        assertEquals(2, ChatUnread.total(conversations))
        assertFalse(chat.isGroup)
    }

    @Test
    fun aConversationWithAnAgentHasNoChat() {
        val conversation = conversationOf("""{"id":"c1","kind":"main","title":"","summary":"","lastAt":null,"chat":null}""")
        assertNull(conversation.chat)
        assertFalse(conversation.isChat)
    }

    @Test
    fun theCountOnTheEntryLeavesOutChatsNotToldOfEverything() {
        val quiet = conversationOf(chatAnswer)
        assertEquals(ChatNotificationLevel.All, quiet.chat?.level)
        assertEquals(2, ChatUnread.total(listOf(quiet)))
        assertEquals(0, ChatUnread.total(listOf(quiet.copy(chat = quiet.chat!!.withLevel(ChatNotificationLevel.Mentions)))))
        assertEquals(0, ChatUnread.total(listOf(quiet.copy(chat = quiet.chat!!.withLevel(ChatNotificationLevel.Off)))))
        // A level the app does not know is all.
        assertEquals(ChatNotificationLevel.All, quiet.chat!!.copy(notificationLevel = "loud").level)
    }

    @Test
    fun aDirectChatIsNamedByTheOtherPersonAndAGroupByItsTitle() {
        val chat = conversationOf(chatAnswer).chat!!
        assertEquals("person-b", chat.name("user-a", "(deleted account)", "Chat"))
        val group = ChatSummary(chatId = "chat-2", chatKind = "group", title = "Fixture Home", members = chat.members)
        assertEquals("Fixture Home", group.name("user-a", "(deleted account)", "Chat"))
        val untitled = ChatSummary(chatId = "chat-3", chatKind = "group", members = chat.members)
        assertEquals("Person A", untitled.name("user-b", "(deleted account)", "Chat"))
    }

    @Test
    fun aPersonWhoseAccountIsGoneKeepsTheirNameWithANote() {
        val gone = ChatMember(userId = "", username = "", name = "Person C", isDeletedAccount = true)
        assertEquals("Person C (deleted account)", ChatSummary.nameOf(gone, "(deleted account)"))
        val chat = ChatSummary("chat-1", "direct", members = listOf(ChatMember(userId = "user-a", username = "person-a"), gone))
        assertEquals("Person C (deleted account)", chat.name("user-a", "(deleted account)", "Chat"))
    }

    @Test
    fun aGroupShowsOnlyThoseStillInItAndADirectChatKeepsTheOtherPerson() {
        val left = ChatMember(userId = "user-b", username = "person-b", isRemoved = true)
        val staying = ChatMember(userId = "user-c", username = "person-c")
        val me = ChatMember(userId = "user-a", username = "person-a", memberRole = "admin")
        val group = ChatSummary("g", "group", members = listOf(me, staying, left))
        assertEquals(listOf("user-c"), group.otherMembers("user-a").map { member -> member.userId })
        val direct = ChatSummary("d", "direct", members = listOf(me, left))
        assertEquals(listOf("user-b"), direct.otherMembers("user-a").map { member -> member.userId })
    }

    @Test
    fun onlyAnAdminOfAGroupThatIsNotACirclesChangesWhoIsInIt() {
        val admin = ChatMember(userId = "user-a", username = "person-a", memberRole = "admin")
        val member = ChatMember(userId = "user-b", username = "person-b")
        val group = ChatSummary("g", "group", members = listOf(admin, member))
        assertTrue(group.canChangeMembers("user-a"))
        assertFalse(group.canChangeMembers("user-b"))
        assertTrue(group.canBeLeft)
        val circles = ChatSummary("c", "group", isCircleChat = true, members = listOf(admin, member))
        assertFalse(circles.canChangeMembers("user-a"))
        assertFalse(circles.canBeLeft)
        assertFalse(circles.canBeRenamed)
        val direct = ChatSummary("d", "direct", members = listOf(admin, member))
        assertFalse(direct.canChangeMembers("user-a"))
        assertFalse(direct.canBeLeft)
    }

    @Test
    fun aChatsMessagesAreLinesWithTheirAuthors() {
        val stored = messagesOf(
            chatMessage("01J0000000000000000000001", "Dinner at seven?", "user-b", "Person B", false),
            chatMessage("01J0000000000000000000002", "Sounds good", "user-a", "Person A", true),
        )
        val transcript = Transcript(stored, isChat = true)
        assertEquals(listOf(Kind.Person, Kind.Person), transcript.lines.map { line -> line.kind })
        assertEquals(listOf("Dinner at seven?", "Sounds good"), transcript.lines.map { line -> line.text })
        assertEquals(listOf(false, true), transcript.lines.map { line -> line.author?.isOwnWords })
        assertEquals("Person B", transcript.lines.first().author?.authorName)
    }

    @Test
    fun theSameMessagesInAConversationWithAnAgentAreDrawnAsTheyAlwaysWere() {
        val stored = messagesOf(
            chatMessage("01J0000000000000000000001", "Dinner at seven?", "user-a", "Person A", true),
            chatMessage("01J0000000000000000000002", "Booked.", "", "Robin", false, authorKind = "agent"),
        )
        val transcript = Transcript(stored)
        assertEquals(listOf(Kind.Person, Kind.Answer), transcript.lines.map { line -> line.kind })
        assertNull(transcript.lines.first().author)
    }

    @Test
    fun anAgentsWordsInAChatAreAnAnswerUnderItsNameEvenWhenTheyAreTheirOwners() {
        val stored = messagesOf(chatMessage("01J0000000000000000000001", "Tell them I will be late", "user-a", "Robin", true, authorKind = "agent"))
        val line = Transcript(stored, isChat = true).lines.first()
        assertEquals(Kind.Answer, line.kind)
        assertTrue(line.author?.isAgent ?: false)
        assertFalse(line.author?.isOwnWords ?: true)
    }

    @Test
    fun aRowOfTheMembersOwnAgentsWorkInAChatIsNotAMessageOfTheChat() {
        val stored = messagesOf(
            chatMessage("01J0000000000000000000001", "Hello", "user-b", "Person B", false),
            """{"id":"01J0000000000000000000002","createdAt":"2030-01-01T10:00:01Z","role":"note","content":"looked it up","name":"depth"}""",
        )
        assertEquals(listOf(Kind.Person, Kind.Note), Transcript(stored, isChat = true).lines.map { line -> line.kind })
    }

    @Test
    fun anAuthorIsNamedOnceForARun() {
        val stored = messagesOf(
            chatMessage("01J0000000000000000000001", "One", "user-b", "Person B", false, at = "2030-01-01T10:00:00Z"),
            chatMessage("01J0000000000000000000002", "Two", "user-b", "Person B", false, at = "2030-01-01T10:00:30Z"),
            chatMessage("01J0000000000000000000003", "Three", "user-a", "Person A", true, at = "2030-01-01T10:01:00Z"),
            chatMessage("01J0000000000000000000004", "Four", "user-b", "Person B", false, at = "2030-01-01T10:01:30Z"),
            chatMessage("01J0000000000000000000005", "Five", "user-b", "Person B", false, at = "2030-01-01T11:30:00Z"),
        )
        val lines = Transcript(stored, isChat = true).lines
        val begins = lines.indices.map { index -> lines[index].beginsRun(if (index == 0) null else lines[index - 1], ZoneId.of("UTC")) }
        // The second of Person B's two in a row continues the run; the one after the reply begins another; the one an hour and a half later is named again.
        assertEquals(listOf(true, false, true, true, true), begins)
    }

    @Test
    fun theMessageUnderACallLineIsNamedAgain() {
        val stored = messagesOf(
            chatMessage("01J0000000000000000000001", "", "user-b", "Person B", false, at = "2030-01-01T10:00:00Z", extra = ""","callId":"call-one""""),
            chatMessage("01J0000000000000000000002", "After the call", "user-b", "Person B", false, at = "2030-01-01T10:04:00Z"),
        )
        val lines = Transcript(stored, isChat = true).lines
        assertTrue(lines[0].isCallLine)
        assertTrue(lines[1].beginsRun(lines[0]))
    }

    @Test
    fun theFirstMessageUnderADateDividerIsNamedAgainBySamePerson() {
        val stored = messagesOf(
            chatMessage("01J0000000000000000000001", "Late", "user-b", "Person B", false, at = "2030-01-01T23:59:00Z"),
            chatMessage("01J0000000000000000000002", "Early", "user-b", "Person B", false, at = "2030-01-02T00:01:00Z"),
            chatMessage("01J0000000000000000000003", "Soon after", "user-b", "Person B", false, at = "2030-01-02T00:02:00Z"),
        )
        val lines = Transcript(stored, isChat = true).lines
        val begins = lines.indices.map { index -> lines[index].beginsRun(if (index == 0) null else lines[index - 1], ZoneId.of("UTC")) }
        // Two minutes apart across midnight: the divider between them starts a new run. The third is the same day as the second and continues it.
        assertEquals(listOf(true, true, false), begins)
    }

    @Test
    fun eachOtherMemberIsDrawnUnderTheNewestMessageBySomebodyElseTheyHaveRead() {
        val stored = messagesOf(
            chatMessage("01J0000000000000000000001", "One", "user-b", "Person B", false),
            chatMessage("01J0000000000000000000002", "Two", "user-b", "Person B", false),
            chatMessage("01J0000000000000000000003", "Three", "user-a", "Person A", true),
        )
        val transcript = Transcript(stored, isChat = true)
        val members = listOf(
            ChatMember(userId = "user-a", username = "person-a", readThroughMessageId = "01J0000000000000000000003"),
            ChatMember(userId = "user-b", username = "person-b", readThroughMessageId = "01J0000000000000000000003"),
            ChatMember(userId = "user-c", username = "person-c", readThroughMessageId = "01J0000000000000000000002"),
            ChatMember(userId = "user-d", username = "person-d", readThroughMessageId = ""),
            ChatMember(userId = "user-e", username = "person-e", isRemoved = true, readThroughMessageId = "01J0000000000000000000003"),
        )
        val readers = transcript.readers(members, "user-a")
        assertEquals(listOf("user-b"), readers["01J0000000000000000000003"]?.map { member -> member.userId })
        assertEquals(listOf("user-c"), readers["01J0000000000000000000002"]?.map { member -> member.userId })
        assertNull(readers["01J0000000000000000000001"])
        assertEquals("01J0000000000000000000003", transcript.newestChatMessageId)
    }

    @Test
    fun aMemberWhoHasReadOnlyTheirOwnMessagesIsUnderNone() {
        val stored = messagesOf(
            chatMessage("01J0000000000000000000001", "One", "user-b", "Person B", false),
            chatMessage("01J0000000000000000000002", "Two", "user-b", "Person B", false),
        )
        val members = listOf(ChatMember(userId = "user-b", username = "person-b", readThroughMessageId = "01J0000000000000000000002"))
        assertTrue(Transcript(stored, isChat = true).readers(members, "user-a").isEmpty())
    }

    @Test
    fun aSaidEventDecodesAndAddsTheMessageOnce() {
        val transcript = Transcript(isChat = true)
        val said = eventOf("""{"kind":"said","runId":"","sequence":0,"message":${chatMessage("01J0000000000000000000004", "See you there", "user-b", "Person B", false)}}""")
        assertEquals("Person B", said.message?.authorName)
        transcript.apply(said)
        transcript.apply(said)
        assertEquals(listOf("See you there"), transcript.lines.map { line -> line.text })
    }

    @Test
    fun theChatsCopyOfAMessageJustSentReplacesTheLineDrawnWhileItWasOnItsWay() {
        val transcript = Transcript(isChat = true)
        val me = ChatAuthor("person", authorUserId = "user-a", authorName = "Person A", isMine = true)
        val sentId = transcript.addSent("On my way", author = me)
        assertEquals(listOf(sentId), transcript.lines.map { line -> line.id })
        transcript.apply(eventOf("""{"kind":"said","runId":"","sequence":0,"message":${chatMessage("01J0000000000000000000005", "On my way", "user-a", "Person A", true)}}"""))
        assertEquals(listOf("01J0000000000000000000005"), transcript.lines.map { line -> line.id })
        assertEquals("01J0000000000000000000005", transcript.lines.first().messageId)
    }

    @Test
    fun aMessageEditedShowsItsNewWordsAndTheMark() {
        val transcript = Transcript(messagesOf(chatMessage("01J0000000000000000000001", "Dinner at seven?", "user-b", "Person B", false)), isChat = true)
        val edited = chatMessage("01J0000000000000000000001", "Dinner at eight?", "user-b", "Person B", false).replace(""""editedAt":null""", """"editedAt":"2030-01-01T10:05:00Z"""")
        transcript.apply(eventOf("""{"kind":"edited","runId":"","sequence":0,"message":$edited}"""))
        assertEquals("Dinner at eight?", transcript.lines.first().text)
        assertEquals("2030-01-01T10:05:00Z", transcript.lines.first().editedAt)
    }

    @Test
    fun aMessageTakenBackIsAMarkThatItWas() {
        val reaction = ""","reactions":[{"emoji":"👍","reactorKind":"person","reactedAt":null,"reactorUserId":"user-a","reactorName":"Person A"}]"""
        val transcript = Transcript(messagesOf(chatMessage("01J0000000000000000000001", "Dinner at seven?", "user-b", "Person B", false, extra = reaction)), isChat = true)
        assertEquals(1, transcript.lines.first().reactions.size)
        transcript.apply(eventOf("""{"kind":"deleted","runId":"","sequence":0,"messageId":"01J0000000000000000000001"}"""))
        assertEquals(1, transcript.lines.size)
        assertEquals("", transcript.lines.first().text)
        assertTrue(transcript.lines.first().isDeleted)
        assertTrue(transcript.lines.first().reactions.isEmpty())
    }

    @Test
    fun wordsStreamedByATurnThatStoppedAreTakenOffTheScreen() {
        val transcript = Transcript(isChat = true)
        transcript.apply(eventOf("""{"kind":"text","runId":"run-1","sequence":0,"text":"Thursday at","name":"Leaf","authorOwnerName":"Person A","authorAgentId":"agent-a"}"""))
        assertEquals(1, transcript.lines.size)
        transcript.apply(eventOf("""{"kind":"deleted","runId":"","sequence":0,"messageId":"streaming-run-1"}"""))
        assertTrue(transcript.lines.isEmpty())
    }

    @Test
    fun eachPersonsReactionInAChatStaysAndReplacesOnlyTheirOwn() {
        val transcript = Transcript(messagesOf(chatMessage("01J0000000000000000000001", "Dinner?", "user-b", "Person B", false)), isChat = true)
        fun reacted(userId: String, name: String, emoji: String) = eventOf(
            """{"kind":"reacted","runId":"","sequence":0,"messageId":"01J0000000000000000000001","emoji":"$emoji","reactorKind":"person","reactorUserId":"$userId","reactorName":"$name"}""",
        )
        transcript.apply(reacted("user-a", "Person A", "👍"))
        transcript.apply(reacted("user-b", "Person B", "❤️"))
        assertEquals(listOf("👍", "❤️"), transcript.lines.first().reactions.map { reaction -> reaction.emoji })
        transcript.apply(reacted("user-a", "Person A", "🎉"))
        assertEquals(listOf("❤️", "🎉").sorted(), transcript.lines.first().reactions.map { reaction -> reaction.emoji }.sorted())
        assertEquals("🎉", transcript.lines.first().reaction("person", "user-a"))
        assertEquals("❤️", transcript.lines.first().reaction("person", "user-b"))
        transcript.apply(reacted("user-a", "Person A", ""))
        assertEquals(listOf("❤️"), transcript.lines.first().reactions.map { reaction -> reaction.emoji })
        assertNull(transcript.lines.first().reaction("person", "user-a"))
    }

    @Test
    fun aReadEventMovesAMembersMarkForwardAndNeverBack() {
        var chat = conversationOf(chatAnswer).chat!!
        chat = chat.withRead("user-b", "01J0000000000000000000009")
        assertEquals("01J0000000000000000000009", chat.member("user-b")?.readThroughMessageId)
        chat = chat.withRead("user-b", "01J0000000000000000000003")
        assertEquals("01J0000000000000000000009", chat.member("user-b")?.readThroughMessageId)
    }

    @Test
    fun typingShowsForAFewSecondsAndEndsWithTheMessage() {
        val typing = ChatTyping()
        val nowMS = 1_000_000_000L
        typing.note("user-b", "Person B", nowMS)
        assertEquals(listOf("Person B"), typing.names(nowMS + 2_000))
        assertTrue(typing.names(nowMS + 6_000).isEmpty())
        typing.prune(nowMS + 6_000)
        assertTrue(typing.isEmpty(nowMS))
        typing.note("user-b", "Person B", nowMS)
        typing.clear("user-b")
        assertTrue(typing.isEmpty(nowMS))
    }

    @Test
    fun anAgentThinkingIsToldApartFromAPersonTyping() {
        val typing = ChatTyping()
        val nowMS = 1_000_000_000L
        typing.noteAgent("agent-1", "Robin", nowMS = nowMS)
        typing.note("user-b", "Person B", nowMS)
        assertEquals(listOf("Robin"), typing.agentSpeakers(nowMS + 2_000, ::ownedWords))
        assertEquals(listOf("Person B"), typing.names(nowMS + 2_000))
        assertFalse(typing.isEmpty(nowMS))
        typing.clear("user-b")
        assertEquals(listOf("Robin"), typing.agentSpeakers(nowMS, ::ownedWords))
        typing.clearAgent("agent-1")
        assertTrue(typing.isEmpty(nowMS))
        typing.noteAgent("agent-1", "Robin", nowMS = nowMS)
        typing.prune(nowMS + 6_000)
        assertTrue(typing.agentSpeakers(nowMS, ::ownedWords).isEmpty())
    }

    @Test
    fun anAgentIsNamedByWhoseItIsWhenTwoAreBothCalledAgent() {
        assertEquals("Bertie · Ada's agent", AgentSpeaker.name("Bertie", "Ada", ::ownedWords))
        // The default name would only repeat the word.
        assertEquals("Ada's agent", AgentSpeaker.name("Agent", "Ada", ::ownedWords))
        assertEquals("Ben's agent", AgentSpeaker.name("Agent", "Ben", ::ownedWords))
        assertEquals("Ben's agent", AgentSpeaker.name("", "Ben", ::ownedWords))
        // An agent whose owner is not known is called by its own name.
        assertEquals("Bertie", AgentSpeaker.name("Bertie", " ", ::ownedWords))
    }

    @Test
    fun theThinkingLineNamesEachAgentByWhoseItIs() {
        val typing = ChatTyping()
        val nowMS = 1_000_000_000L
        typing.noteAgent("agent-1", "Agent", "Ada", nowMS)
        typing.noteAgent("agent-2", "Bertie", "Ben", nowMS + 1_000)
        assertEquals(listOf("Ada's agent", "Bertie · Ben's agent"), typing.agentSpeakers(nowMS + 2_000, ::ownedWords))
        assertFalse(typing.isEmpty(nowMS))
        typing.clearAgent("agent-1")
        assertEquals(listOf("Bertie · Ben's agent"), typing.agentSpeakers(nowMS + 2_000, ::ownedWords))
    }

    // A chat of person-a, whose agent is on, with person-b, who let the others ask theirs, person-c, who did not, and person-d, who did but whose agent is off.
    private fun roomOfAgents(): ChatSummary {
        fun member(userId: String, agentName: String, isAskable: Boolean, isOn: Boolean = true) = ChatMember(
            userId = userId, username = userId, name = userId.uppercase(), isAgentAskableByOthers = isAskable, agentId = "agent-$userId",
            agentName = agentName, isAgentActive = isOn,
        )
        return ChatSummary(
            "chat-1", "group", title = "Trip",
            members = listOf(
                member("user-a", "Annie", false), member("user-b", "Benny", true), member("user-c", "Cyrus", false), member("user-d", "Dee", true, isOn = false),
                ChatMember(userId = "user-e", username = "user-e", isRemoved = true, isAgentAskableByOthers = true, agentId = "agent-user-e", agentName = "Evie", isAgentActive = true),
            ),
        )
    }

    @Test
    fun theAtOffersTheOwnAgentFirstAndThenThoseTheirOwnersLetTheOthersAsk() {
        val agents = roomOfAgents().addressableAgents("user-a", "(deleted)")
        assertEquals(listOf("Annie", "Benny"), agents.map { agent -> agent.name })
        assertEquals(listOf(true, false), agents.map { agent -> agent.isOwn })
        assertEquals(listOf("Cyrus", "Benny"), roomOfAgents().addressableAgents("user-c", "").map { agent -> agent.name })
    }

    @Test
    fun aMemberWhoseAgentIsNotToldIsNotOffered() {
        val members = TeaNodeJson.decodeFromString(
            kotlinx.serialization.builtins.ListSerializer(ChatMember.serializer()),
            """[{"userId":"user-b","username":"b","name":"B","avatarUrl":null,"avatarColorIndex":1,"memberRole":"member","isDeletedAccount":false,"isRemoved":false,"readThroughMessageId":"","isAgentReading":false,"isAgentAskableByOthers":false,"agentId":"","agentName":"","isAgentActive":false}]""",
        )
        assertTrue(ChatSummary("c", "direct", members = members).addressableAgents("user-a", "").isEmpty())
    }

    @Test
    fun theMentionBeingTypedIsTheAtAtTheEndThatStartsAWord() {
        assertEquals("", ChatMention.query("@"))
        assertEquals("Be", ChatMention.query("hello @Be"))
        assertNull(ChatMention.query("mail me@example.com"))
        assertNull(ChatMention.query("hello @Benny and"))
        assertNull(ChatMention.query("no at sign"))
    }

    @Test
    fun theAtOffersTheOwnAgentEverybodyThenEachPersonAndTheirAgentNamedApart() {
        val choices = roomOfAgents().mentionChoices("user-a", "")
        assertEquals(listOf("agent:agent-user-a", "all", "person:user-b", "agent:agent-user-b", "person:user-c", "person:user-d"), choices.map { choice -> choice.identity })
        val direct = ChatSummary("c", "direct", members = roomOfAgents().members)
        assertFalse(direct.mentionChoices("user-a", "").contains(MentionChoice.Everybody))
    }

    @Test
    fun theLettersNarrowTheChoicesAndChoosingPutsTheWordInTheWords() {
        val choices = roomOfAgents().mentionChoices("user-a", "")
        assertEquals(choices.size, ChatMention.matching(choices, "", "all").size)
        assertEquals(listOf("agent:agent-user-b"), ChatMention.matching(choices, "be", "all").map { choice -> choice.identity })
        assertEquals(listOf("person:user-c"), ChatMention.matching(choices, "user-c", "all").map { choice -> choice.identity })
        assertEquals(listOf("all"), ChatMention.matching(choices, "al", "所有人").map { choice -> choice.identity })
        assertEquals(listOf("all"), ChatMention.matching(choices, "所", "所有人").map { choice -> choice.identity })
        assertTrue(ChatMention.matching(choices, "x", "all").isEmpty())
        assertEquals("so @Benny ", ChatMention.choosing(choices[3], "so @Be", "all"))
        assertEquals("@全員 ", ChatMention.choosing(MentionChoice.Everybody, "@", "全員"))
        assertEquals("hi @USER-B ", ChatMention.choosing(choices[2], "hi @us", "all"))
    }

    @Test
    fun aMentionOfAPersonIsReadFromTheWordsAndAskingTheirAgentIsNone() {
        val room = roomOfAgents()
        val mentioned = ChatMention.mentioned("@USER-B and @all, dinner?", room, "user-a", "", emptyList())
        assertEquals(listOf("user-b"), mentioned.userIds)
        assertTrue(mentioned.isAllMentioned)
        assertTrue(ChatMention.mentioned("大家@所有人", room, "user-a", "", emptyList()).isAllMentioned)
        // Asking Benny, user-b's agent, is no mention of user-b, and starts Benny.
        val agents = room.addressableAgents("user-a", "")
        val asked = ChatMention.mentioned("@Benny when?", room, "user-a", "", agents)
        assertTrue(asked.userIds.isEmpty())
        assertFalse(asked.isAllMentioned)
        assertEquals(listOf("agent-user-b"), ChatMention.addressed("@Benny when?", agents))
        // Mentioning user-b asks no agent.
        assertTrue(ChatMention.addressed("@USER-B when?", agents).isEmpty())
        // An agent chosen under a person's name is the agent.
        val namesake = AddressableAgent("agent-x", "USER-C", false, "Someone")
        assertTrue(ChatMention.mentioned("@USER-C when?", room, "user-a", "", listOf(namesake)).userIds.isEmpty())
        // An address and a longer word mention nobody; the person is not offered themselves.
        assertTrue(ChatMention.mentioned("x@USER-B.example @USER-Bx @USER-A", room, "user-a", "", emptyList()).userIds.isEmpty())
    }

    @Test
    fun theMentionsInAMessageAreFoundForMarking() {
        val text = "mail ada@example.com, @Ada Lin and @所有人!"
        val ranges = ChatMention.ranges(listOf("Ada", "Ada Lin", "所有人"), text)
        assertEquals(listOf("@Ada Lin", "@所有人"), ranges.map { range -> text.substring(range.first, range.last + 1) })
    }

    @Test
    fun theMentionsOfALineAreTheNamesOfThoseItMentionsAndTheWordsForEverybody() {
        val line = lineOf("m1", Kind.Person, "@USER-B @all").copy(mentionedUserIds = listOf("user-b", "user-gone"), isAllMentioned = true)
        val words = line.mentionWords(roomOfAgents(), "user-b", "")
        assertEquals(ChatMentionWord("USER-B", true), words.first())
        assertEquals(ChatMention.allWords.toSet(), words.drop(1).map { word -> word.word }.toSet())
    }

    @Test
    fun aMessageOfAChatCarriesWhomItMentions() {
        val message = AgentMessage("m1", "2030-01-01T10:00:00Z", "user", "@USER-B hi", authorKind = "person", authorName = "USER-A", mentionedUserIds = listOf("user-b"), isAllMentioned = true)
        val line = Transcript.chatLine(message)!!
        assertEquals(listOf("user-b"), line.mentionedUserIds)
        assertTrue(line.isAllMentioned)
    }

    @Test
    fun askingInAChatSendsWhomItMentions() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"AskAgent":{"runId":"","conversationId":"c"}}}""")
        connection.ask("@USER-B hi", "c", "d", mentionedUserIds = listOf("user-b"), isAllMentioned = true)
        val sent = sentVariables(fake.sent.last())
        assertEquals(JsonArray(listOf(JsonPrimitive("user-b"))), sent["mentionedUserIds"])
        assertEquals(JsonPrimitive(true), sent["isAllMentioned"])
    }

    @Test
    fun aMessageIsForTheAgentsChosenWhoseNamesAreStillInItAndTheOneRepliedTo() {
        val agents = roomOfAgents().addressableAgents("user-a", "")
        assertEquals(listOf("agent-user-a", "agent-user-b"), ChatMention.addressed("@Annie and @Benny, hello", agents))
        assertEquals(listOf("agent-user-b"), ChatMention.addressed("@Benny, hello", agents))
        assertTrue(ChatMention.addressed("@Anniefied", agents).isEmpty())
        assertTrue(ChatMention.addressed("@Annie", emptyList()).isEmpty())
        assertEquals(listOf("agent-user-b"), ChatMention.addressed("thanks", emptyList(), "agent-user-b"))
        assertEquals(listOf("agent-user-b"), ChatMention.addressed("@Benny thanks", agents, "agent-user-b"))
    }

    @Test
    fun anAgentsMessageAndItsWordsBeingWrittenCarryWhoseAgentItIs() {
        val stored = messageOf(
            """{"id":"01J0000000000000000000030","createdAt":"2030-01-01T10:00:00Z","role":"assistant","content":"Free on Friday.",
             "authorKind":"agent","authorUserId":"user-b","authorName":"Agent","authorOwnerName":"Person B","authorAgentId":"agent-b",
             "agentRunId":"run-1","isMine":false,"isAgentWork":false}""",
        )
        val transcript = Transcript(isChat = true)
        transcript.apply(AgentEvent(kind = "text", runId = "run-2", sequence = 0, text = "Busy", name = "Bertie", authorAgentId = "agent-c", authorOwnerName = "Person C"))
        transcript.apply(AgentEvent(kind = "said", runId = "", sequence = 0, message = stored))
        val names = transcript.lines.mapNotNull { line -> line.author?.speakerName(::ownedWords) }
        assertEquals(listOf("Bertie · Person C's agent", "Person B's agent"), names)
        // A person is named as they are, and a message from before the owner was recorded still has a name.
        assertEquals("Ada", ChatAuthor("person", authorUserId = "u", authorName = "Ada").speakerName(::ownedWords))
        assertEquals("Agent", ChatAuthor("agent", authorName = "Agent").speakerName(::ownedWords))
    }

    @Test
    fun whatTheAgentDidToAnswerIsNotSaidInTheChat() {
        val stored = messagesOf(
            """{"id":"01J0000000000000000000010","createdAt":"2030-01-01T10:00:00Z","role":"assistant","content":"Let me look.",
              "authorKind":"agent","authorUserId":"user-a","authorName":"Robin","authorAgentId":"agent-1","agentRunId":"run-1",
              "isMine":true,"isAgentWork":false}""",
            """{"id":"01J0000000000000000000011","createdAt":"2030-01-01T10:00:01Z","role":"assistant","content":"",
              "toolCalls":[{"id":"call-1","name":"calendar","arguments":"{}"}],
              "authorKind":"agent","authorName":"Robin","authorAgentId":"agent-1","isMine":true,"isAgentWork":true}""",
            """{"id":"01J0000000000000000000012","createdAt":"2030-01-01T10:00:02Z","role":"tool","content":"{}","name":"calendar",
              "toolCallId":"call-1","authorKind":"agent","authorName":"Robin","isMine":true,"isAgentWork":true}""",
        )
        val transcript = Transcript(stored, isChat = true)
        assertEquals(listOf(Kind.Answer, Kind.Tool), transcript.lines.map { line -> line.kind })
        assertEquals(true, transcript.lines.first().author?.isAgent)
        assertEquals("run-1", transcript.lines.first().runId)
        assertEquals("calendar", transcript.lines.last().detail)
    }

    @Test
    fun anAgentsAnswerIsDrawnAsItIsWrittenAndReplacedByTheStoredMessage() {
        val transcript = Transcript(isChat = true)
        fun words(text: String) = AgentEvent(kind = "text", runId = "run-1", sequence = 0, text = text, name = "Robin", authorAgentId = "agent-1")
        transcript.apply(words("Thurs"))
        transcript.apply(words("day at 3"))
        assertEquals(1, transcript.lines.size)
        assertEquals("Thursday at 3", transcript.lines.first().text)
        assertEquals(true, transcript.lines.first().isStreaming)
        assertEquals("Robin", transcript.lines.first().author?.authorName)
        // The turn's own end-of-answer event adds nothing in a chat; the chat says it.
        transcript.apply(AgentEvent(kind = "message", runId = "run-1", sequence = 3, text = "Thursday at 3."))
        assertEquals(1, transcript.lines.size)
        // And the stored message takes the place of the words, where they stood.
        val stored = messageOf(
            """{"id":"01J0000000000000000000020","createdAt":"2030-01-01T10:00:00Z","role":"assistant","content":"Thursday at 3.",
             "authorKind":"agent","authorUserId":"user-a","authorName":"Robin","authorAgentId":"agent-1","agentRunId":"run-1",
             "isMine":true,"isAgentWork":false}""",
        )
        transcript.apply(AgentEvent(kind = "said", runId = "", sequence = 0, message = stored))
        assertEquals(1, transcript.lines.size)
        assertEquals("Thursday at 3.", transcript.lines.first().text)
        assertEquals(false, transcript.lines.first().isStreaming)
        assertEquals("01J0000000000000000000020", transcript.lines.first().messageId)
    }

    @Test
    fun theTurnOfAMessageInAChatDrawsNoLineOfItsOwn() {
        val transcript = Transcript(isChat = true)
        transcript.apply(AgentEvent(kind = "asked", runId = "run-2", sequence = 0, text = "what time?", note = "chat"))
        assertTrue(transcript.lines.isEmpty())
    }

    @Test
    fun readingAChatAgainKeepsTheWordsOfAnAnswerStillBeingWritten() {
        val earlier = Transcript(isChat = true)
        earlier.apply(AgentEvent(kind = "text", runId = "run-1", sequence = 0, text = "Thursday at", name = "Robin", authorAgentId = "agent-1"))
        val reread = Transcript(messagesOf(chatMessage("01J0000000000000000000001", "Dinner?", "user-b", "Person B", false)), isChat = true)
        reread.keepStreaming(earlier)
        assertEquals(listOf("Dinner?", "Thursday at"), reread.lines.map { line -> line.text })
        assertEquals(true, reread.lines.last().isStreaming)
        // The stored message later takes the place of the words, as it does when nothing was read again.
        val storedMessage = chatMessage("01J0000000000000000000002", "Thursday at 3.", "user-a", "Robin", false, authorKind = "agent", extra = ""","agentRunId":"run-1"""")
        reread.apply(eventOf("""{"kind":"said","runId":"","sequence":0,"message":$storedMessage}"""))
        assertEquals(listOf("Dinner?", "Thursday at 3."), reread.lines.map { line -> line.text })
        assertEquals(false, reread.lines.last().isStreaming)
    }

    @Test
    fun wordsThatAreAlreadyStoredAreNotKeptBesideTheirMessage() {
        val earlier = Transcript(isChat = true)
        earlier.apply(AgentEvent(kind = "text", runId = "run-1", sequence = 0, text = "Thursday at", name = "Robin", authorAgentId = "agent-1"))
        val reread = Transcript(
            messagesOf(chatMessage("01J0000000000000000000002", "Thursday at 3.", "user-a", "Robin", false, authorKind = "agent", extra = ""","agentRunId":"run-1"""")),
            isChat = true,
        )
        reread.keepStreaming(earlier)
        assertEquals(listOf("Thursday at 3."), reread.lines.map { line -> line.text })
    }

    @Test
    fun aReactionThatNamesNobodyInAChatIsNeverTheReadersOwn() {
        val reactions = ""","reactions":[{"emoji":"👍","reactorKind":"person","reactedAt":"2030-01-01T10:01:00Z","reactorUserId":null,"reactorName":null},{"emoji":"👍","reactorKind":"person","reactedAt":"2030-01-01T10:02:00Z","reactorUserId":null,"reactorName":null}]"""
        val transcript = Transcript(messagesOf(chatMessage("01J0000000000000000000001", "Dinner?", "user-b", "Person B", false, extra = reactions)), isChat = true)
        val line = transcript.lines.first()
        assertEquals(2, line.reactions.size)
        // Two accounts that are gone do not share one identity in a list.
        assertEquals(2, line.reactions.map { reaction -> reaction.identity }.toSet().size)
        assertNull(line.reaction("person", "user-a"))
        assertNull(line.reaction("person", null))
        // The reader reacting replaces nothing of theirs and removes nobody else's.
        transcript.apply(eventOf("""{"kind":"reacted","runId":"","sequence":0,"messageId":"01J0000000000000000000001","emoji":"🎉","reactorKind":"person","reactorUserId":"user-a","reactorName":"Person A"}"""))
        assertEquals(listOf("👍", "👍", "🎉"), transcript.lines.first().reactions.map { reaction -> reaction.emoji })
        assertEquals("🎉", transcript.lines.first().reaction("person", "user-a"))
    }

    @Test
    fun aReactionThatNamesNobodyInAConversationWithTheAgentIsTheReaders() {
        val line = lineOf("m", Kind.Answer, "Hello", listOf(AgentMessageReaction("👍", "person")))
        assertEquals("👍", line.reaction("person", "user-a"))
    }

    @Test
    fun theAgentSwitchesOfOneMemberMoveAndNobodyElses() {
        val chat = ChatSummary(
            "chat-1", "group",
            members = listOf(
                ChatMember(userId = "user-a", username = "person-a", isAgentReading = false, isAgentAskableByOthers = false),
                ChatMember(userId = "user-b", username = "person-b", isAgentReading = false, isAgentAskableByOthers = false),
            ),
        )
        val changed = chat.withAgentSwitches("user-a", isAgentReading = true)
        assertEquals(listOf(true, false), changed.members.map { member -> member.isAgentReading })
        assertEquals(listOf(false, false), changed.members.map { member -> member.isAgentAskableByOthers })
        val askable = changed.withAgentSwitches("user-a", isAgentAskableByOthers = true)
        assertEquals(listOf(true, false), askable.members.map { member -> member.isAgentReading })
        assertEquals(listOf(true, false), askable.members.map { member -> member.isAgentAskableByOthers })
    }

    @Test
    fun aPersonWithNoAgentIsToldApartFromOneWhoseAgentIsOff() = runBlocking {
        val noAgent = connectionAnswering("""{"data":{"ReadAgent":{"agent":null}}}""").first.agentIdentity()
        assertFalse(noAgent.hasAgent)
        assertFalse(noAgent.isOn)
        val agentOff = connectionAnswering("""{"data":{"ReadAgent":{"agent":{"name":"Robin","enabled":false}}}}""").first.agentIdentity()
        assertTrue(agentOff.hasAgent)
        assertFalse(agentOff.isOn)
        assertEquals("Robin", agentOff.agentName)
    }

    @Test
    fun openingADirectChatAsksForThePersonAndAnswersTheConversation() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"OpenDirectChat":{"id":"conversation-9"}}}""")
        assertEquals("conversation-9", connection.openDirectChat("user-b"))
        assertEquals(mapOf("userId" to JsonPrimitive("user-b")), sentVariables(fake.sent.last()))
    }

    @Test
    fun makingAGroupSendsItsTrimmedNameAndPeople() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"CreateGroupChat":{"id":"conversation-10"}}}""")
        var draft = GroupChatDraft()
        assertFalse(draft.canSubmit)
        draft = draft.copy(title = "  Weekend plans ")
        assertFalse(draft.canSubmit)
        draft = draft.copy(userIds = setOf("user-c", "user-b"))
        assertTrue(draft.canSubmit)
        assertEquals("conversation-10", connection.createGroupChat(draft))
        assertEquals(
            mapOf("title" to JsonPrimitive("Weekend plans"), "userIds" to JsonArray(listOf(JsonPrimitive("user-b"), JsonPrimitive("user-c")))),
            sentVariables(fake.sent.last()),
        )
    }

    @Test
    fun changingAChatSendsWhatTheServerAsksFor() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"RenameChat":{"id":"c"},"MarkChatRead":true,"SetChatNotificationLevel":{"id":"c"},"SetChatTyping":true}}""")
        connection.renameChat("c", " Dinner ")
        assertEquals(mapOf("conversationId" to JsonPrimitive("c"), "title" to JsonPrimitive("Dinner")), sentVariables(fake.sent.last()))
        connection.markChatRead("c", "01J0000000000000000000003")
        assertEquals(mapOf("conversationId" to JsonPrimitive("c"), "throughMessageId" to JsonPrimitive("01J0000000000000000000003")), sentVariables(fake.sent.last()))
        connection.setChatNotificationLevel("c", ChatNotificationLevel.Mentions)
        assertEquals(mapOf("conversationId" to JsonPrimitive("c"), "notificationLevel" to JsonPrimitive("mentions")), sentVariables(fake.sent.last()))
        connection.setChatTyping("c")
        assertEquals(mapOf("conversationId" to JsonPrimitive("c")), sentVariables(fake.sent.last()))
    }

    @Test
    fun askingSendsTheAgentsAddressed() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"AskAgent":{"runId":"run-1","conversationId":"c"}}}""")
        connection.ask("@Annie hi", "c", "d", addressedAgentIds = listOf("agent-user-a"))
        assertEquals(JsonArray(listOf(JsonPrimitive("agent-user-a"))), sentVariables(fake.sent.last())["addressedAgentIds"])
        connection.ask("hi", "c", "d")
        assertNull(sentVariables(fake.sent.last())["addressedAgentIds"])
    }

    @Test
    fun theTwoSwitchesSendTheirOwnDocuments() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SetChatAgentReading":{"id":"c"},"SetChatAgentAskableByOthers":{"id":"c"}}}""")
        connection.setChatAgentReading("c", true)
        assertEquals(mapOf("conversationId" to JsonPrimitive("c"), "isAgentReading" to JsonPrimitive(true)), sentVariables(fake.sent.last()))
        connection.setChatAgentAskableByOthers("c", false)
        assertEquals(mapOf("conversationId" to JsonPrimitive("c"), "isAgentAskableByOthers" to JsonPrimitive(false)), sentVariables(fake.sent.last()))
    }

    // Voice messages: what one adds to the app's reading of a chat.
    private val voiceMessageJson = """
        {"id":"01J0000000000000000000009","createdAt":"2030-01-01T10:00:00Z","role":"assistant","content":"","authorKind":"person",
         "authorUserId":"user-b","authorName":"Person B","authorAvatarUrl":null,"authorAvatarColorIndex":4,"authorAgentId":"",
         "isMine":false,"editedAt":null,"isDeleted":false,
         "attachments":[{"id":"file-1","name":"Voice message.m4a","contentType":"audio/mp4","size":9000,"isRemoved":false,
           "audioDurationMS":7000,"audioWaveform":"AAECAwQF"}],
         "isVoiceMessage":true,"transcriptText":"","transcriptState":"waiting"}
    """.trimIndent()

    @Test
    fun aVoiceMessageDecodesWithItsRecordingFacts() {
        val message = messageOf(voiceMessageJson)
        assertEquals(true, message.isVoiceMessage)
        assertEquals("waiting", message.transcriptState)
        val recording = message.attachments!!.first()
        assertEquals(7000L, recording.audioDurationMS)
        assertEquals("AAECAwQF", recording.audioWaveform)
    }

    @Test
    fun anOrdinaryMessageAndFileDecodeWithNoVoiceFields() {
        val message = messageOf("""{"id":"m","createdAt":"2030-01-01T10:00:00Z","role":"user","content":"hi","attachments":[{"id":"f","name":"a.pdf","contentType":"application/pdf","size":3}]}""")
        assertNull(message.isVoiceMessage)
        assertNull(message.transcriptText)
        assertNull(message.attachments?.first()?.audioDurationMS)
        assertNull(message.attachments?.first()?.audioWaveform)
    }

    @Test
    fun aVoiceMessageIsALineWithItsRecordingAndLaterItsWords() {
        val message = messageOf(voiceMessageJson)
        val transcript = Transcript(listOf(message), isChat = true)
        var line = transcript.lines.first()
        assertTrue(line.isVoiceMessage)
        assertEquals("file-1", line.voiceAttachment?.id)
        assertEquals("waiting", line.transcriptState)
        assertEquals("", line.readableText)
        // The words arrive as an edit of the message.
        transcript.applyEdited(message.copy(transcriptText = "Running late, save me a seat", transcriptState = "done"))
        line = transcript.lines.first()
        assertEquals("Running late, save me a seat", line.transcriptText)
        assertEquals("done", line.transcriptState)
        assertEquals("Running late, save me a seat", line.readableText)
        // Taken back, it is a mark that it was said, with no recording.
        transcript.applyDeleted(message.id)
        line = transcript.lines.first()
        assertTrue(line.isDeleted)
        assertNull(line.voiceAttachment)
        assertEquals("", line.transcriptText)
    }

    @Test
    fun aVoiceMessageJustSentIsDrawnAsOneUntilTheChatHasIt() {
        val transcript = Transcript(isChat = true)
        val me = ChatAuthor("person", authorUserId = "user-a", authorName = "Person A", isMine = true)
        val recording = AgentAttachment("file-2", "Voice message.m4a", "audio/mp4", 100, audioDurationMS = 4000)
        val sentId = transcript.addSent("", attachments = listOf(recording), author = me, isVoiceMessage = true)
        assertEquals("file-2", transcript.lines.first().voiceAttachment?.id)
        val said = messageOf(voiceMessageJson).copy(authorUserId = "user-a", isMine = true)
        transcript.applySaid(said)
        assertEquals(listOf(said.id), transcript.lines.map { line -> line.id })
        assertFalse(transcript.lines.map { line -> line.id }.contains(sentId))
    }

    @Test
    fun theDocumentsThatReadMessagesAskForTheVoiceFields() {
        for (document in listOf(ChatDocuments.CONVERSATION, ChatDocuments.CONVERSATION_EVENTS)) {
            assertTrue(document.contains("isVoiceMessage transcriptText transcriptState"))
            assertTrue(document.contains("audioDurationMS audioWaveform"))
        }
        assertTrue(ChatDocuments.ASK.contains("\$voiceMessageAttachmentId: String"))
        assertTrue(ChatDocuments.ASK.contains("voiceMessageAttachmentId: \$voiceMessageAttachmentId"))
        assertTrue(ChatDocuments.VOICE.contains("isVoiceMessageAvailable"))
    }

    @Test
    fun askingWithAVoiceMessageSendsItsAttachment() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"AskAgent":{"runId":"","conversationId":"c"}}}""")
        connection.ask("", "c", "d", voiceMessageAttachmentId = "attachment-1")
        var sent = sentVariables(fake.sent.last())
        assertEquals(JsonPrimitive("attachment-1"), sent["voiceMessageAttachmentId"])
        // The recording is the message, not one of its files.
        assertEquals(JsonArray(emptyList()), sent["attachmentIds"])
        connection.ask("hi", "c", "d")
        sent = sentVariables(fake.sent.last())
        assertNull(sent["voiceMessageAttachmentId"])
    }

    @Test
    fun aVoiceMessageIsTranscribedByName() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"TranscribeChatVoiceMessage":{"id":"m-1","transcriptState":"done","transcriptText":"See you at six"}}}""")
        connection.transcribeChatVoiceMessage("m-1")
        assertEquals(JsonPrimitive("m-1"), sentVariables(fake.sent.last())["messageId"])
        assertTrue(ChatDocuments.TRANSCRIBE_CHAT_VOICE_MESSAGE.contains("TranscribeChatVoiceMessage(messageId: \$messageId)"))
    }

    @Test
    fun theVoiceAvailabilityTellsIfVoiceMessagesAreOffered() = runBlocking {
        val voice = connectionAnswering("""{"data":{"ReadAgentVoice":{"isVoiceAvailable":true,"sampleRate":24000,"isVoiceMessageAvailable":true}}}""").first.voiceAvailability()
        assertTrue(voice.isVoiceAvailable)
        assertEquals(true, voice.isVoiceMessageAvailable)
        val before = connectionAnswering("""{"data":{"ReadAgentVoice":{"isVoiceAvailable":false,"sampleRate":null}}}""").first.voiceAvailability()
        assertNull(before.isVoiceMessageAvailable)
    }

}
