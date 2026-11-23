package com.teanode.kit.calls

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.notifications.NotificationContent
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * ChatCallTest covers calls in chats: what the server answers, what each operation sends, the words of a call's line,
 * the order of the signaling, and what the phone shows for each push that rings.
 */
class ChatCallTest {
    private fun connectionAnswering(body: String): Pair<Connection, FakeServer> {
        val fake = FakeServer { 200 to body }
        val account = Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", System.currentTimeMillis() + 3_600_000, "mira")
        return Connection(account, fake) to fake
    }

    private fun sentVariables(fake: FakeServer): JsonObject =
        (TeaNodeJson.parseToJsonElement(fake.sent.last().body!!.decodeToString()) as JsonObject)["variables"] as JsonObject

    private fun sentDocument(fake: FakeServer): String =
        ((TeaNodeJson.parseToJsonElement(fake.sent.last().body!!.decodeToString()) as JsonObject)["query"] as JsonPrimitive).content

    /** A call of Mira's, going, with Tobias on it and Priya still ringing. */
    private val goingCall = """
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
            "isMuted":true,"joinedAt":null,"leftAt":null}]}
        """

    private fun decodedCall(json: String): ChatCallView = TeaNodeJson.decodeFromString(ChatCallView.serializer(), json)

    // MARK: Reading

    @Test
    fun aCallDecodesWithItsMembers() {
        val call = decodedCall(goingCall)
        assertEquals("call-1", call.id)
        assertTrue(call.isGoing)
        assertEquals(listOf("Mira", "Tobias", "Priya"), call.members.map { member -> member.name })
        assertEquals(2, call.onCount)
        assertEquals(listOf("user-mira", "user-tobias"), call.onMembers.map { member -> member.userId })
        assertEquals(true, call.member("user-tobias")?.isVideoOn)
        assertEquals(listOf(false, false, true), call.members.map { member -> member.isMuted })
        assertEquals(252_000L, call.durationMs)
        assertNotNull(call.answeredEpochMS)
        assertNull(call.endedAt)
    }

    @Test
    fun aCallMissingFieldsStillDecodes() {
        val call = decodedCall("""{"id":"call-2"}""")
        assertEquals("call-2", call.id)
        assertFalse(call.isGoing)
        assertTrue(call.members.isEmpty())
        assertEquals("", call.askerRingState)
    }

    @Test
    fun aCallLineReadsFromTheRecordAMessageCarries() {
        val record = TeaNodeJson.decodeFromString(
            ChatCallRecord.serializer(),
            """{"id":"call-1","startedByUserId":"user-mira","startedAt":"2030-01-01T10:00:00Z","answeredAt":"2030-01-01T10:00:20Z",
                "endedAt":"2030-01-01T10:04:32Z","endReason":"ended","transcriptionAskedByUserId":null,"transcriptState":"done",
                "summaryMessageId":null,
                "members":[{"userId":"user-mira","ringState":"none","joinedAt":"2030-01-01T10:00:00Z","leftAt":"2030-01-01T10:04:32Z","isVideoOn":false},
                           {"userId":"user-tobias","ringState":"answered","joinedAt":"2030-01-01T10:00:20Z","leftAt":"2030-01-01T10:04:30Z","isVideoOn":true}]}""",
        )
        assertEquals(2, record.members.size)
        val words = ChatCallLine.of(record, "user-tobias")
        assertEquals(ChatCallLineKind.Ended("4:12"), words.kind)
        assertTrue(words.isTranscriptDone)
        assertFalse(words.isGoing)
        assertEquals("answered", record.ringState("user-tobias"))
        assertEquals("", record.ringState(null))
    }

    // MARK: Operations

    @Test
    fun startingACallSendsTheChatAndVideo() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"StartChatCall":$goingCall}}""")
        val call = connection.startChatCall("conversation-1", isVideoOn = true)
        assertEquals("call-1", call.id)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("conversation-1"), variables["conversationId"])
        assertEquals(JsonPrimitive(true), variables["isVideoOn"])
        assertTrue(sentDocument(fake).contains("StartChatCall"))
        assertNull(variables["userIds"])
    }

    @Test
    fun startingACallCanRingOnlyThePeopleChosen() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"StartChatCall":$goingCall}}""")
        connection.startChatCall("conversation-1", userIds = listOf("user-tobias"))
        assertEquals(JsonArray(listOf(JsonPrimitive("user-tobias"))), sentVariables(fake)["userIds"])
    }

    @Test
    fun mutingTellsTheCall() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SetChatCallMuted":$goingCall}}""")
        connection.setChatCallMuted("call-1", isMuted = true)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("call-1"), variables["callId"])
        assertEquals(JsonPrimitive(true), variables["isMuted"])
        assertTrue(sentDocument(fake).contains("SetChatCallMuted"))
    }

    @Test
    fun joiningAndLeavingNameTheCall() = runBlocking {
        val (joining, joiningFake) = connectionAnswering("""{"data":{"JoinChatCall":$goingCall}}""")
        joining.joinChatCall("call-1")
        assertEquals(JsonPrimitive("call-1"), sentVariables(joiningFake)["callId"])
        assertTrue(sentDocument(joiningFake).contains("JoinChatCall("))

        val (leaving, leavingFake) = connectionAnswering("""{"data":{"LeaveChatCall":$goingCall}}""")
        leaving.leaveChatCall("call-1")
        assertTrue(sentDocument(leavingFake).contains("LeaveChatCall"))

        val (declining, decliningFake) = connectionAnswering("""{"data":{"DeclineChatCall":$goingCall}}""")
        declining.declineChatCall("call-1", agentPhoneId = "phone-1")
        assertTrue(sentDocument(decliningFake).contains("DeclineChatCall"))
        // The phone that declines names itself, so it is not told to stop ringing; with no phone, none is named.
        assertEquals(JsonPrimitive("phone-1"), sentVariables(decliningFake)["agentPhoneId"])
        assertNull(sentVariables(joiningFake)["agentPhoneId"])
    }

    @Test
    fun joiningTheMediaSendsTheOfferAndReturnsTheAnswer() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"JoinChatCallMedia":{"answerSdp":"v=0 answer"}}}""")
        val answer = connection.joinChatCallMedia("call-1", "v=0 offer")
        assertEquals("v=0 answer", answer)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("call-1"), variables["callId"])
        assertEquals(JsonPrimitive("v=0 offer"), variables["offerSdp"])
        // The device asks to be told who is speaking.
        assertTrue(sentDocument(fake).contains("shouldTellSpeaker: true"))
    }

    @Test
    fun featuringSendsThePersonOrNoneToFollowTheSpeaker() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SetChatCallFeatured":true}}""")
        connection.setChatCallFeatured("call-1", "user-tobias")
        assertEquals(JsonPrimitive("user-tobias"), sentVariables(fake)["userId"])
        connection.setChatCallFeatured("call-1", null)
        assertEquals(JsonNull, sentVariables(fake)["userId"])
    }

    @Test
    fun aSignalCanSayWhoIsSpeaking() {
        val speaking = TeaNodeJson.decodeFromString(ChatCallSignalView.serializer(), """{"offerSdp":"","speakingUserId":"user-tobias"}""")
        assertEquals("user-tobias", speaking.speakingUserId)
        val offer = TeaNodeJson.decodeFromString(ChatCallSignalView.serializer(), """{"offerSdp":"v=0","speakingUserId":null}""")
        assertNull(offer.speakingUserId)
        assertEquals("v=0", offer.offerSdp)
    }

    @Test
    fun theFeaturedPersonIsTheOneChosenElseTheSpeakerNeverOneself() {
        val members = decodedCall(goingCall).members
        fun featured(chosen: String, speaking: String, last: String): String? =
            ChatCallStage.featured(members, "user-mira", chosen, speaking, last)?.userId
        assertEquals("user-priya", featured("user-priya", "user-tobias", ""))
        assertEquals("user-tobias", featured("", "user-tobias", ""))
        assertEquals("user-priya", featured("", "user-mira", "user-priya"))
        assertEquals("user-tobias", featured("", "", ""))
        assertEquals("user-tobias", featured("user-gone", "", ""))
        assertFalse(ChatCallStage.isFeatured(4, ""))
        assertTrue(ChatCallStage.isFeatured(5, ""))
        assertTrue(ChatCallStage.isFeatured(2, "user-tobias"))
    }

    @Test
    fun answeringAnOfferSendsTheAnswer() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"AnswerChatCallMedia":true}}""")
        assertTrue(connection.answerChatCallMedia("call-1", "v=0 answer"))
        assertEquals(JsonPrimitive("v=0 answer"), sentVariables(fake)["answerSdp"])
    }

    @Test
    fun theCameraIsToldWithTheCall() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SetChatCallVideo":$goingCall}}""")
        connection.setChatCallVideo("call-1", isVideoOn = false)
        assertEquals(JsonPrimitive(false), sentVariables(fake)["isVideoOn"])
    }

    @Test
    fun transcriptionIsTurnedOn() = runBlocking {
        val (asking, askingFake) = connectionAnswering("""{"data":{"AskChatCallTranscription":$goingCall}}""")
        asking.askChatCallTranscription("call-1")
        assertTrue(sentDocument(askingFake).contains("AskChatCallTranscription"))
        assertEquals(JsonPrimitive("call-1"), sentVariables(askingFake)["callId"])
    }

    @Test
    fun theCallsGoingAreListed() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ListGoingChatCalls":[$goingCall]}}""")
        assertEquals(listOf("call-1"), connection.listGoingChatCalls().map { call -> call.id })
        assertTrue(sentDocument(fake).contains("ListGoingChatCalls"))
    }

    @Test
    fun aChatWithNoCallGoingReadsNull() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ReadGoingChatCall":null}}""")
        assertNull(connection.readGoingChatCall("conversation-1"))
        assertEquals(JsonPrimitive("conversation-1"), sentVariables(fake)["conversationId"])
    }

    @Test
    fun aChatWithACallGoingReadsIt() = runBlocking {
        val (connection, _) = connectionAnswering("""{"data":{"ReadGoingChatCall":$goingCall}}""")
        assertEquals(2, connection.readGoingChatCall("conversation-1")?.onCount)
    }

    @Test
    fun aChatsCallsAreListedAPageAtATime() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ListChatCalls":[$goingCall]}}""")
        connection.listChatCalls("conversation-1", first = 20, beforeEpochMS = 1_900_000_000_000L)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive(20), variables["first"])
        assertNotNull(variables["before"])

        connection.listChatCalls("conversation-1")
        assertNull(sentVariables(fake)["first"])
        assertNull(sentVariables(fake)["before"])
    }

    @Test
    fun aCallIsReadWithItsTranscript() = runBlocking {
        val body = """
            {"data":{"ReadChatCall":{"call":$goingCall,"transcript":[
              {"id":"s1","callId":"call-1","userId":"user-mira","speakerName":"Mira","startMs":4000,"endMs":9000,"text":"Shall we leave at nine?"},
              {"id":"s2","callId":"call-1","userId":null,"speakerName":"Tobias","startMs":65000,"endMs":70000,"text":"Nine works."}]}}}
            """
        val (connection, fake) = connectionAnswering(body)
        val detail = connection.readChatCall("call-1")
        assertEquals("call-1", detail.call.id)
        assertEquals(listOf("Mira", "Tobias"), detail.transcript.map { segment -> segment.speakerName })
        assertEquals(listOf("0:04", "1:05"), detail.transcript.map { segment -> segment.startText })
        assertNull(detail.transcript[1].userId)
        assertEquals(JsonPrimitive("call-1"), sentVariables(fake)["callId"])
    }

    // MARK: Words

    @Test
    fun aDurationIsMinutesAndSeconds() {
        assertEquals("0:00", ChatCallDuration.text(0))
        assertEquals("0:09", ChatCallDuration.text(9_999))
        assertEquals("4:12", ChatCallDuration.text(252_000))
        assertEquals("59:59", ChatCallDuration.text(3_599_000))
        assertEquals("1:04:12", ChatCallDuration.text(3_852_000))
        assertEquals("0:00", ChatCallDuration.text(-5))
    }

    @Test
    fun theLinesOfACallInEachState() {
        fun kind(reason: String, isGoing: Boolean = false, durationMs: Long = 0, isStarted: Boolean = false, isAnswered: Boolean = false) =
            ChatCallLineKind.of(reason, isGoing, durationMs, isStarted, isAnswered)
        assertEquals("Call going", kind("", isGoing = true).englishText)
        assertEquals("Call, 4:12", kind("ended", durationMs = 252_000, isAnswered = true).englishText)
        assertEquals("Missed call", kind("missed").englishText)
        assertEquals("No answer", kind("missed", isStarted = true).englishText)
        assertEquals("Declined call", kind("declined").englishText)
        assertEquals("Declined call", kind("declined", isStarted = true).englishText)
        assertEquals("Call failed", kind("failed").englishText)
        // Hung up while it rang: nobody answered, whatever the reason says.
        assertEquals("Missed call", kind("ended", isAnswered = false).englishText)
        assertEquals("No answer", kind("ended", isStarted = true, isAnswered = false).englishText)
    }

    @Test
    fun theLineIsWordedForWhoIsReadingIt() {
        val missed = decodedCall("""{"id":"c","startedByUserId":"user-mira","isGoing":false,"endReason":"missed","askerRingState":"missed","startedAt":"2030-01-01T10:00:00Z"}""")
        assertEquals(ChatCallLineKind.Missed, ChatCallLine.of(missed, "user-tobias").kind)
        assertEquals(ChatCallLineKind.NoAnswer, ChatCallLine.of(missed, "user-mira").kind)
        val going = decodedCall(goingCall)
        val line = ChatCallLine.of(going, "user-tobias")
        assertEquals(ChatCallLineKind.Going, line.kind)
        assertEquals(2, line.onCount)
        assertTrue(line.isGoing)
        assertFalse(line.isTranscriptDone)
    }

    @Test
    fun theLineOfACallStillGoingReadsFromTheMessage() {
        val record = TeaNodeJson.decodeFromString(
            ChatCallRecord.serializer(),
            """{"id":"c","startedByUserId":"user-mira","startedAt":"2030-01-01T10:00:00Z","answeredAt":null,"endedAt":null,"endReason":"","members":[{"userId":"user-mira","ringState":"none","joinedAt":"2030-01-01T10:00:00Z","leftAt":null}]}""",
        )
        val line = ChatCallLine.of(record, "user-mira")
        assertEquals(ChatCallLineKind.Going, line.kind)
        assertEquals(1, line.onCount)
        assertEquals(0L, record.durationMs())
    }

    @Test
    fun theTranscriptionNoticeSaysWhoTurnedItOn() {
        val call = decodedCall(goingCall)
        // Tobias is told Mira turned it on; nobody asks him.
        assertEquals(ChatCallTranscriptionStatus.Transcribing("Mira", isAskedByPerson = false), ChatCallTranscriptionStatus.of(call, "user-tobias"))
        // Mira turned it on herself.
        assertEquals(ChatCallTranscriptionStatus.Transcribing("Mira", isAskedByPerson = true), ChatCallTranscriptionStatus.of(call, "user-mira"))
        val nobodyAsked = call.copy(transcriptionAskedByUserId = null)
        assertEquals(ChatCallTranscriptionStatus.NotAsked, ChatCallTranscriptionStatus.of(nobodyAsked, "user-tobias"))
        assertTrue(ChatCallTranscriptionStatus.NotAsked.canAsk)
        assertFalse(ChatCallTranscriptionStatus.Transcribing("Mira", isAskedByPerson = false).canAsk)
        val ended = call.copy(isGoing = false)
        assertEquals(ChatCallTranscriptionStatus.NotAsked, ChatCallTranscriptionStatus.of(ended, "user-tobias"))
    }

    @Test
    fun thePeopleOnACallAreLaidOutInColumns() {
        assertEquals(listOf(1, 1, 1, 2, 2, 2, 2, 3, 3), (0..8).map { count -> ChatCallGrid.columnCount(count) })
        assertEquals(listOf(0, 1, 2, 2, 2, 3, 3, 3, 3), (0..8).map { count -> ChatCallGrid.rowCount(count) })
    }

    @Test
    fun theTimesTheServerWritesAreRead() {
        assertEquals(1_893_492_020_000L, ChatCallTime.epochMS("2030-01-01T10:00:20Z"))
        assertEquals(1_893_492_020_000L, ChatCallTime.epochMS("2030-01-01T10:00:20.123456789Z"))
        assertEquals(1_893_492_020_000L, ChatCallTime.epochMS("2030-01-01T10:00:20.5+00:00"))
        assertNull(ChatCallTime.epochMS("yesterday"))
        assertNull(ChatCallTime.epochMS(null))
        assertEquals("2030-01-01T10:00:20Z", ChatCallTime.text(1_893_492_020_999L))
    }

    // MARK: The signaling

    @Test
    fun theDeviceJoinsFirst() {
        val signaling = ChatCallSignaling()
        assertEquals(listOf(ChatCallSignalingCommand.Join), signaling.start())
        assertEquals(ChatCallSignaling.Phase.Joining, signaling.phase)
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.start())
    }

    @Test
    fun anOfferBeforeTheJoinsAnswerWaitsForIt() {
        val signaling = ChatCallSignaling()
        signaling.start()
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.receive("offer-1"))
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.receive("offer-2"))
        assertEquals(listOf(ChatCallSignalingCommand.Answer("offer-1")), signaling.joinAnswered())
        assertEquals(ChatCallSignaling.Phase.Answering, signaling.phase)
        assertEquals(listOf("offer-2"), signaling.waitingOffers)
    }

    @Test
    fun offersAreAnsweredOneAtATimeInOrder() {
        val signaling = ChatCallSignaling()
        signaling.start()
        signaling.joinAnswered()
        assertEquals(listOf(ChatCallSignalingCommand.Answer("offer-1")), signaling.receive("offer-1"))
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.receive("offer-2"))
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.receive("offer-3"))
        assertEquals(listOf(ChatCallSignalingCommand.Answer("offer-2")), signaling.answerSent())
        assertEquals(listOf(ChatCallSignalingCommand.Answer("offer-3")), signaling.answerSent())
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.answerSent())
        assertEquals(ChatCallSignaling.Phase.Ready, signaling.phase)
        assertEquals(listOf(ChatCallSignalingCommand.Answer("offer-4")), signaling.receive("offer-4"))
    }

    @Test
    fun anAnswerIsOnlyCountedWhenOneIsBeingMade() {
        val signaling = ChatCallSignaling()
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.answerSent())
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.joinAnswered())
        signaling.start()
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.answerSent())
        assertEquals(ChatCallSignaling.Phase.Joining, signaling.phase)
    }

    @Test
    fun aFailureStopsEverything() {
        val signaling = ChatCallSignaling()
        signaling.start()
        signaling.receive("offer-1")
        signaling.fail()
        assertEquals(ChatCallSignaling.Phase.Failed, signaling.phase)
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.joinAnswered())
        assertEquals(emptyList<ChatCallSignalingCommand>(), signaling.receive("offer-2"))
        assertTrue(signaling.waitingOffers.isEmpty())
    }

    @Test
    fun theSectionsOfAnOfferTellWhoseTrackEachIs() {
        val offer = listOf(
            "v=0", "o=- 1 2 IN IP4 127.0.0.1", "s=-", "t=0 0",
            "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:0", "a=sendonly", "a=msid:user-tobias track-a",
            "m=video 9 UDP/TLS/RTP/SAVPF 96", "a=mid:1", "a=sendonly", "a=msid:user-tobias track-v",
            "m=audio 9 UDP/TLS/RTP/SAVPF 111", "a=mid:2", "a=recvonly",
            "m=video 9 UDP/TLS/RTP/SAVPF 96", "a=mid:3", "a=inactive", "a=msid:- -",
        ).joinToString("\r\n") + "\r\n"
        val sections = ChatCallSDP.sections(offer)
        assertEquals(listOf("0", "1", "2", "3"), sections.map { section -> section.mid })
        assertEquals(listOf("audio", "video", "audio", "video"), sections.map { section -> section.mediaKind })
        assertEquals(listOf("user-tobias", "user-tobias", null, null), sections.map { section -> section.streamId })
        assertEquals(listOf(true, true, false, false), sections.map { section -> section.isSending })
    }

    // MARK: Ringing

    private fun content(kind: String, callId: String? = "call-1", title: String = "Mira"): NotificationContent {
        val callText = callId?.let { id -> """"callId":"$id",""" } ?: ""
        val json = """{"notificationKind":"$kind","conversationId":"conversation-1",$callText"title":"$title","body":"$title is calling"}"""
        return TeaNodeJson.decodeFromString(NotificationContent.serializer(), json)
    }

    @Test
    fun aCallRingsOnce() {
        val ledger = ChatCallLedger()
        val action = IncomingCallPush.decide(content("call"), "account-1", ledger, "first")
        assertEquals(IncomingCallPush.Action.Ring("first", "call-1", "conversation-1", "Mira", "account-1"), action)
        assertEquals(1, ledger.ringingEntries.size)
        // The same call again is not rung twice.
        assertEquals(IncomingCallPush.Action.Ignore("second"), IncomingCallPush.decide(content("call"), "account-1", ledger, "second"))
        assertEquals(1, ledger.ringingEntries.size)
    }

    @Test
    fun anEndedPushStopsTheRingForACallItRings() {
        val ledger = ChatCallLedger()
        IncomingCallPush.decide(content("call"), "account-1", ledger, "ringing")
        val action = IncomingCallPush.decide(content("call_ended"), "account-1", ledger)
        assertEquals(IncomingCallPush.Action.StopRinging("ringing"), action)
        assertTrue(ledger.entries.isEmpty())
    }

    @Test
    fun anEndedPushForACallItDoesNotKnowShowsNothing() {
        val ledger = ChatCallLedger()
        assertEquals(IncomingCallPush.Action.Ignore("fresh"), IncomingCallPush.decide(content("call_ended", "call-9"), "account-1", ledger, "fresh"))
        // And a ring that comes late for that call is not shown.
        assertEquals(IncomingCallPush.Action.Ignore("late"), IncomingCallPush.decide(content("call", "call-9"), "account-1", ledger, "late"))
    }

    @Test
    fun anEndedPushForTheCallThePhoneIsOnLeavesItAlone() {
        val ledger = ChatCallLedger()
        IncomingCallPush.decide(content("call"), "account-1", ledger, "ringing")
        assertNotNull(ledger.answer("ringing"))
        // The server tells the phones of the person that it was answered, this one too.
        assertEquals(IncomingCallPush.Action.Ignore("fresh"), IncomingCallPush.decide(content("call_ended"), "account-1", ledger, "fresh"))
        assertEquals(ChatCallLedger.Phase.Joining, ledger.entry("ringing")?.phase)
    }

    @Test
    fun aPushThatCannotBeReadShowsNothing() {
        val ledger = ChatCallLedger()
        assertEquals(IncomingCallPush.Action.Ignore("fresh"), IncomingCallPush.decide(null, null, ledger, "fresh"))
        assertEquals(IncomingCallPush.Action.Ignore("fresh"), IncomingCallPush.decide(content("call", callId = null), "account-1", ledger, "fresh"))
        assertEquals(IncomingCallPush.Action.Ignore("fresh"), IncomingCallPush.decide(content("chat"), "account-1", ledger, "fresh"))
        assertTrue(ledger.entries.isEmpty())
    }

    @Test
    fun aCallAnsweredElsewhereStopsRinging() {
        val ledger = ChatCallLedger()
        ledger.ringIncoming("call-1", "conversation-1", "Mira", "account-1", "ringing")
        var view = decodedCall(goingCall).copy(askerRingState = "ringing")
        assertEquals(ChatCallLedger.Reaction.None, ledger.apply(view))
        view = view.copy(askerRingState = "answered")
        assertEquals(ChatCallLedger.Reaction.End("ringing", ChatCallEndReason.AnsweredElsewhere), ledger.apply(view))
        assertTrue(ledger.entries.isEmpty())
    }

    @Test
    fun aCallDeclinedElsewhereOrMissedStopsRinging() {
        val ledger = ChatCallLedger()
        ledger.ringIncoming("call-1", "conversation-1", "Mira", "account-1", "declined")
        val view = decodedCall(goingCall).copy(askerRingState = "declined")
        assertEquals(ChatCallLedger.Reaction.End("declined", ChatCallEndReason.DeclinedElsewhere), ledger.apply(view))
        ledger.ringIncoming("call-2", "conversation-1", "Mira", "account-1", "missed")
        val ended = decodedCall(goingCall).copy(id = "call-2", isGoing = false, endReason = "missed")
        assertEquals(ChatCallLedger.Reaction.End("missed", ChatCallEndReason.Unanswered), ledger.apply(ended))
    }

    @Test
    fun aCallTheServerEndsEndsForThePhoneOnIt() {
        val ledger = ChatCallLedger()
        ledger.ringIncoming("call-1", "conversation-1", "Mira", "account-1", "entry")
        ledger.answer("entry")
        ledger.connected("entry")
        var view = decodedCall(goingCall)
        assertEquals(ChatCallLedger.Reaction.None, ledger.apply(view))
        view = view.copy(isGoing = false, endReason = "ended")
        assertEquals(ChatCallLedger.Reaction.End("entry", ChatCallEndReason.RemoteEnded), ledger.apply(view))
        assertNull(ledger.activeEntry)
    }

    @Test
    fun aCallAPersonStartsIsKnownOnceTheServerMakesIt() {
        val ledger = ChatCallLedger()
        ledger.place("conversation-1", "Weekend plans", "account-1", "entry")
        assertEquals(ChatCallLedger.Phase.Placing, ledger.activeEntry?.phase)
        assertNull(ledger.entry("entry")?.callId)
        assertNull(ledger.attach("call-1", "entry"))
        assertEquals("entry", ledger.entryOfCall("call-1")?.entryId)
    }

    @Test
    fun startingACallAlreadyRingingHereTakesOverTheRing() {
        val ledger = ChatCallLedger()
        ledger.ringIncoming("call-1", "conversation-1", "Mira", "account-1", "ringing")
        ledger.place("conversation-1", "Weekend plans", "account-1", "placed")
        assertEquals("ringing", ledger.attach("call-1", "placed"))
        assertEquals(listOf("placed"), ledger.entries.map { entry -> entry.entryId })
    }

    @Test
    fun readingTheCallsGoingEndsTheRingsThatAreOver() {
        val ledger = ChatCallLedger()
        ledger.ringIncoming("call-1", "conversation-1", "Mira", "account-1", "kept")
        ledger.ringIncoming("call-2", "conversation-1", "Mira", "account-1", "gone")
        val stillRinging = decodedCall(goingCall).copy(askerRingState = "ringing")
        val reactions = ledger.reconcile(listOf(stillRinging))
        assertEquals(listOf<ChatCallLedger.Reaction>(ChatCallLedger.Reaction.End("gone", ChatCallEndReason.RemoteEnded)), reactions)
        assertEquals(listOf("kept"), ledger.entries.map { entry -> entry.entryId })
    }
}
