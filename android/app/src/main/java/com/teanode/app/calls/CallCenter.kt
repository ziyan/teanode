package com.teanode.app.calls

import com.teanode.kit.Connection
import com.teanode.kit.accessSession
import com.teanode.kit.calls.ChatCallAudioRoute
import com.teanode.kit.calls.ChatCallEndReason
import com.teanode.kit.calls.ChatCallFeed
import com.teanode.kit.calls.ChatCallLedger
import com.teanode.kit.calls.ChatCallSignaling
import com.teanode.kit.calls.ChatCallSignalingCommand
import com.teanode.kit.calls.ChatCallView
import com.teanode.kit.calls.IncomingCallPush
import com.teanode.kit.calls.answerChatCallMedia
import com.teanode.kit.calls.askChatCallTranscription
import com.teanode.kit.calls.declineChatCall
import com.teanode.kit.calls.joinChatCall
import com.teanode.kit.calls.joinChatCallMedia
import com.teanode.kit.calls.leaveChatCall
import com.teanode.kit.calls.listGoingChatCalls
import com.teanode.kit.calls.readGoingChatCall
import com.teanode.kit.calls.setChatCallFeatured
import com.teanode.kit.calls.setChatCallMuted
import com.teanode.kit.calls.setChatCallVideo
import com.teanode.kit.calls.startChatCall
import com.teanode.kit.notifications.PayloadKey
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/** CallEnvironment is what the calls need of the app around them. */
interface CallEnvironment {
    /** The account in use. */
    val activeAccountKey: String?

    /** The connection of an account on the phone, the one in use or one kept for it; null when the account is gone. */
    fun connection(accountKey: String): Connection?

    /** The id this phone has on an account's server, named when answering or declining so that it is not told to stop ringing. */
    fun phoneId(accountKey: String): String?

    fun hasMicrophone(): Boolean

    fun hasCamera(): Boolean

    /** Whether the app is in front of the person. */
    fun isAppInFront(): Boolean

    /** Says what went wrong in the person's language. */
    fun describe(error: Throwable): String

    /** The words of a call that could not go on. */
    fun words(problem: CallProblem): String

    /** The service that keeps the app alive and shows the ring or the ongoing call is started or stopped by what the center holds. */
    fun callStateChanged()
}

/**
 * CallCenter is the calls with the people of a chat on this phone. It is the Android side of the iPhone app's
 * `ChatCallManager`, and brings three things together:
 *
 * - The system's call surfaces, which make the call the phone's: the full-screen incoming call over the lock screen,
 *   the ongoing call notification, and the audio route (`CallService`, `IncomingCallActivity`, `CallAudioRouter`).
 *   Every call, rung or started, is one the [ChatCallLedger] knows by an id this keeps with the server's id of the
 *   call, and the ledger decides what to tell of each ring and each change.
 * - Firebase, through `PushHandler`: a high priority message wakes the app, which shows the ring at once
 *   ([receiveRing]).
 * - WebRTC, through [CallMedia]: the audio and video, in the order `ChatCallSignaling` gives.
 *
 * It follows ChatCallEvents while the app is signed in, for the calls ringing the person, the banner of a chat with a
 * call going, and the line of a call in a chat. Everything here runs on the main thread.
 */
class CallCenter(
    private val environment: CallEnvironment,
    private val mediaFactory: CallMediaFactory,
    private val audio: CallAudio,
    private val system: CallSystem? = null,
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val ledger = ChatCallLedger()

    private val currentSession = MutableStateFlow<CallSessionState?>(null)

    /** The call this phone is on or going onto. */
    val session: StateFlow<CallSessionState?> = currentSession.asStateFlow()

    private val views = MutableStateFlow<Map<String, ChatCallView>>(emptyMap())

    /** The newest view of each call heard of, by call id: a call's line in a chat is drawn from it. */
    val callViews: StateFlow<Map<String, ChatCallView>> = views.asStateFlow()

    private val goingIds = MutableStateFlow<Map<String, String>>(emptyMap())

    /** The call going in each chat, by the person's conversation for it. */
    val goingCallIds: StateFlow<Map<String, String>> = goingIds.asStateFlow()

    private val ringingCalls = MutableStateFlow<List<IncomingRing>>(emptyList())

    /** The calls ringing this phone. */
    val ringing: StateFlow<List<IncomingRing>> = ringingCalls.asStateFlow()

    /** The routes the person can send a call's sound to now. */
    val audioRoutes: StateFlow<List<ChatCallAudioRoute>> get() = audio.availableRoutes

    private val voiceCall = MutableStateFlow<AgentVoiceCall?>(null)

    /** The call with the agent, while the phone is on one: it keeps the app alive as a call with people does. */
    val agentVoice: StateFlow<AgentVoiceCall?> = voiceCall.asStateFlow()

    /** Takes a call with the agent started by the chats part; the call service keeps the microphone going while it lasts. */
    fun beginAgentVoice(call: AgentVoiceCall) {
        voiceCall.value?.hangUp()
        voiceCall.value = call
        environment.callStateChanged()
        scope.launch {
            call.callState.first { state -> state == com.teanode.kit.calls.VoiceCallState.Ended }
            if (voiceCall.value === call) voiceCall.value = null
        }
    }

    private val problemEvents = MutableSharedFlow<String>(extraBufferCapacity = 8)

    /** What went wrong, for a snackbar. */
    val problems: SharedFlow<String> = problemEvents.asSharedFlow()

    /** What a call on this phone holds besides what the screen draws. */
    private class ActiveCall(val connection: Connection) {
        var media: CallMedia? = null
        val signaling = ChatCallSignaling()
        var signalsJob: Job? = null
        var routeJob: Job? = null

        /** Set once the call is being ended, so that what fails because of it is not told as a problem. */
        var isEnding = false
        var hasConnectedOnce = false
    }

    private var active: ActiveCall? = null

    /** Whether this phone is on the call (or going onto it). */
    fun isOnCall(callId: String): Boolean = currentSession.value?.callId == callId

    private fun problem(problem: CallProblem) {
        problemEvents.tryEmit(environment.words(problem))
    }

    private fun updateSession(change: (CallSessionState) -> CallSessionState) {
        currentSession.update { state -> state?.let(change) }
    }

    private fun publishRinging() {
        ringingCalls.value = ledger.ringingEntries.map { entry ->
            IncomingRing(entry.entryId, entry.callId.orEmpty(), entry.conversationId, entry.title, entry.accountKey)
        }
        ringingCalls.value.forEach { ring -> system?.hostRing(ring, this) }
        environment.callStateChanged()
    }

    // MARK: A ring from a push

    /**
     * A push of a call ringing, or of one that stopped ringing, arrived and was opened. Every one is answered here: a
     * call that rings, or nothing at all when the call is not known or is known already.
     */
    fun receiveRing(opened: PayloadKey.Opened) {
        scope.launch {
            when (IncomingCallPush.decide(opened.content, opened.accountKey, ledger)) {
                is IncomingCallPush.Action.Ring -> publishRinging()
                is IncomingCallPush.Action.StopRinging -> publishRinging()
                is IncomingCallPush.Action.Ignore -> Unit
            }
        }
    }

    // MARK: Following the server

    /**
     * Follows ChatCallEvents while the app is signed in, reading the calls going again whenever the feed has to be
     * opened anew. The kit's feed ends with an error when the connection drops.
     */
    suspend fun follow(connection: Connection) {
        var delayMS = FOLLOW_DELAY_FIRST_MS
        while (currentCoroutineContextIsActive()) {
            readCallsGoing(connection)
            try {
                ChatCallFeed.events(connection).collect { view ->
                    note(view)
                    delayMS = FOLLOW_DELAY_FIRST_MS
                }
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (_: Exception) {
                // The feed dropped; what was missed is read again above.
            }
            delay(delayMS)
            delayMS = minOf(delayMS * 2, FOLLOW_DELAY_MOST_MS)
        }
    }

    private suspend fun currentCoroutineContextIsActive(): Boolean = kotlin.coroutines.coroutineContext.isActive

    /**
     * Reads the calls going the person has a part in: the rings that are over are ended, and a call ringing them that
     * this phone does not know rings, when the app is in front (a phone that registered no ring token is rung this way,
     * and a ring the push missed).
     */
    private suspend fun readCallsGoing(connection: Connection) {
        val going = try {
            connection.listGoingChatCalls()
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (_: Exception) {
            return
        }
        for (view in going.filter { call -> call.isGoing }) {
            views.update { known -> known + (view.id to view) }
            goingIds.update { known -> known + (view.conversationId to view.id) }
            if (currentSession.value?.callId == view.id) updateSession { state -> state.copy(call = view) }
        }
        ledger.reconcile(going).forEach { reaction -> handle(reaction) }
        publishRinging()
        if (!environment.isAppInFront() || currentSession.value != null) return
        for (view in going.filter { call -> call.isGoing && call.askerRingState == "ringing" }) {
            val callerName = view.startedByName.ifEmpty { view.chatTitle }
            if (ledger.ringIncoming(view.id, view.conversationId, callerName, connection.accountKey) is ChatCallLedger.RingDecision.Ring) publishRinging()
        }
    }

    /** Reads the call going in a chat that has just been opened, for its banner; later changes come with ChatCallEvents. */
    suspend fun readGoingCall(conversationId: String, connection: Connection) {
        // Nothing going, or the server could not be asked: the banner is the one the events built, if any.
        val found = try {
            connection.readGoingChatCall(conversationId)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (_: Exception) {
            null
        } ?: return
        note(found)
    }

    /** A call changed, as the person sees it now. */
    fun note(view: ChatCallView) {
        views.update { known -> known + (view.id to view) }
        if (view.isGoing) {
            goingIds.update { known -> known + (view.conversationId to view.id) }
        } else if (goingIds.value[view.conversationId] == view.id) {
            goingIds.update { known -> known - view.conversationId }
        }
        if (currentSession.value?.callId == view.id) updateSession { state -> state.copy(call = view) }
        handle(ledger.apply(view))
        publishRinging()
    }

    /** What the incoming call and the call screen do for a change the ledger decided on. */
    private fun handle(reaction: ChatCallLedger.Reaction) {
        if (reaction !is ChatCallLedger.Reaction.End) return
        val state = currentSession.value
        if (state != null && state.entryId == reaction.entryId) teardown()
    }

    // MARK: Starting and joining

    /**
     * Calls the people of a chat, with the camera on or off, ringing everybody else in it or only the people in
     * [userIds]: starts a call, or joins the one going in it.
     */
    fun startCall(conversationId: String, title: String, isVideoOn: Boolean = false, userIds: List<String>? = null) {
        scope.launch {
            val callId = goingIds.value[conversationId]
            if (callId != null) {
                join(callId, conversationId, title)
                return@launch
            }
            if (isVideoOn && !environment.hasCamera()) {
                problem(CallProblem.CameraRefused)
                return@launch
            }
            place(conversationId, title, existingCallId = null, isVideoOn = isVideoOn, userIds = userIds)
        }
    }

    /** Joins a call going: the one ringing this phone is answered, one not ringing is joined as a call this phone places. */
    fun join(callId: String, conversationId: String, title: String) {
        scope.launch {
            val ringingEntry = ledger.entryOfCall(callId)
            if (ringingEntry != null && ringingEntry.phase == ChatCallLedger.Phase.Ringing) {
                answerRing(ringingEntry.entryId)
                return@launch
            }
            // The call the phone is on already: its screen comes back.
            if (currentSession.value?.callId == callId) {
                updateSession { state -> state.copy(isMinimized = false) }
                return@launch
            }
            place(conversationId, title, existingCallId = callId)
        }
    }

    private fun place(conversationId: String, title: String, existingCallId: String?, isVideoOn: Boolean = false, userIds: List<String>? = null) {
        if (currentSession.value != null || ledger.activeEntry != null) {
            problem(CallProblem.AlreadyOnCall)
            return
        }
        val accountKey = environment.activeAccountKey
        val connection = accountKey?.let { key -> environment.connection(key) }
        if (accountKey == null || connection == null) {
            problem(CallProblem.NotSignedIn)
            return
        }
        val entry = ledger.place(conversationId, title, accountKey)
        if (existingCallId != null) ledger.attach(existingCallId, entry.entryId)?.let { publishRinging() }
        active = ActiveCall(connection)
        currentSession.value = CallSessionState(
            entryId = entry.entryId, accountKey = accountKey, conversationId = conversationId, title = title, isOutgoing = true,
            callId = existingCallId, isCameraOn = isVideoOn, ringUserIds = userIds,
        )
        environment.callStateChanged()
        currentSession.value?.let { placed -> system?.hostCall(placed, this) }
        scope.launch { runPlaced(entry.entryId) }
    }

    /** The server's side of a call this phone places or joins. */
    private suspend fun runPlaced(entryId: String) {
        val call = active ?: return
        val connection = call.connection
        try {
            if (!environment.hasMicrophone()) throw CallRefusedException(CallProblem.MicrophoneRefused)
            readOwnUserId(connection)
            val state = currentSession.value ?: return
            val agentPhoneId = environment.phoneId(state.accountKey)
            var view: ChatCallView
            if (state.callId != null) {
                view = connection.joinChatCall(state.callId, agentPhoneId)
            } else {
                view = connection.startChatCall(state.conversationId, state.isCameraOn, state.ringUserIds)
                // A call going in the chat is returned to be joined.
                if (!view.isAskerOn) view = connection.joinChatCall(view.id, agentPhoneId)
            }
            updateSession { current -> current.copy(callId = view.id) }
            note(view)
            if (ledger.attach(view.id, entryId) != null) publishRinging()
            if (call.isEnding) {
                runCatching { connection.leaveChatCall(view.id) }
                return
            }
            startMedia(entryId)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (error: Exception) {
            fail(error)
        }
    }

    /** The server's side of a ringing call the person answered. */
    private suspend fun runAnswered(entryId: String) {
        val call = active ?: return
        val connection = call.connection
        val state = currentSession.value ?: return
        val callId = state.callId ?: return
        try {
            if (!environment.hasMicrophone()) throw CallRefusedException(CallProblem.MicrophoneRefused)
            readOwnUserId(connection)
            val view = connection.joinChatCall(callId, environment.phoneId(state.accountKey))
            note(view)
            if (call.isEnding) {
                runCatching { connection.leaveChatCall(callId) }
                return
            }
            startMedia(entryId)
        } catch (cancellation: CancellationException) {
            throw cancellation
        } catch (error: Exception) {
            fail(error)
        }
    }

    private suspend fun readOwnUserId(connection: Connection) {
        if (currentSession.value?.ownUserId.isNullOrEmpty()) {
            connection.accessSession().userId?.let { userId -> updateSession { state -> state.copy(ownUserId = userId) } }
        }
    }

    /** Answers a call ringing this phone. */
    fun answerRing(entryId: String) {
        val entry = ledger.answer(entryId) ?: return
        val connection = environment.connection(entry.accountKey)
        if (connection == null) {
            ledger.remove(entryId)
            publishRinging()
            problem(CallProblem.NotSignedIn)
            return
        }
        // The phone is on one call: the first is left.
        if (currentSession.value != null && currentSession.value?.entryId != entryId) {
            leaveOnServer()
            currentSession.value?.entryId?.let { existing -> ledger.remove(existing) }
            teardown()
        }
        active = ActiveCall(connection)
        currentSession.value = CallSessionState(
            entryId = entry.entryId, accountKey = entry.accountKey, conversationId = entry.conversationId, title = entry.title,
            isOutgoing = false, callId = entry.callId,
        )
        publishRinging()
        currentSession.value?.let { answered -> system?.hostCall(answered, this) }
        scope.launch { runAnswered(entryId) }
    }

    /** Says no to a call ringing this phone. */
    fun declineRing(entryId: String) {
        val removed = ledger.remove(entryId) ?: return
        publishRinging()
        val callId = removed.callId ?: return
        val connection = environment.connection(removed.accountKey) ?: return
        scope.launch { runCatching { connection.declineChatCall(callId, environment.phoneId(removed.accountKey)) } }
    }

    // MARK: The media

    /** Opens the signals, then joins the call's media: the order of docs/subsystems/chat-calls.md, "A device on a call". */
    private suspend fun startMedia(entryId: String) {
        val call = active ?: return
        val state = currentSession.value ?: return
        val callId = state.callId ?: return
        val media = mediaFactory.create()
        call.media = media
        media.setMuted(state.isMuted)
        media.listener = object : CallMediaListener {
            override fun onConnectionState(state: CallConnectionState) {
                scope.launch { mediaStateChanged(state, entryId) }
            }

            override fun onRemoteVideoChange(tracks: Map<String, CallVideoTrack>) {
                scope.launch { updateSession { current -> current.copy(remoteVideoTracks = tracks) } }
            }
        }
        audio.start()
        call.routeJob = scope.launch { audio.route.collect { route -> updateSession { current -> current.copy(audioRoute = route) } } }
        if (state.isCameraOn) {
            if (media.setCamera(isOn = true, usesFrontCamera = state.usesFrontCamera)) {
                updateSession { current -> current.copy(localVideoTrack = media.localVideoTrack) }
                // A picture is looked at, not held to the ear.
                if (ChatCallAudioRoute.shouldMoveToSpeaker(audio.route.value)) setAudioRoute(ChatCallAudioRoute.Speaker)
            } else {
                updateSession { current -> current.copy(isCameraOn = false) }
                problem(CallProblem.CameraRefused)
            }
        }
        openSignals(call, callId)
        perform(call.signaling.start(), call, callId)
    }

    /** Follows the offers of the media server for as long as the call goes, opening the feed again when it drops: the server keeps what it said meanwhile. */
    private fun openSignals(call: ActiveCall, callId: String) {
        call.signalsJob?.cancel()
        call.signalsJob = scope.launch {
            var delayMS = SIGNALS_DELAY_FIRST_MS
            while (isActive) {
                try {
                    ChatCallFeed.signals(call.connection, callId).collect { signal ->
                        delayMS = SIGNALS_DELAY_FIRST_MS
                        val speaking = signal.speakingUserId
                        if (!speaking.isNullOrEmpty()) {
                            updateSession { current ->
                                current.copy(
                                    speakingUserId = speaking,
                                    lastOtherSpeakerId = if (speaking != current.ownUserId) speaking else current.lastOtherSpeakerId,
                                )
                            }
                        } else {
                            perform(call.signaling.receive(signal.offerSdp), call, callId)
                        }
                    }
                } catch (cancellation: CancellationException) {
                    throw cancellation
                } catch (_: Exception) {
                    // Opened again below.
                }
                delay(delayMS)
                delayMS = minOf(delayMS * 2, SIGNALS_DELAY_MOST_MS)
            }
        }
    }

    /** Takes the steps the signaling asks for, one after the other: the join, then each offer of the server's, answered in order. A step that fails ends the call. */
    private suspend fun perform(commands: List<ChatCallSignalingCommand>, call: ActiveCall, callId: String) {
        val pending = ArrayDeque(commands)
        while (pending.isNotEmpty()) {
            val command = pending.removeFirst()
            val media = call.media
            if (media == null || call.isEnding) return
            try {
                when (command) {
                    ChatCallSignalingCommand.Join -> {
                        val offer = media.makeJoinOffer()
                        val answer = call.connection.joinChatCallMedia(callId, offer)
                        media.setJoinAnswer(answer)
                        pending.addAll(call.signaling.joinAnswered())
                    }
                    is ChatCallSignalingCommand.Answer -> {
                        val answer = media.answer(command.offerSdp)
                        call.connection.answerChatCallMedia(callId, answer)
                        pending.addAll(call.signaling.answerSent())
                    }
                }
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (error: Exception) {
                call.signaling.fail()
                fail(error)
                return
            }
        }
    }

    private fun mediaStateChanged(state: CallConnectionState, entryId: String) {
        val call = active
        if (call == null || call.isEnding || currentSession.value?.entryId != entryId) return
        when (state) {
            CallConnectionState.Connecting -> Unit
            CallConnectionState.Connected -> {
                call.hasConnectedOnce = true
                updateSession { current -> current.copy(phase = CallPhase.Connected) }
                ledger.connected(entryId)
            }
            CallConnectionState.Disconnected -> updateSession { current -> current.copy(phase = CallPhase.Reconnecting) }
            CallConnectionState.Failed -> {
                problem(CallProblem.LostConnection)
                endWith(ChatCallEndReason.Failed)
            }
        }
    }

    // MARK: What the person does on the call

    /** Mutes or unmutes the microphone and tells the others, to show it beside the name. The sound stops either way, so a failure to tell them is not the person's to handle. */
    fun setMuted(isMuted: Boolean) {
        val call = active ?: return
        val state = currentSession.value ?: return
        updateSession { current -> current.copy(isMuted = isMuted) }
        call.media?.setMuted(isMuted)
        val callId = state.callId ?: return
        if (call.media == null) return
        scope.launch { runCatching { call.connection.setChatCallMuted(callId, isMuted) }.getOrNull()?.let { view -> note(view) } }
        environment.callStateChanged()
    }

    /** Features a person's picture, or null to follow whoever speaks, and asks the server to send that one whole. The picture is featured here at once; a failure to tell the server leaves it small. */
    fun setFeatured(userId: String?) {
        val call = active ?: return
        updateSession { current -> current.copy(featuredUserId = userId.orEmpty()) }
        val callId = currentSession.value?.callId ?: return
        if (call.media == null) return
        scope.launch { runCatching { call.connection.setChatCallFeatured(callId, userId) } }
    }

    /** Turns the camera on or off, and tells the others. */
    fun setCamera(isOn: Boolean) {
        val call = active ?: return
        scope.launch {
            if (isOn && !environment.hasCamera()) {
                problem(CallProblem.CameraRefused)
                return@launch
            }
            updateSession { current -> current.copy(isCameraOn = isOn) }
            val media = call.media
            if (media != null) {
                val state = currentSession.value ?: return@launch
                if (!media.setCamera(isOn, state.usesFrontCamera)) {
                    updateSession { current -> current.copy(isCameraOn = false, localVideoTrack = null) }
                    problem(CallProblem.CameraRefused)
                    return@launch
                }
                updateSession { current -> current.copy(localVideoTrack = media.localVideoTrack) }
            }
            // A picture is looked at, not held to the ear.
            if (isOn && ChatCallAudioRoute.shouldMoveToSpeaker(audio.route.value)) setAudioRoute(ChatCallAudioRoute.Speaker)
            val callId = currentSession.value?.callId ?: return@launch
            try {
                note(call.connection.setChatCallVideo(callId, isOn))
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (error: Exception) {
                problemEvents.tryEmit(environment.describe(error))
            }
        }
    }

    /** Changes to the phone's other camera. */
    fun flipCamera() {
        val call = active ?: return
        val state = currentSession.value ?: return
        val media = call.media ?: return
        if (!state.isCameraOn) return
        scope.launch {
            // Only a camera that opened is the one in use: a phone whose other camera will not open stays on this one.
            if (media.flipCamera(!state.usesFrontCamera)) updateSession { current -> current.copy(usesFrontCamera = !current.usesFrontCamera) }
        }
    }

    /** Plays the call on the earpiece, the speaker, or whatever is connected. */
    fun setAudioRoute(route: ChatCallAudioRoute) {
        audio.choose(route)
        updateSession { current -> current.copy(audioRoute = route) }
    }

    /** Puts the call screen away to the bar, or brings it back. */
    fun setMinimized(isMinimized: Boolean) {
        updateSession { current -> current.copy(isMinimized = isMinimized) }
    }

    /** Asks for the call to be transcribed. Everybody on it is told. */
    fun askTranscription() {
        val call = active ?: return
        val callId = currentSession.value?.callId ?: return
        scope.launch {
            try {
                note(call.connection.askChatCallTranscription(callId))
            } catch (cancellation: CancellationException) {
                throw cancellation
            } catch (error: Exception) {
                problemEvents.tryEmit(environment.describe(error))
            }
        }
    }

    // MARK: Ending

    /** Hangs up. */
    fun hangUp() {
        voiceCall.value?.let { call ->
            call.hangUp()
            voiceCall.value = null
        }
        val state = currentSession.value ?: return
        leaveOnServer()
        ledger.remove(state.entryId)
        teardown()
    }

    /** Ends a call that went wrong. */
    private fun endWith(reason: ChatCallEndReason) {
        val state = currentSession.value ?: return
        if (active?.isEnding == true) return
        leaveOnServer()
        ledger.remove(state.entryId)
        teardown()
        if (reason == ChatCallEndReason.Failed) environment.callStateChanged()
    }

    private fun fail(error: Throwable) {
        if (active?.isEnding == true) return
        problemEvents.tryEmit(if (error is CallRefusedException) environment.words(error.problem) else environment.describe(error))
        endWith(ChatCallEndReason.Failed)
    }

    private fun leaveOnServer() {
        val call = active ?: return
        val callId = currentSession.value?.callId ?: return
        scope.launch { runCatching { call.connection.leaveChatCall(callId) } }
    }

    /** Lets go of the connection, the camera, the audio and the call screen. */
    private fun teardown() {
        active?.let { call ->
            call.isEnding = true
            call.signalsJob?.cancel()
            call.routeJob?.cancel()
            call.media?.close()
            call.media = null
        }
        active = null
        audio.stop()
        currentSession.value = null
        publishRinging()
    }

    private class CallRefusedException(val problem: CallProblem) : Exception()

    private companion object {
        const val FOLLOW_DELAY_FIRST_MS = 1_000L
        const val FOLLOW_DELAY_MOST_MS = 30_000L
        const val SIGNALS_DELAY_FIRST_MS = 500L
        const val SIGNALS_DELAY_MOST_MS = 5_000L
    }
}

/** CallAudio is the phone's audio for a call: taken when the media starts, given back when the call ends. */
interface CallAudio {
    val route: StateFlow<ChatCallAudioRoute>
    val availableRoutes: StateFlow<List<ChatCallAudioRoute>>
    fun start()
    fun stop()
    fun choose(route: ChatCallAudioRoute)
}
