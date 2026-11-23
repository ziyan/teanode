package com.teanode.app.calls

import com.teanode.kit.calls.ChatCallAudioRoute
import com.teanode.kit.calls.ChatCallView

/** How the path to the media server stands. */
enum class CallConnectionState { Connecting, Connected, Disconnected, Failed }

/** What the call screen shows of how the call stands. */
enum class CallPhase {
    /** Asking the server, or finding the path to the media server. */
    Connecting,
    Connected,

    /** The path was lost and is looked for again. */
    Reconnecting,
}

/** CallVideoTrack is a picture a tile draws; the WebRTC one is [WebRtcVideoTrack], and the demo has none. */
interface CallVideoTrack {
    val trackId: String
}

/**
 * CallSessionState is the call this phone is on, or is going onto: what the call screen draws. It is the Android side
 * of the iPhone app's `ChatCallSession`.
 */
data class CallSessionState(
    /** The ledger's name for the call. */
    val entryId: String,
    val accountKey: String,
    val conversationId: String,
    /** The chat's name as the person sees it, or the caller's. */
    val title: String,
    val isOutgoing: Boolean,
    /** The server's id of the call; null until it has made it. */
    val callId: String? = null,
    /** The call as the server last told it. */
    val call: ChatCallView? = null,
    val phase: CallPhase = CallPhase.Connecting,
    /** The person's own user id, which of the call's members is them. */
    val ownUserId: String = "",
    val isMuted: Boolean = false,
    /** Who a call this phone starts rings: null for everybody else in the chat. */
    val ringUserIds: List<String>? = null,
    /** Who the server says is speaking, and the last of the others who did, so the person's own voice does not take the featured picture away. */
    val speakingUserId: String = "",
    val lastOtherSpeakerId: String = "",
    /** Whose picture the person chose to feature, empty to follow whoever speaks. */
    val featuredUserId: String = "",
    /** Whether the call screen is put away to the bar at the top of the app, so the person can use the rest of it while on the call. */
    val isMinimized: Boolean = false,
    val isCameraOn: Boolean = false,
    val audioRoute: ChatCallAudioRoute = ChatCallAudioRoute.Phone,
    val usesFrontCamera: Boolean = true,
    /** The pictures of the others and the person's own, by whom they are of. */
    val remoteVideoTracks: Map<String, CallVideoTrack> = emptyMap(),
    val localVideoTrack: CallVideoTrack? = null,
)

/** CallProblem is a call that could not go on, in words for the person. */
enum class CallProblem {
    MicrophoneRefused,
    CameraRefused,
    AlreadyOnCall,
    NotSignedIn,
    CouldNotStart,
    LostConnection,
}

/** IncomingRing is a call ringing this phone. */
data class IncomingRing(
    val entryId: String,
    val callId: String,
    val conversationId: String,
    val callerName: String,
    val accountKey: String,
)
