package com.teanode.kit.calls

import kotlinx.serialization.Serializable

// A call with the agent: what the person says is heard by the server and becomes a turn, and the agent's answers are
// read back to them. This file is what the voice socket says, and the bookkeeping of the answer being read: which
// sentences are asked for ahead of the one playing, which have played, and how much of an answer was heard when the
// person talks over it. The microphone, the speaker and the socket are the app's (`AgentVoiceCall`).

/** VoiceCallEvent is one thing the server says on the voice socket (`/api/v1/agent/voice`, the same socket the dashboard's call uses). */
@Serializable
data class VoiceCallEvent(
    /** ready, speechStarted, speechStopped, transcriptDelta, transcriptFinal, transcriptFailed, answerAudio, answerAudioDone, answerAudioFailed, problem, refused or error. */
    val voiceEvent: String,
    val utteranceId: String? = null,
    val transcriptText: String? = null,
    val errorMessage: String? = null,
    val answerSegmentId: String? = null,
    /** Some of a spoken answer: base64 of mono 16-bit PCM at 24000 Hz. */
    val answerAudio: String? = null,
)

/** VoiceCallState is what a call with the agent is doing. */
enum class VoiceCallState { Starting, Listening, Hearing, Working, Speaking, Ended }

/** What the person heard of an answer they talked over, and what they did not. */
data class InterruptedAnswer(val heardText: String, val unheardText: String)

/** VoiceAnswerQueue is the answer being read to the person, sentence by sentence. */
class VoiceAnswerQueue(private val callName: String, private val ahead: Int = AHEAD) {
    class Segment(val segmentId: String, val text: String, var isDone: Boolean = false, var isPlayed: Boolean = false)

    /** A sentence the server is asked to say. */
    data class SpeakRequest(val segmentId: String, val text: String)

    private val segments = mutableListOf<Segment>()
    private val waitingSentences = ArrayDeque<String>()
    private var segmentCount = 0

    /** Whether an answer is being played, or asked for and not yet played. */
    val isSpeakingAnswer: Boolean get() = segments.any { segment -> !segment.isPlayed }

    /** Whether sentences wait to be asked for. */
    val hasWaitingSentences: Boolean get() = waitingSentences.isNotEmpty()

    /** Whether anything is left to be said or heard. */
    val isBusy: Boolean get() = isSpeakingAnswer || hasWaitingSentences

    /** The sentences of text, queued; returns what to ask the server for now. */
    fun speak(sentences: List<String>): List<SpeakRequest> {
        waitingSentences.addAll(sentences)
        return requestSentences()
    }

    /** Asks for the next sentences, a few ahead of the one playing. */
    fun requestSentences(): List<SpeakRequest> {
        val requests = mutableListOf<SpeakRequest>()
        while (waitingSentences.isNotEmpty() && segments.count { segment -> !segment.isDone } < ahead) {
            val text = waitingSentences.removeFirst()
            segmentCount += 1
            val segment = Segment("$callName-$segmentCount", text)
            segments.add(segment)
            requests.add(SpeakRequest(segment.segmentId, text))
        }
        return requests
    }

    /** The server has sent all the sound of a segment (or could not make it); returns what to ask for next. */
    fun segmentDone(segmentId: String?): List<SpeakRequest> {
        segments.firstOrNull { segment -> segment.segmentId == segmentId }?.isDone = true
        return requestSentences()
    }

    /** Whether a segment is known, so that sound arriving for one that was cut short is dropped. */
    fun isKnown(segmentId: String): Boolean = segments.any { segment -> segment.segmentId == segmentId }

    /**
     * A segment's last sound has played. A segment is played once it is done arriving. Returns whether nothing more is
     * to be said, which ends the answer.
     */
    fun played(segmentId: String): Boolean {
        val segment = segments.firstOrNull { candidate -> candidate.segmentId == segmentId } ?: return false
        if (segment.isDone) {
            segment.isPlayed = true
            segments.removeAll { candidate -> candidate.isPlayed && candidate.segmentId != segmentId }
        }
        return !isSpeakingAnswer && waitingSentences.isEmpty()
    }

    /** What was heard and what was not, when the person talks over the answer. */
    fun interruption(): InterruptedAnswer {
        val heard = segments.filter { segment -> segment.isPlayed }.joinToString(" ") { segment -> segment.text }
        val unheard = (segments.filter { segment -> !segment.isPlayed }.map { segment -> segment.text } + waitingSentences).joinToString(" ")
        return InterruptedAnswer(heard, unheard)
    }

    /** Stops the answer; returns the segments the server is told to stop making. */
    fun stop(): List<String> {
        val pending = segments.filter { segment -> !segment.isPlayed }.map { segment -> segment.segmentId }
        segments.clear()
        waitingSentences.clear()
        return pending
    }

    companion object {
        /** How many sentences are asked for ahead of the one playing. */
        const val AHEAD = 3
    }
}

/** What the agent's events add to a call that started a turn, in words to be spoken. */
object VoiceCallSpeech {
    /** A spoken question: its note and its choices. */
    fun questionText(note: String?, choices: String?): String =
        listOf(note ?: "", (choices ?: "").split("\n").filter { line -> line.isNotEmpty() }.joinToString(", ")).filter { part -> part.isNotEmpty() }.joinToString(" ")
}
