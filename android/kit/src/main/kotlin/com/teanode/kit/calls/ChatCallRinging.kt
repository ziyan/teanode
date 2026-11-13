package com.teanode.kit.calls

import com.teanode.kit.notifications.NotificationContent
import java.util.UUID

// How the phone rings for a call. The system's call screen knows a call by an id the app makes; the server knows it by
// an id of its own. The ledger keeps the two together and says what to tell the system when the server says something
// about a call, in one place, so that it is tested without a phone. Every push has an answer here, so that a late or
// repeated ring is never shown twice.

/** ChatCallEndReason is why a call ended for the phone ringing for it, which the incoming call notification words. */
enum class ChatCallEndReason {
    /** The person answered on another device. */
    AnsweredElsewhere,

    /** The person said no on another device. */
    DeclinedElsewhere,

    /** Nobody answered before the ringing stopped. */
    Unanswered,

    /** The call ended. */
    RemoteEnded,

    Failed,
}

/** ChatCallLedger is the calls this phone rings for, is placing, or is on. */
class ChatCallLedger {
    enum class Phase {
        /** An outgoing call, started on this phone, not yet known to the server. */
        Placing,

        /** An incoming call, ringing. */
        Ringing,

        /** The person answered, or joined, and the device is connecting. */
        Joining,

        /** The device is on the call. */
        Connected,
    }

    data class Entry(
        val entryId: String,
        /** The server's id of the call; null while an outgoing call is placed. */
        val callId: String?,
        val conversationId: String,
        /** Who is calling, or the chat that is called: what the incoming call shows. */
        val title: String,
        val accountKey: String,
        val isOutgoing: Boolean,
        val phase: Phase,
        /** Whether the ring says the call has the caller's camera on. */
        val isVideoOn: Boolean = false,
    )

    private val kept = mutableListOf<Entry>()

    /** The calls seen ending lately, so that a ring that comes late for one is not shown. Bounded. */
    private val endedCallIds = ArrayDeque<String>()

    val entries: List<Entry> get() = kept.toList()

    // MARK: Looking up

    fun entry(entryId: String): Entry? = kept.firstOrNull { entry -> entry.entryId == entryId }

    fun entryOfCall(callId: String): Entry? = kept.firstOrNull { entry -> entry.callId == callId }

    /** The call the phone is on, or going onto: the one that is not ringing. */
    val activeEntry: Entry? get() = kept.firstOrNull { entry -> entry.phase != Phase.Ringing }

    val ringingEntries: List<Entry> get() = kept.filter { entry -> entry.phase == Phase.Ringing }

    // MARK: A ring from the server

    /** What to show for a push of a call ringing. */
    sealed class RingDecision {
        /** A new incoming call under this id: it rings. */
        data class Ring(val entryId: String) : RingDecision()

        /** A call the phone knows already, or knows to have ended: it is not rung twice. */
        data class Ignore(val entryId: String) : RingDecision()
    }

    fun ringIncoming(
        callId: String,
        conversationId: String,
        title: String,
        accountKey: String,
        entryId: String = UUID.randomUUID().toString(),
    ): RingDecision {
        if (entryOfCall(callId) != null || endedCallIds.contains(callId)) return RingDecision.Ignore(entryId)
        kept.add(Entry(entryId, callId, conversationId, title, accountKey, isOutgoing = false, phase = Phase.Ringing))
        return RingDecision.Ring(entryId)
    }

    /** What to do for a push saying a call no longer rings the phone. */
    sealed class StopDecision {
        /** The phone rings for it: stop that ring. */
        data class StopRinging(val entryId: String) : StopDecision()

        /** The phone does not ring for it (it is on the call, or never knew it): nothing to stop. */
        data class Ignore(val entryId: String) : StopDecision()
    }

    /**
     * The server says the call no longer rings the phone. The phone's own answer is told to its other phones too, so
     * a call the phone is on can be told of: only a call still ringing stops.
     */
    fun stopRinging(callId: String, entryId: String = UUID.randomUUID().toString()): StopDecision {
        val found = entryOfCall(callId)
        if (found == null) {
            // Told before the ring: the ring that follows is not shown.
            noteEnded(callId)
            return StopDecision.Ignore(entryId)
        }
        if (found.phase != Phase.Ringing) return StopDecision.Ignore(entryId)
        remove(found.entryId)
        return StopDecision.StopRinging(found.entryId)
    }

    private fun noteEnded(callId: String) {
        if (endedCallIds.contains(callId)) return
        endedCallIds.addLast(callId)
        while (endedCallIds.size > ENDED_CALL_IDS_KEPT) endedCallIds.removeFirst()
    }

    // MARK: A call the person starts, answers or ends

    /** A call started on this phone, before the server has made it. */
    fun place(conversationId: String, title: String, accountKey: String, entryId: String = UUID.randomUUID().toString()): Entry {
        val placed = Entry(entryId, null, conversationId, title, accountKey, isOutgoing = true, phase = Phase.Placing)
        kept.add(placed)
        return placed
    }

    /**
     * The server made the call, or said the chat has one going already. When this phone was ringing for that very
     * call, the ringing entry is dropped and its id returned, for the incoming call notification to be taken away.
     */
    fun attach(callId: String, entryId: String): String? {
        var displaced: String? = null
        val ringing = kept.firstOrNull { entry -> entry.callId == callId && entry.entryId != entryId && entry.phase == Phase.Ringing }
        if (ringing != null) {
            displaced = ringing.entryId
            kept.removeAll { entry -> entry.entryId == ringing.entryId }
        }
        val index = kept.indexOfFirst { entry -> entry.entryId == entryId }
        if (index >= 0) kept[index] = kept[index].copy(callId = callId)
        return displaced
    }

    /** The person answered the ringing call: the device is joining it. */
    fun answer(entryId: String): Entry? {
        val index = kept.indexOfFirst { entry -> entry.entryId == entryId && entry.phase == Phase.Ringing }
        if (index < 0) return null
        kept[index] = kept[index].copy(phase = Phase.Joining)
        return kept[index]
    }

    /** The device is on the call. */
    fun connected(entryId: String) {
        val index = kept.indexOfFirst { entry -> entry.entryId == entryId }
        if (index >= 0) kept[index] = kept[index].copy(phase = Phase.Connected)
    }

    /** The call is over for the phone, however it ended. */
    fun remove(entryId: String): Entry? {
        val index = kept.indexOfFirst { entry -> entry.entryId == entryId }
        if (index < 0) return null
        val removed = kept.removeAt(index)
        removed.callId?.let { callId -> noteEnded(callId) }
        return removed
    }

    // MARK: A change the server tells

    /** What a change to a call asks of the incoming call and the call screen. */
    sealed class Reaction {
        data object None : Reaction()

        /** The phone rings for it, or is on it, and it is over: show it ended for this reason. The entry has been dropped. */
        data class End(val entryId: String, val reason: ChatCallEndReason) : Reaction()
    }

    /** A ChatCallEvents change, as the person sees the call now. */
    fun apply(view: ChatCallView): Reaction {
        val found = entryOfCall(view.id) ?: return Reaction.None
        if (found.phase == Phase.Ringing) {
            if (!view.isGoing) {
                remove(found.entryId)
                return Reaction.End(found.entryId, if (view.endReason == "missed") ChatCallEndReason.Unanswered else ChatCallEndReason.RemoteEnded)
            }
            val reason = when (view.askerRingState) {
                "answered" -> ChatCallEndReason.AnsweredElsewhere
                "declined" -> ChatCallEndReason.DeclinedElsewhere
                "missed" -> ChatCallEndReason.Unanswered
                else -> null
            } ?: return Reaction.None
            remove(found.entryId)
            return Reaction.End(found.entryId, reason)
        }
        if (!view.isGoing) {
            remove(found.entryId)
            return Reaction.End(found.entryId, if (view.endReason == "failed") ChatCallEndReason.Failed else ChatCallEndReason.RemoteEnded)
        }
        return Reaction.None
    }

    /**
     * The calls going, as the person's phone reads them when it opens or finds its feed dropped: a call this phone
     * rings for that is no longer going or ringing it has ended meanwhile.
     */
    fun reconcile(going: List<ChatCallView>): List<Reaction> {
        val reactions = mutableListOf<Reaction>()
        for (found in ringingEntries) {
            val callId = found.callId ?: continue
            val view = going.firstOrNull { candidate -> candidate.id == callId }
            if (view != null) {
                val reaction = apply(view)
                if (reaction != Reaction.None) reactions.add(reaction)
            } else {
                remove(found.entryId)
                reactions.add(Reaction.End(found.entryId, ChatCallEndReason.RemoteEnded))
            }
        }
        return reactions
    }

    private companion object {
        const val ENDED_CALL_IDS_KEPT = 30
    }
}

/** IncomingCallPush is what a push of a call says, once its sealed content is opened. */
object IncomingCallPush {
    /** What to do for the push, and to remember. */
    sealed class Action {
        /** A call rings: show an incoming call. */
        data class Ring(val entryId: String, val callId: String, val conversationId: String, val callerName: String, val accountKey: String) : Action()

        /** A call the phone rings for no longer does: take the ring away. */
        data class StopRinging(val entryId: String) : Action()

        /** Show nothing: the push is for a call the phone does not ring for, or could not be read. */
        data class Ignore(val entryId: String) : Action()
    }

    /** Decides what a push asks. [content] is null when it could not be opened (no key of an account on the phone fits it). */
    fun decide(content: NotificationContent?, accountKey: String?, ledger: ChatCallLedger, entryId: String = UUID.randomUUID().toString()): Action {
        val callId = content?.callId
        if (content == null || accountKey == null || callId.isNullOrEmpty()) return Action.Ignore(entryId)
        return when (content.notificationKind) {
            "call" -> {
                val callerName = content.title.ifEmpty { content.body }
                val conversationId = content.conversationId ?: ""
                when (val decision = ledger.ringIncoming(callId, conversationId, callerName, accountKey, entryId)) {
                    is ChatCallLedger.RingDecision.Ring -> Action.Ring(decision.entryId, callId, conversationId, callerName, accountKey)
                    is ChatCallLedger.RingDecision.Ignore -> Action.Ignore(decision.entryId)
                }
            }
            "call_ended" -> when (val decision = ledger.stopRinging(callId, entryId)) {
                is ChatCallLedger.StopDecision.StopRinging -> Action.StopRinging(decision.entryId)
                is ChatCallLedger.StopDecision.Ignore -> Action.Ignore(decision.entryId)
            }
            else -> Action.Ignore(entryId)
        }
    }
}
