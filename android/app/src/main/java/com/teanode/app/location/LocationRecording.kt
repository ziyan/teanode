package com.teanode.app.location

import androidx.compose.runtime.compositionLocalOf
import com.teanode.kit.location.LocationPause
import com.teanode.kit.location.LocationPauseChoice
import com.teanode.kit.location.LocationPhonePermission
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.time.Instant

/** What the phone says about its own location permission. */
enum class LocationPermissionState {
    /** The person has not been asked yet. */
    Undecided,

    /** Location is off for the app. */
    Denied,

    /** Only while the app is open: positions are not recorded when it is closed. */
    WhileUsing,

    /** All the time: positions are recorded while the app is closed. */
    Always,
}

/** The phone's position now, as the recorder last read it. */
data class LivePosition(val latitudeDegrees: Double, val longitudeDegrees: Double, val accuracyMeters: Double, val measuredAt: Instant)

/** What the recorder needs the screen to do about the phone's permission: ask the system, explain first, or send the person to the settings. */
enum class LocationPermissionRequest {
    /** Ask for location while the app is open (the system's question). */
    Foreground,

    /** Say why recording while the app is closed needs "all the time", then ask. */
    ExplainAlways,

    /** Ask for location all the time (the system's question, or its settings page from Android 11 on). */
    Background,

    /** Ask for the phone's activity recognition. */
    Activity,

    /** Open the app's page in the system settings. */
    Settings,
}

/**
 * LocationRecording is what the Location screens ask of the recorder that follows the phone: whether it is on, its
 * pause, what waits to be sent, and the phone's permission. The real one is [com.teanode.app.location.recording.LocationRecorder];
 * demo mode and the screenshot tests use [DemoLocationRecording].
 */
interface LocationRecording {
    val isRecordingOn: StateFlow<Boolean>
    val activePause: StateFlow<LocationPause?>
    val hasPendingPauseChange: StateFlow<Boolean>
    val unsentCount: StateFlow<Int>

    /** Why the server refused what the phone noticed, which stopped recording; null when nothing was refused. */
    val refusalText: StateFlow<String?>
    val permission: StateFlow<LocationPermissionState>
    val isMotionAvailable: Boolean
    val isMotionAuthorized: StateFlow<Boolean>
    val livePosition: StateFlow<LivePosition?>

    /** Counts up each time the server took something that changes what the person sees of others. */
    val sharingLimitRevision: StateFlow<Int>

    /** Why a pause or a resume made here was refused by the server, until [clearPauseRefusal]. */
    val pauseRefusalText: StateFlow<String?>

    /** What the recorder wants the screen to ask of the system next. */
    val permissionRequests: kotlinx.coroutines.flow.SharedFlow<LocationPermissionRequest>

    /** The person answered the system's question: read the permission again. */
    fun permissionAnswered()

    /** The person said "Not now" to the explanation of "all the time". */
    fun declineAlwaysExplanation()

    fun setRecording(isOn: Boolean)
    fun pauseRecording(choice: LocationPauseChoice)
    fun resumeRecording()
    fun clearPauseRefusal()

    /** The person's places changed: the places watched for arrivals are chosen again. */
    fun placesDidChange()

    /** The server said which pause is in effect. */
    fun learnPause(serverPause: LocationPause?)
    fun isSetupDismissed(accountKey: String?): Boolean
    fun dismissSetup(accountKey: String?)

    /** Holds reporting while an erase is on its way, and then, on release, throws away what was not sent in the range erased. */
    suspend fun holdReporting()
    fun releaseReporting(discardingFrom: Instant?, until: Instant?, isErased: Boolean)

    /** The phone's position: the recorder's own when it is fresh, else asked of the phone; null when the phone cannot tell. */
    suspend fun currentReading(): LivePosition?

    /** Asks for the location permission the screen needs next (the dialog, then settings once refused). */
    fun requestPermission()

    fun requestMotionPermission()
    fun openSystemSettings()

    /** The phone's permission as the server takes it. */
    val phonePermission: LocationPhonePermission
        get() = when (permission.value) {
            LocationPermissionState.Always -> LocationPhonePermission.Always
            LocationPermissionState.WhileUsing -> LocationPhonePermission.WhileUsing
            else -> LocationPhonePermission.Off
        }
}

/** The recorder this screen talks to; the shell's section provides it. */
val LocalLocationRecording = compositionLocalOf<LocationRecording> { DemoLocationRecording() }

/** DemoLocationRecording stands in for the recorder in demo mode and the screenshot tests: it records nothing and keeps whatever it is told. */
class DemoLocationRecording(
    isRecordingOn: Boolean = true,
    activePause: LocationPause? = null,
    permission: LocationPermissionState = LocationPermissionState.Always,
    unsentCount: Int = 0,
    position: LivePosition? = null,
    private val isSetupAlwaysDismissed: Boolean = false,
) : LocationRecording {
    private val recording = MutableStateFlow(isRecordingOn)
    private val pause = MutableStateFlow(activePause)
    private val permissionState = MutableStateFlow(permission)
    private val unsent = MutableStateFlow(unsentCount)
    private val live = MutableStateFlow(position)
    private val dismissedAccountKeys = mutableSetOf<String?>()

    override val isRecordingOn: StateFlow<Boolean> = recording.asStateFlow()
    override val activePause: StateFlow<LocationPause?> = pause.asStateFlow()
    override val hasPendingPauseChange: StateFlow<Boolean> = MutableStateFlow(false)
    override val unsentCount: StateFlow<Int> = unsent.asStateFlow()
    override val refusalText: StateFlow<String?> = MutableStateFlow(null)
    override val permission: StateFlow<LocationPermissionState> = permissionState.asStateFlow()
    override val isMotionAvailable: Boolean = true
    override val isMotionAuthorized: StateFlow<Boolean> = MutableStateFlow(true)
    override val livePosition: StateFlow<LivePosition?> = live.asStateFlow()
    override val sharingLimitRevision: StateFlow<Int> = MutableStateFlow(0)
    override val pauseRefusalText: StateFlow<String?> = MutableStateFlow(null)

    override val permissionRequests: kotlinx.coroutines.flow.SharedFlow<LocationPermissionRequest> = kotlinx.coroutines.flow.MutableSharedFlow()
    override fun permissionAnswered() = Unit
    override fun declineAlwaysExplanation() = Unit

    override fun setRecording(isOn: Boolean) {
        recording.value = isOn
    }

    override fun pauseRecording(choice: LocationPauseChoice) {
        val now = Instant.now()
        pause.value = LocationPause.of("demo-pause", now, choice.endInstant(now))
    }

    override fun resumeRecording() {
        pause.value = null
    }

    override fun clearPauseRefusal() = Unit
    override fun placesDidChange() = Unit
    override fun learnPause(serverPause: LocationPause?) {
        pause.value = serverPause
    }

    override fun isSetupDismissed(accountKey: String?): Boolean = isSetupAlwaysDismissed || dismissedAccountKeys.contains(accountKey)
    override fun dismissSetup(accountKey: String?) {
        dismissedAccountKeys.add(accountKey)
    }

    override suspend fun holdReporting() = Unit
    override fun releaseReporting(discardingFrom: Instant?, until: Instant?, isErased: Boolean) = Unit
    override suspend fun currentReading(): LivePosition? = live.value ?: LivePosition(10.5001, 20.5001, 12.0, Instant.now())
    override fun requestPermission() {
        permissionState.value = LocationPermissionState.Always
    }

    override fun requestMotionPermission() = Unit
    override fun openSystemSettings() = Unit
}
