package com.teanode.app.calls

import android.content.Context
import android.util.Log
import com.teanode.kit.calls.ChatCallSDP
import kotlinx.coroutines.CancellableContinuation
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.webrtc.AudioSource
import org.webrtc.AudioTrack
import org.webrtc.Camera2Enumerator
import org.webrtc.CameraVideoCapturer
import org.webrtc.DataChannel
import org.webrtc.DefaultVideoDecoderFactory
import org.webrtc.DefaultVideoEncoderFactory
import org.webrtc.EglBase
import org.webrtc.IceCandidate
import org.webrtc.MediaConstraints
import org.webrtc.MediaStream
import org.webrtc.MediaStreamTrack
import org.webrtc.PeerConnection
import org.webrtc.PeerConnectionFactory
import org.webrtc.RtpParameters
import org.webrtc.RtpTransceiver
import org.webrtc.SdpObserver
import org.webrtc.SessionDescription
import org.webrtc.SurfaceTextureHelper
import org.webrtc.VideoCapturer
import org.webrtc.VideoSource
import org.webrtc.VideoTrack
import org.webrtc.audio.JavaAudioDeviceModule
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/** WebRtcVideoTrack is a picture WebRTC delivers, drawn by [CallVideoView]. */
class WebRtcVideoTrack(val track: VideoTrack) : CallVideoTrack {
    override val trackId: String get() = track.id()
}

/** The one WebRTC factory of the process, which owns the audio device, with the phone's own echo cancelling and noise suppression on the microphone. */
object WebRtcFactory {
    val eglBase: EglBase by lazy { EglBase.create() }
    private var factory: PeerConnectionFactory? = null

    @Synchronized
    fun get(context: Context): PeerConnectionFactory {
        factory?.let { existing -> return existing }
        PeerConnectionFactory.initialize(PeerConnectionFactory.InitializationOptions.builder(context.applicationContext).createInitializationOptions())
        val audioDevice = JavaAudioDeviceModule.builder(context.applicationContext)
            .setUseHardwareAcousticEchoCanceler(true)
            .setUseHardwareNoiseSuppressor(true)
            .createAudioDeviceModule()
        return PeerConnectionFactory.builder()
            .setAudioDeviceModule(audioDevice)
            .setVideoEncoderFactory(DefaultVideoEncoderFactory(eglBase.eglBaseContext, true, true))
            .setVideoDecoderFactory(DefaultVideoDecoderFactory(eglBase.eglBaseContext))
            .createPeerConnectionFactory()
            .also { created -> factory = created }
    }
}

/** An observer that says nothing, for the parts of a connection's events this call does not need. */
private open class QuietObserver : PeerConnection.Observer {
    override fun onSignalingChange(state: PeerConnection.SignalingState?) {}
    override fun onIceConnectionChange(state: PeerConnection.IceConnectionState?) {}
    override fun onIceConnectionReceivingChange(isReceiving: Boolean) {}
    override fun onIceGatheringChange(state: PeerConnection.IceGatheringState?) {}
    override fun onIceCandidate(candidate: IceCandidate?) {}
    override fun onIceCandidatesRemoved(candidates: Array<out IceCandidate>?) {}
    override fun onAddStream(stream: MediaStream?) {}
    override fun onRemoveStream(stream: MediaStream?) {}
    override fun onDataChannel(channel: DataChannel?) {}
    override fun onRenegotiationNeeded() {}
}

/**
 * WebRtcCallMedia is [CallMedia] over WebRTC (io.getstream:stream-webrtc-android). Its parts follow the iPhone app's
 * `ChatCallMedia`: one connection with no ICE servers and its local addresses gathered once, the microphone, and a
 * video transceiver sending the camera in two sizes named as the server knows them (`q`, a third of the picture, and
 * `f`, the whole of it; internal/call/video.go).
 */
class WebRtcCallMedia(private val context: Context) : CallMedia {
    override var listener: CallMediaListener? = null

    private val factory: PeerConnectionFactory = WebRtcFactory.get(context)
    private var connection: PeerConnection? = null
    private var audioSource: AudioSource? = null
    private var audioTrack: AudioTrack? = null
    private var videoSource: VideoSource? = null
    private var cameraTrack: VideoTrack? = null
    private var videoTransceiver: RtpTransceiver? = null
    private var capturer: VideoCapturer? = null
    private var textureHelper: SurfaceTextureHelper? = null
    private val gatheringWaiters = mutableListOf<() -> Unit>()
    private var isGatheringComplete = false
    private val userIdByMid = mutableMapOf<String, String>()

    @Volatile
    private var firstFrame: kotlinx.coroutines.CompletableDeferred<Boolean>? = null

    override val localVideoTrack: CallVideoTrack? get() = if (isCameraSending) cameraTrack?.let { track -> WebRtcVideoTrack(track) } else null

    private var isCameraSending = false

    init {
        val configuration = PeerConnection.RTCConfiguration(emptyList()).apply {
            sdpSemantics = PeerConnection.SdpSemantics.UNIFIED_PLAN
            continualGatheringPolicy = PeerConnection.ContinualGatheringPolicy.GATHER_ONCE
            // The media server listens for TCP as well as UDP on its one port, for networks that let no UDP through.
            tcpCandidatePolicy = PeerConnection.TcpCandidatePolicy.ENABLED
        }
        val observer = object : QuietObserver() {
            override fun onIceGatheringChange(state: PeerConnection.IceGatheringState?) {
                if (state == PeerConnection.IceGatheringState.COMPLETE) finishGathering()
            }

            override fun onIceConnectionChange(state: PeerConnection.IceConnectionState?) {
                val mapped = when (state) {
                    PeerConnection.IceConnectionState.CONNECTED, PeerConnection.IceConnectionState.COMPLETED -> CallConnectionState.Connected
                    PeerConnection.IceConnectionState.DISCONNECTED -> CallConnectionState.Disconnected
                    PeerConnection.IceConnectionState.FAILED, PeerConnection.IceConnectionState.CLOSED -> CallConnectionState.Failed
                    else -> CallConnectionState.Connecting
                }
                listener?.onConnectionState(mapped)
            }
        }
        val made = factory.createPeerConnection(configuration, observer) ?: throw IllegalStateException("the media connection could not be made")
        connection = made
        val source = factory.createAudioSource(MediaConstraints())
        audioSource = source
        val track = factory.createAudioTrack("audio-teanode-phone", source)
        audioTrack = track
        made.addTrack(track, listOf(STREAM_ID))
        videoSource = factory.createVideoSource(false)
        cameraTrack = factory.createVideoTrack("video-camera", videoSource)
        // The video line is always in the offer, so turning the camera on later needs no new negotiation.
        val transceiver = made.addTransceiver(
            MediaStreamTrack.MediaType.MEDIA_TYPE_VIDEO,
            RtpTransceiver.RtpTransceiverInit(RtpTransceiver.RtpTransceiverDirection.SEND_RECV, listOf(STREAM_ID), videoLayers()),
        )
        videoTransceiver = transceiver
        preferVideoCodecs(transceiver)
    }

    /** The server takes VP8 and H.264 only, so those are offered first. */
    private fun preferVideoCodecs(transceiver: RtpTransceiver) {
        val capabilities = factory.getRtpReceiverCapabilities(MediaStreamTrack.MediaType.MEDIA_TYPE_VIDEO)
        val wanted = listOf("vp8", "h264").flatMap { name -> capabilities.codecs.filter { codec -> codec.name.equals(name, ignoreCase = true) } } +
            capabilities.codecs.filter { codec -> codec.name.equals("rtx", ignoreCase = true) }
        if (wanted.isNotEmpty()) transceiver.setCodecPreferences(wanted)
    }

    /** The two sizes the camera is sent in: q, a third of the picture, and f, the whole of it. The server passes on the whole one only to those who feature this person. */
    private fun videoLayers(): List<RtpParameters.Encoding> = listOf(
        RtpParameters.Encoding("q", true, 3.0).apply { maxBitrateBps = 200_000 },
        RtpParameters.Encoding("f", true, 1.0).apply { maxBitrateBps = 1_200_000 },
    )

    // MARK: Gathering the local addresses

    private fun finishGathering() {
        val waiters = synchronized(gatheringWaiters) {
            isGatheringComplete = true
            gatheringWaiters.toList().also { gatheringWaiters.clear() }
        }
        waiters.forEach { waiter -> waiter() }
    }

    /** Waits until the local addresses are all found, or a few seconds, so that the offer or answer carries them: the server is sent no candidates of their own. */
    private suspend fun waitForGathering() {
        withTimeoutOrNull(GATHERING_WAIT_MS) {
            suspendCancellableCoroutine { continuation ->
                val isDone = synchronized(gatheringWaiters) {
                    if (!isGatheringComplete) gatheringWaiters += { if (continuation.isActive) continuation.resume(Unit) }
                    isGatheringComplete
                }
                if (isDone) continuation.resume(Unit)
            }
        }
    }

    // MARK: Offers and answers

    private suspend fun createDescription(isOffer: Boolean): SessionDescription {
        val active = connection ?: throw IllegalStateException("the media connection is closed")
        return suspendCancellableCoroutine { continuation ->
            val creation = object : SdpObserver {
                override fun onCreateSuccess(description: SessionDescription) = continuation.resume(description)
                override fun onCreateFailure(error: String?) = continuation.resumeWithException(IllegalStateException(error ?: "the description could not be made"))
                override fun onSetSuccess() {}
                override fun onSetFailure(error: String?) {}
            }
            if (isOffer) active.createOffer(creation, MediaConstraints()) else active.createAnswer(creation, MediaConstraints())
        }
    }

    private fun setObserver(continuation: CancellableContinuation<Unit>) = object : SdpObserver {
        override fun onCreateSuccess(description: SessionDescription?) {}
        override fun onCreateFailure(error: String?) {}
        override fun onSetSuccess() = continuation.resume(Unit)
        override fun onSetFailure(error: String?) = continuation.resumeWithException(IllegalStateException(error ?: "the description was refused"))
    }

    private suspend fun setLocal(description: SessionDescription) {
        val active = connection ?: throw IllegalStateException("the media connection is closed")
        suspendCancellableCoroutine { continuation -> active.setLocalDescription(setObserver(continuation), description) }
    }

    private suspend fun setRemote(description: SessionDescription) {
        val active = connection ?: throw IllegalStateException("the media connection is closed")
        suspendCancellableCoroutine { continuation -> active.setRemoteDescription(setObserver(continuation), description) }
    }

    override suspend fun makeJoinOffer(): String {
        setLocal(createDescription(isOffer = true))
        waitForGathering()
        return connection?.localDescription?.description ?: throw IllegalStateException("the offer is missing")
    }

    override suspend fun setJoinAnswer(answerSdp: String) {
        setRemote(SessionDescription(SessionDescription.Type.ANSWER, answerSdp))
        refreshRemoteTracks(answerSdp)
    }

    override suspend fun answer(offerSdp: String): String {
        setRemote(SessionDescription(SessionDescription.Type.OFFER, offerSdp))
        setLocal(createDescription(isOffer = false))
        waitForGathering()
        refreshRemoteTracks(offerSdp)
        return connection?.localDescription?.description ?: throw IllegalStateException("the answer is missing")
    }

    /**
     * Finds the others' pictures. Each track belongs to the person whose user id is its stream id, which the offer
     * names for every media section (`a=msid`); the section's own transceiver holds the track.
     */
    private fun refreshRemoteTracks(sdp: String) {
        for (section in ChatCallSDP.sections(sdp)) {
            if (section.isSending && section.streamId != null) userIdByMid[section.mid] = section.streamId!! else userIdByMid.remove(section.mid)
        }
        val tracks = mutableMapOf<String, CallVideoTrack>()
        for (transceiver in connection?.transceivers.orEmpty()) {
            if (transceiver.mediaType != MediaStreamTrack.MediaType.MEDIA_TYPE_VIDEO) continue
            val userId = transceiver.mid?.let { mid -> userIdByMid[mid] } ?: continue
            val track = transceiver.receiver.track() as? VideoTrack ?: continue
            tracks[userId] = WebRtcVideoTrack(track)
        }
        listener?.onRemoteVideoChange(tracks)
    }

    // MARK: The person's own microphone and camera

    override fun setMuted(isMuted: Boolean) {
        audioTrack?.setEnabled(!isMuted)
    }

    override suspend fun setCamera(isOn: Boolean, usesFrontCamera: Boolean): Boolean = withContext(Dispatchers.Main.immediate) {
        val transceiver = videoTransceiver ?: return@withContext false
        if (!isOn) {
            stopCapture()
            transceiver.sender.setTrack(null, false)
            isCameraSending = false
            return@withContext true
        }
        if (!startCapture(usesFrontCamera)) return@withContext false
        // A camera the app may not use, or one that is busy, opens and then never delivers a frame.
        val hasFrame = withTimeoutOrNull(FIRST_FRAME_WAIT_MS) { firstFrame?.await() } ?: false
        if (!hasFrame) {
            stopCapture()
            return@withContext false
        }
        cameraTrack?.setEnabled(true)
        transceiver.sender.setTrack(cameraTrack, false)
        isCameraSending = true
        true
    }

    override suspend fun flipCamera(usesFrontCamera: Boolean): Boolean = withContext(Dispatchers.Main.immediate) {
        val active = capturer as? CameraVideoCapturer ?: return@withContext false
        suspendCancellableCoroutine { continuation ->
            active.switchCamera(object : CameraVideoCapturer.CameraSwitchHandler {
                override fun onCameraSwitchDone(isFrontCamera: Boolean) {
                    if (continuation.isActive) continuation.resume(true)
                }

                override fun onCameraSwitchError(errorDescription: String?) {
                    if (continuation.isActive) continuation.resume(false)
                }
            })
        }
    }

    private val cameraEvents = object : CameraVideoCapturer.CameraEventsHandler {
        override fun onCameraError(errorDescription: String?) {
            firstFrame?.complete(false)
        }

        override fun onCameraDisconnected() {}
        override fun onCameraFreezed(errorDescription: String?) {}
        override fun onCameraOpening(cameraName: String?) {}
        override fun onFirstFrameAvailable() {
            firstFrame?.complete(true)
        }

        override fun onCameraClosed() {}
    }

    private fun startCapture(usesFrontCamera: Boolean): Boolean {
        val source = videoSource ?: return false
        val enumerator = Camera2Enumerator(context)
        val names = enumerator.deviceNames
        val name = names.firstOrNull { candidate -> enumerator.isFrontFacing(candidate) == usesFrontCamera } ?: names.firstOrNull() ?: return false
        firstFrame = kotlinx.coroutines.CompletableDeferred()
        val created = enumerator.createCapturer(name, cameraEvents) ?: return false
        val helper = SurfaceTextureHelper.create("CallCapture", WebRtcFactory.eglBase.eglBaseContext)
        created.initialize(helper, context, source.capturerObserver)
        // About 960 by 540, which looks good in a tile and is light enough for a phone to send; at most 30 frames a second.
        created.startCapture(CAPTURE_WIDTH, CAPTURE_HEIGHT, CAPTURE_FRAMES_PER_SECOND)
        capturer = created
        textureHelper = helper
        return true
    }

    private fun stopCapture() {
        runCatching { capturer?.stopCapture() }
        capturer?.dispose()
        capturer = null
        textureHelper?.dispose()
        textureHelper = null
    }

    override fun close() {
        stopCapture()
        runCatching { videoTransceiver?.sender?.setTrack(null, false) }
        connection?.close()
        connection?.dispose()
        connection = null
        audioTrack?.dispose()
        cameraTrack?.dispose()
        audioSource?.dispose()
        videoSource?.dispose()
        audioTrack = null
        cameraTrack = null
        audioSource = null
        videoSource = null
        listener = null
        finishGathering()
    }

    private companion object {
        const val STREAM_ID = "teanode-phone"
        const val GATHERING_WAIT_MS = 5_000L
        const val FIRST_FRAME_WAIT_MS = 4_000L
        const val CAPTURE_WIDTH = 960
        const val CAPTURE_HEIGHT = 540
        const val CAPTURE_FRAMES_PER_SECOND = 30
    }
}
