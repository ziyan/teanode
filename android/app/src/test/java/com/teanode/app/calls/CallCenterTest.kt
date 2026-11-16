package com.teanode.app.calls

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.HTTPResponse
import com.teanode.kit.HTTPTransport
import com.teanode.kit.ServerAddress
import com.teanode.kit.SubscriptionTransport
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calls.ChatCallAudioRoute
import com.teanode.kit.notifications.NotificationContent
import com.teanode.kit.notifications.PayloadKey
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** A server that answers what the test says and keeps what was sent. */
private class FakeServer(private val answer: (HTTPRequest) -> Pair<Int, String>) : HTTPTransport {
    val sent = java.util.concurrent.CopyOnWriteArrayList<HTTPRequest>()

    override suspend fun send(request: HTTPRequest): HTTPResponse {
        sent.add(request)
        val (statusCode, body) = answer(request)
        return HTTPResponse(statusCode, body.toByteArray())
    }
}

/** The call center on a phone with no phone: a server that answers what a call asks, media that does nothing, and audio that only says it was taken. */
@OptIn(ExperimentalCoroutinesApi::class)
class CallCenterTest {
    private val goingCall = """
        {"id":"call-1","conversationId":"chat-1","chatTitle":"Home","isGroup":true,"startedByUserId":"user-robin","startedByName":"Robin","startedAt":"2030-01-01T10:00:00Z","answeredAt":"2030-01-01T10:00:20Z","endedAt":null,"endReason":"","isGoing":true,"durationMs":0,"askerRingState":"none","isAskerOn":true,"transcriptionAskedByUserId":null,"transcriptState":"","summaryMessageId":null,"members":[{"userId":"user-robin","name":"Robin","ringState":"none","isOn":true,"joinedAt":"2030-01-01T10:00:00Z"}]}
        """

    private class FakeMedia : CallMedia {
        override var listener: CallMediaListener? = null
        override val localVideoTrack: CallVideoTrack? = null
        var joinOffers = 0
        var answers = mutableListOf<String>()
        var isMicrophoneMuted = false
        var isClosed = false

        override suspend fun makeJoinOffer(): String {
            joinOffers += 1
            return "v=0 offer"
        }

        override suspend fun setJoinAnswer(answerSdp: String) {}

        override suspend fun answer(offerSdp: String): String {
            answers.add(offerSdp)
            return "v=0 answer"
        }

        override fun setMuted(isMuted: Boolean) {
            isMicrophoneMuted = isMuted
        }

        override suspend fun setCamera(isOn: Boolean, usesFrontCamera: Boolean): Boolean = true
        override suspend fun flipCamera(usesFrontCamera: Boolean): Boolean = true
        override fun close() {
            isClosed = true
        }
    }

    private class FakeAudio : CallAudio {
        override val route: StateFlow<ChatCallAudioRoute> = MutableStateFlow(ChatCallAudioRoute.Phone)
        override val availableRoutes: StateFlow<List<ChatCallAudioRoute>> = MutableStateFlow(listOf(ChatCallAudioRoute.Phone, ChatCallAudioRoute.Speaker))
        var isStarted = false
        override fun start() {
            isStarted = true
        }

        override fun stop() {
            isStarted = false
        }

        override fun choose(route: ChatCallAudioRoute) {}
    }

    private class Setup(val center: CallCenter, val server: FakeServer, val media: FakeMedia, val audio: FakeAudio, val problems: MutableList<String>, val serviceCalls: IntArray)

    private fun TestScope.setup(hasMicrophone: Boolean = true, answer: (document: String) -> String): Setup {
        val server = FakeServer { request ->
            val document = ((TeaNodeJson.parseToJsonElement(request.body!!.decodeToString()) as JsonObject)["query"] as kotlinx.serialization.json.JsonPrimitive).content
            200 to answer(document)
        }
        val silentFeed = object : SubscriptionTransport {
            override fun subscribe(connection: Connection, document: String, variables: Map<String, JsonElement>): Flow<JsonElement> = flow { awaitCancellation() }
        }
        val account = Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", System.currentTimeMillis() + 3_600_000, "robin")
        val connection = Connection(account, server, null, silentFeed)
        val media = FakeMedia()
        val audio = FakeAudio()
        val serviceCalls = intArrayOf(0)
        val environment = object : CallEnvironment {
            override val activeAccountKey: String = account.accountKey
            override fun connection(accountKey: String): Connection? = connection.takeIf { accountKey == account.accountKey }
            override fun phoneId(accountKey: String): String = "phone-1"
            override fun hasMicrophone(): Boolean = hasMicrophone
            override fun hasCamera(): Boolean = true
            override fun isAppInFront(): Boolean = true
            override fun describe(error: Throwable): String = error.message.orEmpty()
            override fun words(problem: CallProblem): String = problem.name
            override fun callStateChanged() {
                serviceCalls[0] += 1
            }
        }
        val center = CallCenter(environment, { media }, audio, scope = CoroutineScope(StandardTestDispatcher(testScheduler) + SupervisorJob()))
        val problems = mutableListOf<String>()
        backgroundScope.launch(UnconfinedTestDispatcher(testScheduler)) { center.problems.collect { text -> problems.add(text) } }
        return Setup(center, server, media, audio, problems, serviceCalls)
    }

    /** Lets what was asked happen: a few seconds of the call's own time, not until nothing is left (a call keeps asking for the media server's offers for as long as it goes). */
    private fun TestScope.settle() {
        advanceTimeBy(3_000)
        runCurrent()
    }

    private val answers: (String) -> String = { document ->
        when {
            document.contains("GetSession") -> """{"data":{"GetSession":{"userId":"user-robin","permissions":{"everywhere":[]}}}}"""
            document.contains("StartChatCall") -> """{"data":{"StartChatCall":$goingCall}}"""
            document.contains("JoinChatCallMedia") -> """{"data":{"JoinChatCallMedia":{"answerSdp":"v=0 answer"}}}"""
            document.contains("JoinChatCall(") -> """{"data":{"JoinChatCall":$goingCall}}"""
            document.contains("LeaveChatCall") -> """{"data":{"LeaveChatCall":$goingCall}}"""
            document.contains("DeclineChatCall") -> """{"data":{"DeclineChatCall":$goingCall}}"""
            document.contains("SetChatCallMuted") -> """{"data":{"SetChatCallMuted":$goingCall}}"""
            else -> """{"errors":[{"message":"unexpected"}]}"""
        }
    }

    @Test
    fun startingACallPlacesItJoinsTheMediaAndHangingUpLeavesIt() = runTest {
        val setup = setup(answer = answers)
        setup.center.startCall("chat-1", "Home")
        settle()
        val state = setup.center.session.value
        assertNotNull(state)
        assertEquals("call-1", state?.callId)
        assertEquals("user-robin", state?.ownUserId)
        assertTrue(state!!.isOutgoing)
        assertEquals(1, setup.media.joinOffers)
        assertTrue(setup.audio.isStarted)
        assertTrue(setup.serviceCalls[0] > 0, "the call service was asked to start")

        setup.center.setMuted(true)
        settle()
        assertTrue(setup.media.isMicrophoneMuted)
        assertTrue(setup.center.session.value!!.isMuted)

        setup.center.hangUp()
        settle()
        assertNull(setup.center.session.value)
        assertTrue(setup.media.isClosed)
        assertFalse(setup.audio.isStarted)
        assertTrue(setup.server.sent.any { request -> request.body!!.decodeToString().contains("LeaveChatCall") })
    }

    @Test
    fun aCallIsNotStartedWithoutTheMicrophone() = runTest {
        val setup = setup(hasMicrophone = false, answer = answers)
        setup.center.startCall("chat-1", "Home")
        settle()
        assertNull(setup.center.session.value)
        assertEquals(listOf("MicrophoneRefused"), setup.problems)
    }

    @Test
    fun aSecondCallWhileOnOneIsRefused() = runTest {
        val setup = setup(answer = answers)
        setup.center.startCall("chat-1", "Home")
        settle()
        setup.center.startCall("chat-2", "Work")
        settle()
        assertEquals(listOf("AlreadyOnCall"), setup.problems)
        assertEquals("chat-1", setup.center.session.value?.conversationId)
        // The call is ended so that nothing of it is left asking for the media server's offers when the test ends.
        setup.center.hangUp()
        settle()
    }

    @Test
    fun aRingFromAPushRingsOnceAndCanBeDeclined() = runTest {
        val setup = setup(answer = answers)
        val account = "https://mail.example.com|robin"
        val content = NotificationContent(notificationKind = "call", conversationId = "chat-1", callId = "call-1", title = "Sam", body = "Sam is calling")
        setup.center.receiveRing(PayloadKey.Opened(account, content))
        settle()
        assertEquals(1, setup.center.ringing.value.size)
        assertEquals("Sam", setup.center.ringing.value.first().callerName)
        // The same ring again does not ring twice.
        setup.center.receiveRing(PayloadKey.Opened(account, content))
        settle()
        assertEquals(1, setup.center.ringing.value.size)

        setup.center.declineRing(setup.center.ringing.value.first().entryId)
        settle()
        assertTrue(setup.center.ringing.value.isEmpty())
        assertTrue(setup.server.sent.any { request -> request.body!!.decodeToString().contains("DeclineChatCall") })
    }

    @Test
    fun aRingThatStoppedStopsRinging() = runTest {
        val setup = setup(answer = answers)
        val account = "https://mail.example.com|robin"
        setup.center.receiveRing(PayloadKey.Opened(account, NotificationContent(notificationKind = "call", conversationId = "chat-1", callId = "call-1", title = "Sam")))
        settle()
        assertEquals(1, setup.center.ringing.value.size)
        setup.center.receiveRing(PayloadKey.Opened(account, NotificationContent(notificationKind = "call_ended", callId = "call-1")))
        settle()
        assertTrue(setup.center.ringing.value.isEmpty())
    }

    @Test
    fun answeringARingJoinsTheCallAsTheSecondPhoneNamedInTheAnswer() = runTest {
        val setup = setup(answer = answers)
        val account = "https://mail.example.com|robin"
        setup.center.receiveRing(PayloadKey.Opened(account, NotificationContent(notificationKind = "call", conversationId = "chat-1", callId = "call-1", title = "Sam")))
        settle()
        setup.center.answerRing(setup.center.ringing.value.first().entryId)
        settle()
        val state = setup.center.session.value
        assertEquals("call-1", state?.callId)
        assertFalse(state!!.isOutgoing)
        assertTrue(setup.center.ringing.value.isEmpty())
        val join = setup.server.sent.first { request -> request.body!!.decodeToString().contains("JoinChatCall(") }
        assertTrue(join.body!!.decodeToString().contains("phone-1"), "the phone that answers names itself")
        assertEquals(1, setup.media.joinOffers)
        setup.center.hangUp()
        settle()
    }

    @Test
    fun theCallBannerIsBuiltFromTheEventsHeard() = runTest {
        val setup = setup(answer = answers)
        val view = TeaNodeJson.decodeFromString(com.teanode.kit.calls.ChatCallView.serializer(), goingCall)
        setup.center.note(view)
        assertEquals("call-1", setup.center.goingCallIds.value["chat-1"])
        setup.center.note(view.copy(isGoing = false, endReason = "ended"))
        assertNull(setup.center.goingCallIds.value["chat-1"])
        assertFalse(setup.center.callViews.value.getValue("call-1").isGoing)
    }
}
