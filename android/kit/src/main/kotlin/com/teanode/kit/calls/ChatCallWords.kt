package com.teanode.kit.calls

// What the screens say of a call, worked out from what the server sent and kept apart from the screens so that it is
// tested here. The app words each kind in the person's language (the string resources); `englishText` is the English
// it starts from.

/** ChatCallDuration writes how long a call went. */
object ChatCallDuration {
    /** "4:12", and from an hour "1:04:12". A part of a second is dropped. */
    fun text(milliseconds: Long): String {
        val totalSeconds = maxOf(0, milliseconds) / 1000
        val seconds = totalSeconds % 60
        val minutes = (totalSeconds / 60) % 60
        val hours = totalSeconds / 3600
        val secondsText = if (seconds < 10) "0$seconds" else "$seconds"
        if (hours > 0) {
            val minutesText = if (minutes < 10) "0$minutes" else "$minutes"
            return "$hours:$minutesText:$secondsText"
        }
        return "$minutes:$secondsText"
    }
}

/** ChatCallLineKind is what a call's line in a chat says. */
sealed class ChatCallLineKind {
    /** The call goes now. */
    data object Going : ChatCallLineKind()

    /** It was answered and has ended, after the time given ("4:12"). */
    data class Ended(val durationText: String) : ChatCallLineKind()

    /** Nobody answered, for somebody it rang. */
    data object Missed : ChatCallLineKind()

    /** Nobody answered, for whoever started it. */
    data object NoAnswer : ChatCallLineKind()

    data object Declined : ChatCallLineKind()

    /** The server stopped under it. */
    data object Failed : ChatCallLineKind()

    /** The line in English: "Call, 4:12", "Call going", "Missed call", "No answer", "Declined call" or "Call failed". */
    val englishText: String
        get() = when (this) {
            Going -> "Call going"
            is Ended -> "Call, $durationText"
            Missed -> "Missed call"
            NoAnswer -> "No answer"
            Declined -> "Declined call"
            Failed -> "Call failed"
        }

    companion object {
        /**
         * The kind for a call that has the facts given. A call that ended with nobody answering it (the person who
         * started it hung up while it rang) reads as missed or as no answer, not as a call of no length.
         */
        fun of(endReason: String, isGoing: Boolean, durationMs: Long, isStartedByPerson: Boolean, isAnswered: Boolean): ChatCallLineKind {
            if (isGoing) return Going
            return when (endReason) {
                "missed" -> if (isStartedByPerson) NoAnswer else Missed
                "declined" -> Declined
                "failed" -> Failed
                else -> {
                    if (!isAnswered) return if (isStartedByPerson) NoAnswer else Missed
                    Ended(ChatCallDuration.text(durationMs))
                }
            }
        }
    }
}

/**
 * ChatCallLine is what a call's line in a chat, and the banner of a chat with a call going, say: from the call as a
 * message carries it, or from the newest view of it the app has heard.
 */
data class ChatCallLine(
    val callId: String,
    val kind: ChatCallLineKind,
    /** How many people are on the call now. */
    val onCount: Int,
    val isGoing: Boolean,
    /** Whether the call has a transcript to read. */
    val isTranscriptDone: Boolean,
) {
    companion object {
        /** From the newest view of the call. */
        fun of(view: ChatCallView, ownUserId: String?): ChatCallLine = ChatCallLine(
            callId = view.id,
            kind = ChatCallLineKind.of(
                endReason = view.endReason, isGoing = view.isGoing, durationMs = view.durationMs,
                isStartedByPerson = view.isStartedBy(ownUserId), isAnswered = view.answeredAt != null,
            ),
            onCount = view.onCount, isGoing = view.isGoing, isTranscriptDone = view.transcriptState == "done",
        )

        /** From the call as the message carries it. */
        fun of(record: ChatCallRecord, ownUserId: String?, nowEpochMS: Long = System.currentTimeMillis()): ChatCallLine {
            val isStartedByPerson = ownUserId != null && record.startedByUserId == ownUserId
            return ChatCallLine(
                callId = record.id,
                kind = ChatCallLineKind.of(
                    endReason = record.endReason, isGoing = record.isGoing, durationMs = record.durationMs(nowEpochMS),
                    isStartedByPerson = isStartedByPerson, isAnswered = record.answeredAt != null,
                ),
                onCount = record.onCount, isGoing = record.isGoing, isTranscriptDone = record.transcriptState == "done",
            )
        }
    }
}

/**
 * ChatCallTranscriptionStatus is what the call screen says of a transcript: whether the call is being transcribed, and
 * who turned it on. Turning it on transcribes everybody on the call; nobody is asked, everybody is told.
 */
sealed class ChatCallTranscriptionStatus {
    /** Nobody turned it on. */
    data object NotAsked : ChatCallTranscriptionStatus()

    /** Somebody turned it on: "Mira turned on transcription", or "You turned on transcription" when it was the person. */
    data class Transcribing(val askerName: String, val isAskedByPerson: Boolean) : ChatCallTranscriptionStatus()

    /** Whether the transcribe button is offered: nobody has asked yet. */
    val canAsk: Boolean get() = this == NotAsked

    companion object {
        fun of(view: ChatCallView, ownUserId: String?): ChatCallTranscriptionStatus {
            val askerId = view.transcriptionAskedByUserId
            if (askerId.isNullOrEmpty() || !view.isGoing) return NotAsked
            return Transcribing(askerName = view.member(askerId)?.name ?: "", isAskedByPerson = askerId == ownUserId)
        }
    }
}

/**
 * ChatCallStage is who is featured on a call: one person whole above a row of the rest, once the call is big enough
 * for the grid's pictures to be small, or once the person chose somebody. The server sends the featured picture whole
 * and the others small.
 */
object ChatCallStage {
    /** How many people on a call turn the grid into one featured above the rest. */
    const val FEATURED_FROM = 5

    /** Whether one person is featured above the rest. */
    fun isFeatured(tileCount: Int, chosenUserId: String): Boolean = tileCount >= FEATURED_FROM || chosenUserId.isNotEmpty()

    /**
     * Who is featured: the one chosen, or else whoever is speaking, or whoever spoke last while this person speaks, or
     * else the first of the others. Never this person: their own picture is in the row.
     */
    fun featured(
        members: List<ChatCallMemberView>,
        ownUserId: String,
        chosenUserId: String,
        speakingUserId: String,
        lastOtherSpeakerId: String,
    ): ChatCallMemberView? {
        val others = members.filter { member -> member.userId != ownUserId }
        fun named(userId: String): ChatCallMemberView? = if (userId.isEmpty()) null else others.firstOrNull { member -> member.userId == userId }
        return named(chosenUserId) ?: named(speakingUserId) ?: named(lastOtherSpeakerId) ?: others.firstOrNull()
    }
}

/** ChatCallGrid lays the people on a call out. */
object ChatCallGrid {
    /** How many columns the tiles of the people are laid in: one or two people fill the screen one above the other, up to six are two across, and more are three across. */
    fun columnCount(tileCount: Int): Int = when {
        tileCount <= 2 -> 1
        tileCount <= 6 -> 2
        else -> 3
    }

    /** How many rows the tiles take. */
    fun rowCount(tileCount: Int): Int {
        if (tileCount <= 0) return 0
        val columns = columnCount(tileCount)
        return (tileCount + columns - 1) / columns
    }
}
