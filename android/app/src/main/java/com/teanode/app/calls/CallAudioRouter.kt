package com.teanode.app.calls

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceCallback
import android.media.AudioDeviceInfo
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.os.Build
import com.teanode.kit.calls.ChatCallAudioRoute
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * CallAudioRouter keeps the phone's audio ready for a call and says where the sound goes: the earpiece, the speaker,
 * headphones on a wire, or a Bluetooth headset, following the route as it changes (headphones put in or taken out).
 * The audio is in communication mode for as long as a call goes, which is what turns the phone's echo cancelling on.
 * It is the Android side of the iPhone app's `ChatCallAudioRoute` and the audio session it sets up.
 */
class CallAudioRouter(context: Context) : CallAudio {
    private val audioManager: AudioManager = context.getSystemService(AudioManager::class.java)
    private val currentRoute = MutableStateFlow<ChatCallAudioRoute>(ChatCallAudioRoute.Phone)

    /** Where the sound goes now. */
    override val route: StateFlow<ChatCallAudioRoute> = currentRoute.asStateFlow()

    private val offeredRoutes = MutableStateFlow<List<ChatCallAudioRoute>>(listOf(ChatCallAudioRoute.Phone, ChatCallAudioRoute.Speaker))

    /** The routes the person can choose among now. */
    override val availableRoutes: StateFlow<List<ChatCallAudioRoute>> = offeredRoutes.asStateFlow()

    private var focusRequest: AudioFocusRequest? = null
    private var previousMode: Int = AudioManager.MODE_NORMAL
    private var isStarted = false
    private var chosenRoute: ChatCallAudioRoute? = null

    private val deviceCallback = object : AudioDeviceCallback() {
        override fun onAudioDevicesAdded(addedDevices: Array<out AudioDeviceInfo>) = refresh()
        override fun onAudioDevicesRemoved(removedDevices: Array<out AudioDeviceInfo>) = refresh()
    }

    /** Takes the audio for a call. */
    override fun start() {
        if (isStarted) return
        isStarted = true
        previousMode = audioManager.mode
        val request = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT)
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
            .build()
        focusRequest = request
        audioManager.requestAudioFocus(request)
        audioManager.mode = AudioManager.MODE_IN_COMMUNICATION
        audioManager.registerAudioDeviceCallback(deviceCallback, null)
        refresh()
        // A headset that is connected is used, as the Phone app does; the earpiece otherwise.
        val headset = offeredRoutes.value.firstOrNull { offered -> offered.isPrivate }
        choose(headset ?: ChatCallAudioRoute.Phone)
    }

    /** Gives the audio back. */
    override fun stop() {
        if (!isStarted) return
        isStarted = false
        audioManager.unregisterAudioDeviceCallback(deviceCallback)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) audioManager.clearCommunicationDevice() else audioManager.isSpeakerphoneOn = false
        audioManager.mode = previousMode
        focusRequest?.let { request -> audioManager.abandonAudioFocusRequest(request) }
        focusRequest = null
        chosenRoute = null
        currentRoute.value = ChatCallAudioRoute.Phone
    }

    /** Sends the sound to a route. */
    override fun choose(route: ChatCallAudioRoute) {
        chosenRoute = route
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            val device = communicationDevices().firstOrNull { candidate -> routeOf(candidate) == route }
            if (device != null) audioManager.setCommunicationDevice(device)
        } else {
            audioManager.isSpeakerphoneOn = route == ChatCallAudioRoute.Speaker
        }
        currentRoute.value = route
    }

    private fun communicationDevices(): List<AudioDeviceInfo> =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) audioManager.availableCommunicationDevices else audioManager.getDevices(AudioManager.GET_DEVICES_OUTPUTS).toList()

    private fun refresh() {
        val routes = communicationDevices().mapNotNull { device -> routeOf(device) }.distinct()
            .ifEmpty { listOf(ChatCallAudioRoute.Phone, ChatCallAudioRoute.Speaker) }
            .sortedBy { route -> ChatCallAudioRoute.order(route) }
        offeredRoutes.value = routes
        // What was chosen is gone (the headset was taken off): back to the earpiece, as the sound moves by itself.
        val chosen = chosenRoute
        if (isStarted && chosen != null && chosen !in routes) choose(ChatCallAudioRoute.Phone)
    }

    private fun routeOf(device: AudioDeviceInfo): ChatCallAudioRoute? = when (device.type) {
        AudioDeviceInfo.TYPE_BUILTIN_EARPIECE -> ChatCallAudioRoute.Phone
        AudioDeviceInfo.TYPE_BUILTIN_SPEAKER -> ChatCallAudioRoute.Speaker
        AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES, AudioDeviceInfo.TYPE_USB_HEADSET, AudioDeviceInfo.TYPE_USB_DEVICE ->
            ChatCallAudioRoute.Headphones
        AudioDeviceInfo.TYPE_BLUETOOTH_SCO, AudioDeviceInfo.TYPE_BLE_HEADSET, AudioDeviceInfo.TYPE_HEARING_AID -> ChatCallAudioRoute.Bluetooth(device.productName?.toString().orEmpty())
        else -> null
    }
}
