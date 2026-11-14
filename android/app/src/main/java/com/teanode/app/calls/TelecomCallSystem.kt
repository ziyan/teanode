package com.teanode.app.calls

import android.content.Context
import android.net.Uri
import android.telecom.DisconnectCause
import androidx.core.telecom.CallAttributesCompat
import androidx.core.telecom.CallControlScope
import androidx.core.telecom.CallsManager
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch

/** CallSystem tells the phone's own call machinery about each call, as CallKit does on the iPhone. */
interface CallSystem {
    /** A call rings this phone. [center] is told when a headset, a car or a watch answers or ends it. */
    fun hostRing(ring: IncomingRing, center: CallCenter)

    /** A call this phone placed or answered from the app. */
    fun hostCall(state: CallSessionState, center: CallCenter)
}

/**
 * TelecomCallSystem hosts each call as a self-managed Telecom call (androidx.core:core-telecom). The system then offers
 * it to Bluetooth headsets, cars and watches (answer, hang up, mute), keeps other calls from fighting it for the
 * microphone, and treats it as a call in its own state, so that it survives as a system call does. The app still draws
 * its screens and notifications; Telecom is told when a call rings, goes on and ends, and tells the app what a
 * headset or a watch does.
 */
class TelecomCallSystem(context: Context) : CallSystem {
    private val applicationContext = context.applicationContext
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var manager: CallsManager? = null

    /** The rings hosted as system calls, so that answering one does not add the call twice. */
    private val hostedRingEntryIds = mutableSetOf<String>()

    private fun callsManager(): CallsManager? {
        manager?.let { return it }
        return runCatching {
            CallsManager(applicationContext).also { made ->
                made.registerAppWithTelecom(CallsManager.CAPABILITY_BASELINE or CallsManager.CAPABILITY_SUPPORTS_VIDEO_CALLING)
                manager = made
            }
        }.getOrNull()
    }

    private fun attributes(name: String, conversationId: String, isIncoming: Boolean, isVideoOn: Boolean) = CallAttributesCompat(
        displayName = name,
        address = Uri.parse("teanode:chat:$conversationId"),
        direction = if (isIncoming) CallAttributesCompat.DIRECTION_INCOMING else CallAttributesCompat.DIRECTION_OUTGOING,
        callType = if (isVideoOn) CallAttributesCompat.CALL_TYPE_VIDEO_CALL else CallAttributesCompat.CALL_TYPE_AUDIO_CALL,
        callCapabilities = CallAttributesCompat.SUPPORTS_SET_INACTIVE,
    )

    override fun hostRing(ring: IncomingRing, center: CallCenter) {
        val telecom = callsManager() ?: return
        if (!hostedRingEntryIds.add(ring.entryId)) return
        scope.launch {
            try {
                telecom.addCall(
                    attributes(ring.callerName, ring.conversationId, isIncoming = true, isVideoOn = false),
                    onAnswer = { center.answerRing(ring.entryId) },
                    onDisconnect = {
                        if (center.ringing.value.any { candidate -> candidate.entryId == ring.entryId }) center.declineRing(ring.entryId) else center.hangUp()
                    },
                    onSetActive = {},
                    onSetInactive = {},
                ) {
                    launch {
                        // The ring is over when it is answered, declined, or stopped by the server.
                        center.ringing.first { rings -> rings.none { candidate -> candidate.entryId == ring.entryId } }
                        val state = center.session.value
                        if (state != null && state.entryId == ring.entryId) {
                            answer(CallAttributesCompat.CALL_TYPE_AUDIO_CALL)
                            setActive()
                            live(state, center)
                        } else {
                            disconnect(DisconnectCause(DisconnectCause.REJECTED))
                        }
                    }
                }
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (_: Exception) {
                // The system would not take the call: the app's own notifications still ring it.
            } finally {
                hostedRingEntryIds.remove(ring.entryId)
            }
        }
    }

    override fun hostCall(state: CallSessionState, center: CallCenter) {
        // A ring that was answered goes on as the call the system already has.
        if (state.entryId in hostedRingEntryIds) return
        val telecom = callsManager() ?: return
        scope.launch {
            try {
                telecom.addCall(
                    attributes(state.title, state.conversationId, isIncoming = false, isVideoOn = state.isCameraOn),
                    onAnswer = {},
                    onDisconnect = { center.hangUp() },
                    onSetActive = {},
                    onSetInactive = {},
                ) {
                    launch {
                        setActive()
                        live(state, center)
                    }
                }
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (_: Exception) {
                // Not hosted by the system; the call goes on all the same.
            }
        }
    }

    /** While the call goes on: a mute from a headset or a watch reaches the call, and the end of the call ends the system's call. */
    private suspend fun CallControlScope.live(state: CallSessionState, center: CallCenter) {
        scope.launch {
            isMuted.collect { isMutedBySystem ->
                if (isMutedBySystem != (center.session.value?.isMuted ?: isMutedBySystem)) center.setMuted(isMutedBySystem)
            }
        }
        center.session.first { current -> current?.entryId != state.entryId }
        disconnect(DisconnectCause(DisconnectCause.LOCAL))
    }
}
