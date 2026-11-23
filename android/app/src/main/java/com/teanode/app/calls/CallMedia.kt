package com.teanode.app.calls

/** CallMediaListener is told what the path to the media server does. */
interface CallMediaListener {
    fun onConnectionState(state: CallConnectionState)

    /** The others' pictures changed: each by the user id of whoever sends it. */
    fun onRemoteVideoChange(tracks: Map<String, CallVideoTrack>)
}

/**
 * CallMedia is a phone's peer connection to the call's media server, as docs/subsystems/chat-calls.md describes it:
 * one connection with no ICE servers (the server is an ICE-lite agent that answers whatever reaches it), the
 * microphone, and a video transceiver whose track is the camera when it is on and none when it is off. The order of
 * what is said to the server is `ChatCallSignaling`'s; this does each step.
 */
interface CallMedia {
    var listener: CallMediaListener?

    /** The camera's track while the camera is on; the preview draws it. */
    val localVideoTrack: CallVideoTrack?

    /** The device's offer for JoinChatCallMedia, with its addresses in it. */
    suspend fun makeJoinOffer(): String

    /** Sets the server's answer to the join. */
    suspend fun setJoinAnswer(answerSdp: String)

    /** Answers one of the server's offers: sets it, makes the answer and sets that, and returns it to be sent with AnswerChatCallMedia. */
    suspend fun answer(offerSdp: String): String

    /** Stops or starts sending the microphone's sound. */
    fun setMuted(isMuted: Boolean)

    /** Turns the camera on or off. Returns false when the phone has no such camera or it gave no picture. */
    suspend fun setCamera(isOn: Boolean, usesFrontCamera: Boolean): Boolean

    /** Changes to the other camera while the camera is on. Returns false when the other camera would not open. */
    suspend fun flipCamera(usesFrontCamera: Boolean): Boolean

    /** Ends the connection and lets go of the camera and the microphone. */
    fun close()
}

/** CallMediaFactory makes the media of a call; the app's is WebRTC's, a test's is its own. */
fun interface CallMediaFactory {
    fun create(): CallMedia
}
