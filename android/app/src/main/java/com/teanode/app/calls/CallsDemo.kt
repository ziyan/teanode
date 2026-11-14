package com.teanode.app.calls

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text
import com.teanode.kit.calls.ChatCallLine
import com.teanode.kit.calls.ChatCallMemberView
import com.teanode.kit.calls.ChatCallRecord
import com.teanode.kit.calls.ChatCallMemberRecord
import com.teanode.kit.calls.ChatCallTime
import com.teanode.kit.calls.ChatCallView

/**
 * The invented calls that the screenshots of calls photograph, and that demo mode shows: the banner of a chat with a
 * call going, the call screen with three people, the same call put away to its bar, a call of five with one featured,
 * a chat with calls in it (one that ended with its transcript, one missed and one going), and the transcript. The
 * same people as the rest of the demo.
 */
object CallsDemo {
    /** The moment the demo's calls are told from, in whole seconds: a call "four minutes twelve in" is that long before it. */
    val referenceNowMS: Long = System.currentTimeMillis().let { moment -> moment - moment % 1000 }

    private fun atSecondsAgo(seconds: Long): String = ChatCallTime.text(referenceNowMS - seconds * 1000)

    private data class Person(val userId: String, val name: String, val colorIndex: Int)

    private val robin = Person("robin", "Robin", 3)
    private val sam = Person("sam", "Sam", 5)
    private val alex = Person("alex", "Alex", 8)
    private val casey = Person("casey", "Casey", 1)
    private val morgan = Person("morgan", "Morgan", 10)

    private fun member(
        person: Person,
        ringState: String,
        isOn: Boolean,
        isVideoOn: Boolean = false,
        isMuted: Boolean = false,
        joinedAt: String? = null,
    ) = ChatCallMemberView(
        userId = person.userId, name = person.name, avatarColorIndex = person.colorIndex, ringState = ringState, isOn = isOn,
        isVideoOn = isVideoOn, isMuted = isMuted, joinedAt = joinedAt,
    )

    private fun view(
        id: String,
        conversationId: String,
        chatTitle: String,
        isGroup: Boolean,
        startedBy: Person,
        startedAt: String,
        answeredAt: String?,
        askerRingState: String,
        isAskerOn: Boolean,
        members: List<ChatCallMemberView>,
        transcriptionAskedBy: Person? = null,
    ) = ChatCallView(
        id = id, conversationId = conversationId, chatTitle = chatTitle, isGroup = isGroup, startedByUserId = startedBy.userId,
        startedByName = startedBy.name, startedAt = startedAt, answeredAt = answeredAt, isGoing = true, askerRingState = askerRingState,
        isAskerOn = isAskerOn, transcriptionAskedByUserId = transcriptionAskedBy?.userId, members = members,
    )

    /** The call going in the Home circle's chat: Sam and Alex are on it, the person has not joined. */
    val homeCall: ChatCallView
        get() {
            val startedAt = atSecondsAgo(95)
            return view(
                "call-home", "chat-home", "Home", true, sam, startedAt, startedAt, "ringing", false,
                listOf(
                    member(sam, "none", true, joinedAt = startedAt),
                    member(alex, "answered", true, joinedAt = startedAt),
                    member(robin, "ringing", false),
                ),
            )
        }

    /** The call of the call screen: Robin, Sam (with his camera on) and Alex, who has turned on transcription and is muted. Four minutes twelve in. */
    val callScreenCall: ChatCallView
        get() {
            val answeredAt = atSecondsAgo(252)
            return view(
                "call-screen", "chat-home", "Home", true, alex, answeredAt, answeredAt, "answered", true,
                listOf(
                    member(robin, "answered", true, joinedAt = answeredAt),
                    member(sam, "answered", true, isVideoOn = true, joinedAt = answeredAt),
                    member(alex, "none", true, isMuted = true, joinedAt = answeredAt),
                ),
                transcriptionAskedBy = alex,
            )
        }

    /** A call of five, big enough that whoever speaks is featured: Sam, with his camera on, above Robin, Alex, Casey and Morgan. */
    val featuredCall: ChatCallView
        get() {
            val answeredAt = atSecondsAgo(612)
            return view(
                "call-featured", "chat-home", "Home", true, alex, answeredAt, answeredAt, "answered", true,
                listOf(
                    member(robin, "answered", true, joinedAt = answeredAt),
                    member(sam, "answered", true, isVideoOn = true, joinedAt = answeredAt),
                    member(alex, "none", true, isMuted = true, joinedAt = answeredAt),
                    member(casey, "answered", true, joinedAt = answeredAt),
                    member(morgan, "answered", true, isMuted = true, joinedAt = answeredAt),
                ),
            )
        }

    /** A call being rung: Robin has started it and Sam has not answered yet. */
    val ringingOutCall: ChatCallView
        get() {
            val startedAt = atSecondsAgo(6)
            return view(
                "call-direct", "chat-sam", "Sam", false, robin, startedAt, null, "none", true,
                listOf(member(robin, "none", true, joinedAt = startedAt), member(sam, "ringing", false)),
            ).copy(isGoing = true)
        }

    private fun sessionOf(call: ChatCallView, isMinimized: Boolean = false, phase: CallPhase = CallPhase.Connected, isOutgoing: Boolean = false) = CallSessionState(
        entryId = "demo-" + call.id, accountKey = "demo", conversationId = call.conversationId, title = call.chatTitle, isOutgoing = isOutgoing,
        callId = call.id, call = call, phase = phase, ownUserId = robin.userId, isMinimized = isMinimized,
    )

    /** `calls`: the call screen with three people, one with his camera on, and the notice that Alex turned on transcription. */
    val callScreenSession: CallSessionState get() = sessionOf(callScreenCall)

    /** The same call put away to its bar. */
    val minimizedSession: CallSessionState get() = sessionOf(callScreenCall, isMinimized = true)

    /** A call of five with Sam, who is speaking, featured above the rest. */
    val featuredSession: CallSessionState get() = sessionOf(featuredCall).copy(speakingUserId = sam.userId)

    /** A call the person chose to feature one person of: Casey, pinned. */
    val chosenSession: CallSessionState get() = sessionOf(callScreenCall).copy(featuredUserId = alex.userId)

    /** A call the person started, still being rung. */
    val ringingOutSession: CallSessionState
        get() = sessionOf(ringingOutCall, phase = CallPhase.Connecting, isOutgoing = true).copy(isMuted = true)

    /** A call whose path was lost and is looked for again. */
    val reconnectingSession: CallSessionState get() = sessionOf(callScreenCall, phase = CallPhase.Reconnecting).copy(isCameraOn = true)

    /** The calls of the direct chat with Sam: one Robin started that went four minutes twelve and was transcribed, one Sam made that Robin missed, and one going now. */
    val directChatRecords: List<ChatCallRecord>
        get() {
            val endedStartedAt = atSecondsAgo(26 * 3600)
            val endedAnsweredAt = atSecondsAgo(26 * 3600 - 20)
            val endedEndedAt = atSecondsAgo(26 * 3600 - 272)
            val ended = ChatCallRecord(
                id = "call-direct-ended", startedByUserId = robin.userId, startedAt = endedStartedAt, answeredAt = endedAnsweredAt, endedAt = endedEndedAt,
                endReason = "ended", transcriptState = "done",
                members = listOf(
                    ChatCallMemberRecord(robin.userId, "none", endedStartedAt, endedEndedAt),
                    ChatCallMemberRecord(sam.userId, "answered", endedAnsweredAt, endedEndedAt),
                ),
            )
            val missedStartedAt = atSecondsAgo(3 * 3600)
            val missedEndedAt = atSecondsAgo(3 * 3600 - 60)
            val missed = ChatCallRecord(
                id = "call-direct-missed", startedByUserId = sam.userId, startedAt = missedStartedAt, endedAt = missedEndedAt, endReason = "missed",
                members = listOf(ChatCallMemberRecord(sam.userId, "none", missedStartedAt, missedEndedAt), ChatCallMemberRecord(robin.userId, "missed")),
            )
            val goingStartedAt = atSecondsAgo(120)
            val going = ChatCallRecord(
                id = "call-direct-going", startedByUserId = sam.userId, startedAt = goingStartedAt,
                members = listOf(ChatCallMemberRecord(sam.userId, "none", goingStartedAt), ChatCallMemberRecord(robin.userId, "ringing")),
            )
            return listOf(ended, missed, going)
        }

    /** A call that was declined and one that failed, for the lines that say so. */
    val otherRecords: List<ChatCallRecord>
        get() {
            val startedAt = atSecondsAgo(48 * 3600)
            val endedAt = atSecondsAgo(48 * 3600 - 30)
            return listOf(
                ChatCallRecord(
                    id = "call-declined", startedByUserId = robin.userId, startedAt = startedAt, endedAt = endedAt, endReason = "declined",
                    members = listOf(ChatCallMemberRecord(robin.userId, "none", startedAt, endedAt), ChatCallMemberRecord(sam.userId, "declined")),
                ),
                ChatCallRecord(
                    id = "call-failed", startedByUserId = sam.userId, startedAt = startedAt, endedAt = endedAt, endReason = "failed",
                    members = listOf(ChatCallMemberRecord(sam.userId, "none", startedAt, endedAt), ChatCallMemberRecord(robin.userId, "answered", startedAt, endedAt)),
                ),
                ChatCallRecord(
                    id = "call-no-answer", startedByUserId = robin.userId, startedAt = startedAt, endedAt = endedAt, endReason = "missed",
                    members = listOf(ChatCallMemberRecord(robin.userId, "none", startedAt, endedAt), ChatCallMemberRecord(sam.userId, "missed")),
                ),
            )
        }

    /** The line each record draws, for a reader who is Robin. */
    fun lineOf(record: ChatCallRecord): ChatCallLine = ChatCallLine.of(record, robin.userId, referenceNowMS)

    private fun json(call: ChatCallView): String {
        val members = call.members.joinToString(",") { member ->
            """{"userId":"${member.userId}","name":"${member.name}","avatarUrl":null,"avatarColorIndex":${member.avatarColorIndex},"ringState":"${member.ringState}","isOn":${member.isOn},"isVideoOn":${member.isVideoOn},"isMuted":${member.isMuted},"joinedAt":${member.joinedAt?.let { "\"$it\"" } ?: "null"},"leftAt":null}"""
        }
        return """{"id":"${call.id}","conversationId":"${call.conversationId}","chatTitle":"${call.chatTitle}","isGroup":${call.isGroup},"startedByUserId":"${call.startedByUserId}","startedByName":"${call.startedByName}","startedAt":"${call.startedAt}","answeredAt":${call.answeredAt?.let { "\"$it\"" } ?: "null"},"endedAt":null,"endReason":"","isGoing":${call.isGoing},"durationMs":0,"askerRingState":"${call.askerRingState}","isAskerOn":${call.isAskerOn},"transcriptionAskedByUserId":null,"transcriptState":"","summaryMessageId":null,"members":[$members]}"""
    }

    /** What the calls' operations answer in demo mode. */
    val answerers: List<DemoAnswerer> = listOf(
        graphQLField("ReadGoingChatCall") { variables ->
            if (variables.text("conversationId") == "chat-home") json(homeCall) else "null"
        },
        graphQLField("ListGoingChatCalls", "[]"),
        graphQLField("ReadChatCall") { _ ->
            val segments = listOf(
                Triple("segment-1", "robin" to "Robin", Triple(4_000, 9_000, "Are we still having the tart on Saturday?")),
                Triple("segment-2", "sam" to "Sam", Triple(10_000, 15_000, "Yes, bring two. Mom is making soup.")),
                Triple("segment-3", "robin" to "Robin", Triple(66_000, 70_000, "Perfect, I will be there at six.")),
            ).joinToString(",") { (id, who, timing) ->
                """{"id":"$id","callId":"call-direct-ended","userId":"${who.first}","speakerName":"${who.second}","startMs":${timing.first},"endMs":${timing.second},"text":"${timing.third}"}"""
            }
            val startedAt = atSecondsAgo(26 * 3600)
            val call = """{"id":"call-direct-ended","conversationId":"chat-sam","chatTitle":"Sam","isGroup":false,"startedByUserId":"robin","startedByName":"Robin","startedAt":"$startedAt","answeredAt":"$startedAt","endedAt":null,"endReason":"ended","isGoing":false,"durationMs":252000,"askerRingState":"none","isAskerOn":false,"transcriptionAskedByUserId":null,"transcriptState":"done","summaryMessageId":null,"members":[]}"""
            """{"call":$call,"transcript":[$segments],"recordingId":"recording-1","isRecordingOwned":true}"""
        },
        graphQLField("GetAppUpdate") { _ ->
            """{"updateChannel":"android","version":"1.4.0","buildNumber":"42","installUrl":"https://appdistribution.firebase.google.com/testerapps/example/releases/example"}"""
        },
    )
}

/** What demo mode answers for calls; listed with the parts' in `DemoServer`. */
val callsDemoAnswerers: List<DemoAnswerer> get() = CallsDemo.answerers
