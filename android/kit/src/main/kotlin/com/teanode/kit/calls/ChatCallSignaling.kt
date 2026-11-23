package com.teanode.kit.calls

/** ChatCallSignalingCommand is a step the device takes with the media server's peer connection. */
sealed class ChatCallSignalingCommand {
    /** Make the device's offer, wait for its addresses to be gathered, send it with JoinChatCallMedia, and set the answer that comes back. */
    data object Join : ChatCallSignalingCommand()

    /** Set this offer of the server's as the remote description, make the answer and send it with AnswerChatCallMedia. */
    data class Answer(val offerSdp: String) : ChatCallSignalingCommand()
}

/**
 * ChatCallSignaling is the order of a device's talk with the media server (docs/subsystems/chat-calls.md, "A device on
 * a call"). The device joins first; the server's offers are answered one at a time and in order; and an offer that
 * comes before the join's answer has been set is kept until then. It holds no connection: it says which step is next,
 * and the one that owns the connection takes it and tells it how it went.
 */
class ChatCallSignaling {
    enum class Phase {
        /** Nothing has been done. */
        Idle,

        /** The join is under way: the offers that arrive wait. */
        Joining,

        /** Joined, and no offer is being answered. */
        Ready,

        /** An offer is being answered: the offers that arrive wait. */
        Answering,

        /** Something failed; nothing more is done. */
        Failed,
    }

    var phase: Phase = Phase.Idle
        private set

    private val waiting = ArrayDeque<String>()

    /** The offers that have come and not been answered, oldest first. */
    val waitingOffers: List<String> get() = waiting.toList()

    /** The device begins: it joins. */
    fun start(): List<ChatCallSignalingCommand> {
        if (phase != Phase.Idle) return emptyList()
        phase = Phase.Joining
        return listOf(ChatCallSignalingCommand.Join)
    }

    /** An offer from the server: answered at once when the device is joined and not answering another, kept otherwise. */
    fun receive(offerSdp: String): List<ChatCallSignalingCommand> = when (phase) {
        Phase.Failed -> emptyList()
        Phase.Idle, Phase.Joining, Phase.Answering -> {
            waiting.addLast(offerSdp)
            emptyList()
        }
        Phase.Ready -> {
            waiting.addLast(offerSdp)
            next()
        }
    }

    /** The join's answer has been set: the offers that came meanwhile are answered now, the oldest first. */
    fun joinAnswered(): List<ChatCallSignalingCommand> {
        if (phase != Phase.Joining) return emptyList()
        phase = Phase.Ready
        return next()
    }

    /** The answer to the offer being answered has been sent: the next offer waiting is answered. */
    fun answerSent(): List<ChatCallSignalingCommand> {
        if (phase != Phase.Answering) return emptyList()
        phase = Phase.Ready
        return next()
    }

    /** A step failed. The call goes no further. */
    fun fail() {
        phase = Phase.Failed
        waiting.clear()
    }

    private fun next(): List<ChatCallSignalingCommand> {
        if (phase != Phase.Ready || waiting.isEmpty()) return emptyList()
        phase = Phase.Answering
        return listOf(ChatCallSignalingCommand.Answer(waiting.removeFirst()))
    }
}

/** ChatCallSDP reads what the app needs out of a session description. */
object ChatCallSDP {
    /** One media section of a description. */
    data class Section(
        /** audio or video. */
        val mediaKind: String,
        val mid: String,
        /**
         * The stream the section's track belongs to; the media server names it by the user id of whoever the track is
         * from. Null when the section has none (`a=msid:-`).
         */
        val streamId: String?,
        /** sendrecv, sendonly, recvonly or inactive; sendrecv when the section does not say. */
        val direction: String,
    ) {
        /** Whether the side that wrote the description sends in this section. */
        val isSending: Boolean get() = direction == "sendrecv" || direction == "sendonly"
    }

    /** The media sections of a description, in order. */
    fun sections(sdp: String): List<Section> {
        val sections = mutableListOf<Section>()
        var mediaKind: String? = null
        var mid = ""
        var streamId: String? = null
        var direction = "sendrecv"

        fun finish() {
            val kind = mediaKind
            if (kind != null && mid.isNotEmpty()) sections.add(Section(kind, mid, streamId, direction))
        }

        for (line in sdp.lines().map { text -> text.trimEnd('\r') }) {
            when {
                line.startsWith("m=") -> {
                    finish()
                    mediaKind = line.drop(2).split(" ").firstOrNull()
                    mid = ""
                    streamId = null
                    direction = "sendrecv"
                }
                line.startsWith("a=mid:") -> mid = line.removePrefix("a=mid:")
                line.startsWith("a=msid:") -> {
                    val stream = line.removePrefix("a=msid:").split(" ").firstOrNull()
                    streamId = if (stream == "-") null else stream
                }
                line in listOf("a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive") -> direction = line.drop(2)
            }
        }
        finish()
        return sections
    }
}
