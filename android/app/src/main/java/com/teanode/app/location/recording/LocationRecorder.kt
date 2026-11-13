package com.teanode.app.location.recording

import android.Manifest
import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import com.google.android.gms.location.Geofence
import com.google.android.gms.location.GeofencingRequest
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import com.teanode.app.TeaNodeApplication
import com.teanode.app.location.LivePosition
import com.teanode.app.location.LocationPermissionRequest
import com.teanode.app.location.LocationPermissionState
import com.teanode.app.location.LocationRecording
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.location.LocationBatch
import com.teanode.kit.location.LocationDwellDetector
import com.teanode.kit.location.LocationOutbox
import com.teanode.kit.location.LocationPause
import com.teanode.kit.location.LocationPauseChange
import com.teanode.kit.location.LocationPauseChoice
import com.teanode.kit.location.LocationPauseFailure
import com.teanode.kit.location.LocationPauseRetry
import com.teanode.kit.location.LocationPauseTracker
import com.teanode.kit.location.LocationPermissionReport
import com.teanode.kit.location.LocationPhonePermission
import com.teanode.kit.location.LocationPlaceWatch
import com.teanode.kit.location.LocationPointFilter
import com.teanode.kit.location.LocationReportFailure
import com.teanode.kit.location.LocationRetryGate
import com.teanode.kit.location.ReportedPoint
import com.teanode.kit.location.WatchedPlace
import com.teanode.kit.location.locationPlaces
import com.teanode.kit.location.pauseLocation
import com.teanode.kit.location.reportLocation
import com.teanode.kit.location.reportLocationPermission
import com.teanode.kit.location.resumeLocation
import com.teanode.kit.location.watchedStores
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
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.File
import java.time.Instant

/**
 * LocationRecorder follows the phone and tells the server where the person has been, as the iPhone app's
 * `LocationRecorder` does: positions kept by the point filter and stays noticed by the dwell detector wait in a per-account
 * file (the outbox) until the server has taken them; the person's named places are watched with geofences; and a pause
 * applies at once and is sent until the server has it. It runs in the app's process and is fed by
 * [LocationRecordingService], which holds the foreground notification and the location updates while recording is on.
 *
 * Not verified on a phone: the logic it uses (filter, outbox, batches, retries, pause tracker, dwell) is the kit's and is
 * tested on the JVM.
 */
class LocationRecorder private constructor(private val context: Context) : LocationRecording {
    private val preferences: SharedPreferences = context.getSharedPreferences("teanode-location", Context.MODE_PRIVATE)
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private val mutex = Mutex()
    private val application get() = context.applicationContext as TeaNodeApplication

    private val recording = MutableStateFlow(preferences.getBoolean(RECORDING_KEY, true))
    private val pause = MutableStateFlow<LocationPause?>(null)
    private val hasPendingPause = MutableStateFlow(false)
    private val unsent = MutableStateFlow(0)
    private val refusal = MutableStateFlow<String?>(null)
    private val permissionState = MutableStateFlow(readPermission())
    private val motionAuthorized = MutableStateFlow(isActivityRecognitionGranted())
    private val live = MutableStateFlow<LivePosition?>(null)
    private val limitRevision = MutableStateFlow(0)
    private val pauseRefusal = MutableStateFlow<String?>(null)
    private val requests = MutableSharedFlow<LocationPermissionRequest>(extraBufferCapacity = 4)

    override val isRecordingOn: StateFlow<Boolean> = recording.asStateFlow()
    override val activePause: StateFlow<LocationPause?> = pause.asStateFlow()
    override val hasPendingPauseChange: StateFlow<Boolean> = hasPendingPause.asStateFlow()
    override val unsentCount: StateFlow<Int> = unsent.asStateFlow()
    override val refusalText: StateFlow<String?> = refusal.asStateFlow()
    override val permission: StateFlow<LocationPermissionState> = permissionState.asStateFlow()
    override val isMotionAvailable: Boolean = Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q
    override val isMotionAuthorized: StateFlow<Boolean> = motionAuthorized.asStateFlow()
    override val livePosition: StateFlow<LivePosition?> = live.asStateFlow()
    override val sharingLimitRevision: StateFlow<Int> = limitRevision.asStateFlow()
    override val pauseRefusalText: StateFlow<String?> = pauseRefusal.asStateFlow()
    override val permissionRequests: SharedFlow<LocationPermissionRequest> = requests.asSharedFlow()

    private var outbox: LocationOutbox? = null
    private var tracker = LocationPauseTracker()
    private var stay: LocationDwellDetector.Stay? = null
    private val pointFilter = LocationPointFilter()
    private val dwellDetector = LocationDwellDetector()
    private val retryGate = LocationRetryGate()
    private var isReportingHeld = false
    private var pauseJob: Job? = null
    private var lastReportedPermission: LocationPermissionReport? = null
    private var watchedPlaces: List<WatchedPlace> = emptyList()

    private val accountKey: String? get() = application.session.connection.value?.accountKey
    private val connection: Connection? get() = application.session.connection.value

    private fun outboxFile(key: String): File = File(File(context.filesDir, "location").also { directory -> directory.mkdirs() }, LocationOutbox.fileName(key))

    private fun loadOutbox(key: String): LocationOutbox {
        outbox?.takeIf { each -> each.accountKey == key }?.let { each -> return each }
        val read = runCatching { LocationOutbox.decoded(outboxFile(key).readText()) }.getOrNull()?.takeIf { each -> each.accountKey == key } ?: LocationOutbox(accountKey = key)
        outbox = read
        tracker = runCatching { TeaNodeJson.decodeFromString(LocationPauseTracker.serializer(), preferences.getString(LocationPauseTracker.storageKey(key), null)!!) }.getOrDefault(LocationPauseTracker())
        stay = runCatching { TeaNodeJson.decodeFromString(LocationDwellDetector.Stay.serializer(), preferences.getString(STAY_KEY + key, null)!!) }.getOrNull()
        pause.value = tracker.pause?.takeIf { each -> each.isInEffect(Instant.now()) }
        hasPendingPause.value = tracker.hasPendingChange
        unsent.value = read.points.size + read.visits.size + read.activities.size
        return read
    }

    private fun save(key: String) {
        val current = outbox ?: return
        runCatching { outboxFile(key).writeText(current.encoded()) }
        preferences.edit()
            .putString(LocationPauseTracker.storageKey(key), TeaNodeJson.encodeToString(LocationPauseTracker.serializer(), tracker))
            .also { editor -> stay?.let { kept -> editor.putString(STAY_KEY + key, TeaNodeJson.encodeToString(LocationDwellDetector.Stay.serializer(), kept)) } ?: editor.remove(STAY_KEY + key) }
            .apply()
        unsent.value = current.points.size + current.visits.size + current.activities.size
    }

    // The phone's permission

    private fun isGranted(permission: String): Boolean = ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED

    private fun isActivityRecognitionGranted(): Boolean = Build.VERSION.SDK_INT < Build.VERSION_CODES.Q || isGranted(Manifest.permission.ACTIVITY_RECOGNITION)

    private fun readPermission(): LocationPermissionState {
        val isForeground = isGranted(Manifest.permission.ACCESS_FINE_LOCATION) || isGranted(Manifest.permission.ACCESS_COARSE_LOCATION)
        if (!isForeground) return if (preferences.getBoolean(ASKED_KEY, false)) LocationPermissionState.Denied else LocationPermissionState.Undecided
        val isBackground = Build.VERSION.SDK_INT < Build.VERSION_CODES.Q || isGranted(Manifest.permission.ACCESS_BACKGROUND_LOCATION)
        return if (isBackground) LocationPermissionState.Always else LocationPermissionState.WhileUsing
    }

    override fun requestPermission() {
        val next = when (permissionState.value) {
            LocationPermissionState.Undecided -> LocationPermissionRequest.Foreground
            LocationPermissionState.WhileUsing -> if (preferences.getBoolean(DECLINED_ALWAYS_KEY, false)) LocationPermissionRequest.Settings else LocationPermissionRequest.ExplainAlways
            LocationPermissionState.Denied -> LocationPermissionRequest.Settings
            LocationPermissionState.Always -> return
        }
        requests.tryEmit(next)
    }

    override fun requestMotionPermission() {
        requests.tryEmit(if (motionAuthorized.value) LocationPermissionRequest.Settings else LocationPermissionRequest.Activity)
    }

    /** The person read why and went on: the system's question for "all the time". */
    fun requestAlways() {
        requests.tryEmit(LocationPermissionRequest.Background)
    }

    override fun declineAlwaysExplanation() {
        preferences.edit().putBoolean(DECLINED_ALWAYS_KEY, true).apply()
    }

    override fun permissionAnswered() {
        preferences.edit().putBoolean(ASKED_KEY, true).apply()
        permissionState.value = readPermission()
        motionAuthorized.value = isActivityRecognitionGranted()
        applyRecording()
        scope.launch { reportPermissionIfChanged() }
    }

    override fun openSystemSettings() {
        requests.tryEmit(LocationPermissionRequest.Settings)
    }

    // Recording

    override fun setRecording(isOn: Boolean) {
        recording.value = isOn
        refusal.value = null
        preferences.edit().putBoolean(RECORDING_KEY, isOn).apply()
        applyRecording()
        scope.launch { reportPermissionIfChanged() }
    }

    /** Starts or stops the service that follows the phone, as recording, permission and pause say. */
    fun applyRecording() {
        val isAllowed = recording.value && permissionState.value != LocationPermissionState.Undecided && permissionState.value != LocationPermissionState.Denied && refusal.value == null
        if (isAllowed && connection != null) LocationRecordingService.start(context) else LocationRecordingService.stop(context)
        scope.launch { chooseWatchedPlaces() }
    }

    /** One position the service read: kept when the pause, the point filter and the server's rules allow, and stays noticed from it. */
    fun onLocation(latitudeDegrees: Double, longitudeDegrees: Double, accuracyMeters: Double, speedMetersPerSecond: Double?, altitudeMeters: Double?, courseDegrees: Double?, measuredAt: Instant) {
        live.value = LivePosition(latitudeDegrees, longitudeDegrees, accuracyMeters, measuredAt)
        val key = accountKey ?: return
        scope.launch {
            mutex.withLock {
                val current = loadOutbox(key)
                val now = Instant.now()
                tracker = tracker.settled(now)
                if (!tracker.isRecordable(pointMeasuredAt = measuredAt)) return@withLock
                if (pointFilter.shouldKeep(latitudeDegrees, longitudeDegrees, speedMetersPerSecond, measuredAt, isStationary = false)) {
                    current.addPoint(ReportedPoint(latitudeDegrees = latitudeDegrees, longitudeDegrees = longitudeDegrees, accuracyMeters = maxOf(accuracyMeters, 0.0), altitudeMeters = altitudeMeters, speedMetersPerSecond = speedMetersPerSecond, courseDegrees = courseDegrees, measuredAt = measuredAt))
                }
                val (nextStay, events) = dwellDetector.consume(stay, latitudeDegrees, longitudeDegrees, accuracyMeters, measuredAt)
                stay = nextStay
                for (event in events) {
                    val visit = dwellDetector.visitOf(event)
                    if (tracker.isRecordable(visit.arrivedAt, visit.departedAt)) current.addVisit(visit)
                }
                save(key)
            }
            report()
        }
    }

    /** The phone entered or left a watched place. */
    fun onGeofence(identifier: String, isEntering: Boolean, at: Instant) {
        val key = accountKey ?: return
        scope.launch {
            mutex.withLock {
                val current = loadOutbox(key)
                tracker = tracker.settled(at)
                if (isEntering) {
                    val place = watchedPlaces.firstOrNull { each -> each.identifier == identifier } ?: return@withLock
                    if (tracker.isRecordable(visitArrivedAt = at, visitDepartedAt = null)) current.arrive(place, at)
                } else {
                    current.depart(identifier, tracker.departureInstant(at))
                }
                save(key)
            }
            report()
        }
    }

    /** Sends what waits, a batch at a time, until the server has taken it all or a failure says to stop. */
    suspend fun report() {
        val asked = connection ?: return
        val key = asked.accountKey
        mutex.withLock {
            if (isReportingHeld || !retryGate.canTry(Instant.now())) return
            val current = loadOutbox(key)
            current.removeTooOld()
            while (!current.isEmpty) {
                val batch = current.nextBatch()
                try {
                    send(asked, current, batch)
                } catch (error: CancellationException) {
                    throw error
                } catch (error: Exception) {
                    when (val failure = LocationReportFailure.classify(error)) {
                        LocationReportFailure.RetryLater, LocationReportFailure.PhoneNotFound -> { retryGate.didFail(Instant.now()); save(key); return }
                        is LocationReportFailure.Refused -> {
                            refusal.value = failure.reason
                            recording.value = false
                            applyRecording()
                            save(key)
                            return
                        }
                        // Sending the same batch again cannot change the answer, and send() already split it down to one entry.
                        is LocationReportFailure.BatchInvalid -> current.remove(batch)
                    }
                }
            }
            save(key)
        }
        reportPermissionIfChanged()
    }

    /** Sends one batch. When the server calls it invalid without saying why, the halves are sent apart, until one entry is left, which is dropped. */
    private suspend fun send(asked: Connection, current: LocationOutbox, batch: LocationBatch) {
        try {
            val result = asked.reportLocation(batch)
            current.remove(batch)
            tracker = tracker.learning(result.pause, Instant.now())
            pause.value = tracker.pause?.takeIf { each -> each.isInEffect(Instant.now()) }
            retryGate.didSucceed()
        } catch (error: CancellationException) {
            throw error
        } catch (error: Exception) {
            if (LocationReportFailure.classify(error) !is LocationReportFailure.BatchInvalid) throw error
            val halves = batch.split() ?: throw error
            send(asked, current, halves.first)
            send(asked, current, halves.second)
        }
    }

    /** Tells the server the phone's permission and the recording switch, when they changed. */
    private suspend fun reportPermissionIfChanged() {
        val asked = connection ?: return
        val report = LocationPermissionReport(phonePermission, recording.value)
        if (report == lastReportedPermission) return
        try {
            asked.reportLocationPermission(report)
            lastReportedPermission = report
            limitRevision.value += 1
        } catch (error: CancellationException) {
            throw error
        } catch (_: Exception) {
            // Tried again at the next report.
        }
    }

    // Pausing

    override fun pauseRecording(choice: LocationPauseChoice) {
        val key = accountKey ?: return
        scope.launch {
            mutex.withLock {
                loadOutbox(key)
                val now = Instant.now()
                tracker = tracker.startingPause(choice.endInstant(now), now)
                pause.value = tracker.pause?.takeIf { each -> each.isInEffect(now) }
                hasPendingPause.value = tracker.hasPendingChange
                save(key)
            }
            sendPauseChange()
        }
    }

    override fun resumeRecording() {
        val key = accountKey ?: return
        scope.launch {
            mutex.withLock {
                loadOutbox(key)
                val now = Instant.now()
                tracker = tracker.endingPause(now)
                pause.value = tracker.pause?.takeIf { each -> each.isInEffect(now) }
                hasPendingPause.value = tracker.hasPendingChange
                save(key)
            }
            sendPauseChange()
        }
    }

    /** Sends the change made on the phone until the server has it, waiting longer after each failure. */
    private fun sendPauseChange() {
        if (pauseJob?.isActive == true) return
        pauseJob = scope.launch {
            var failureCount = 0
            while (true) {
                val asked = connection ?: return@launch
                val change = mutex.withLock { tracker.pendingChange } ?: return@launch
                try {
                    val answer: LocationPause? = when (change) {
                        is LocationPauseChange.Pause -> asked.pauseLocation(change.endedAt)
                        is LocationPauseChange.Resume -> { asked.resumeLocation(); null }
                    }
                    mutex.withLock {
                        tracker = tracker.changeWasTaken(change, answer, Instant.now())
                        hasPendingPause.value = tracker.hasPendingChange
                        pause.value = tracker.pause?.takeIf { each -> each.isInEffect(Instant.now()) }
                        accountKey?.let { key -> save(key) }
                    }
                    failureCount = 0
                    limitRevision.value += 1
                } catch (error: CancellationException) {
                    throw error
                } catch (error: Exception) {
                    when (val failure = LocationPauseFailure.classify(error)) {
                        is LocationPauseFailure.Refused -> {
                            mutex.withLock {
                                tracker = tracker.changeWasRefused(change)
                                hasPendingPause.value = tracker.hasPendingChange
                                pause.value = tracker.pause?.takeIf { each -> each.isInEffect(Instant.now()) }
                                accountKey?.let { key -> save(key) }
                            }
                            pauseRefusal.value = failure.reason
                        }
                        LocationPauseFailure.RetryLater -> {
                            failureCount += 1
                            delay((LocationPauseRetry.delaySeconds(failureCount) * 1000).toLong())
                        }
                    }
                }
            }
        }
    }

    override fun clearPauseRefusal() {
        pauseRefusal.value = null
    }

    override fun learnPause(serverPause: LocationPause?) {
        val key = accountKey ?: return
        scope.launch {
            mutex.withLock {
                loadOutbox(key)
                tracker = tracker.learning(serverPause, Instant.now())
                pause.value = tracker.pause?.takeIf { each -> each.isInEffect(Instant.now()) }
                hasPendingPause.value = tracker.hasPendingChange
                save(key)
            }
            if (tracker.hasPendingChange) sendPauseChange()
        }
    }

    // The person's places

    override fun placesDidChange() {
        scope.launch { chooseWatchedPlaces() }
    }

    /** Chooses the places to watch for arrivals (the person's named and labeled places and the stores their lists need) and registers them as geofences. */
    @SuppressLint("MissingPermission")
    private suspend fun chooseWatchedPlaces() {
        val asked = connection ?: return
        if (!recording.value || permissionState.value == LocationPermissionState.Undecided || permissionState.value == LocationPermissionState.Denied) return
        val near = live.value
        val places = runCatching { asked.locationPlaces(limitCount = 200).places }.getOrNull() ?: return
        val stores = runCatching { asked.watchedStores(near?.latitudeDegrees, near?.longitudeDegrees) }.getOrDefault(emptyList())
        val chosen = LocationPlaceWatch.choose(places, stores, near?.latitudeDegrees, near?.longitudeDegrees)
        val key = asked.accountKey
        val open = mutex.withLock { loadOutbox(key).openRegionVisits.keys.mapNotNull { identifier -> watchedPlaces.firstOrNull { place -> place.identifier == identifier } } }
        watchedPlaces = LocationPlaceWatch.keepOpen(chosen, open, near?.latitudeDegrees, near?.longitudeDegrees)
        val client = LocationServices.getGeofencingClient(context)
        client.removeGeofences(geofencePendingIntent()).await()
        if (watchedPlaces.isEmpty() || !isGranted(Manifest.permission.ACCESS_FINE_LOCATION)) return
        val request = GeofencingRequest.Builder()
            .setInitialTrigger(0)
            .addGeofences(
                watchedPlaces.map { place ->
                    Geofence.Builder().setRequestId(place.identifier).setCircularRegion(place.latitudeDegrees, place.longitudeDegrees, place.radiusMeters.toFloat())
                        .setExpirationDuration(Geofence.NEVER_EXPIRE).setTransitionTypes(Geofence.GEOFENCE_TRANSITION_ENTER or Geofence.GEOFENCE_TRANSITION_EXIT).build()
                },
            ).build()
        runCatching { client.addGeofences(request, geofencePendingIntent()).await() }
    }

    private fun geofencePendingIntent(): PendingIntent = PendingIntent.getBroadcast(
        context, 0, Intent(context, LocationGeofenceReceiver::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE,
    )

    // The setup and erasing

    override fun isSetupDismissed(accountKey: String?): Boolean = accountKey != null && preferences.getBoolean(SETUP_DISMISSED_KEY + accountKey, false)

    override fun dismissSetup(accountKey: String?) {
        if (accountKey != null) preferences.edit().putBoolean(SETUP_DISMISSED_KEY + accountKey, true).apply()
    }

    override suspend fun holdReporting() {
        mutex.withLock { isReportingHeld = true }
    }

    override fun releaseReporting(discardingFrom: Instant?, until: Instant?, isErased: Boolean) {
        val key = accountKey
        scope.launch {
            mutex.withLock {
                isReportingHeld = false
                // What the phone noticed in the range erased is thrown away, and nothing erased is sent back.
                if (isErased && key != null) {
                    loadOutbox(key).discardUnsent(discardingFrom, until)
                    save(key)
                }
            }
            report()
        }
    }

    @SuppressLint("MissingPermission")
    override suspend fun currentReading(): LivePosition? {
        live.value?.let { position -> if (Instant.now().epochSecond - position.measuredAt.epochSecond < 60) return position }
        if (!isGranted(Manifest.permission.ACCESS_FINE_LOCATION) && !isGranted(Manifest.permission.ACCESS_COARSE_LOCATION)) return null
        val client = LocationServices.getFusedLocationProviderClient(context)
        val location = runCatching { client.getCurrentLocation(Priority.PRIORITY_HIGH_ACCURACY, null).await() }.getOrNull() ?: return null
        return LivePosition(location.latitude, location.longitude, if (location.hasAccuracy()) location.accuracy.toDouble() else -1.0, Instant.ofEpochMilli(location.time))
    }

    companion object {
        private const val RECORDING_KEY = "isRecordingOn"
        private const val ASKED_KEY = "didAskLocation"
        private const val DECLINED_ALWAYS_KEY = "didDeclineAlways"
        private const val SETUP_DISMISSED_KEY = "setupDismissed."
        private const val STAY_KEY = "stay."

        @Volatile private var instance: LocationRecorder? = null

        fun get(context: Context): LocationRecorder = instance ?: synchronized(this) {
            instance ?: LocationRecorder(context.applicationContext).also { created -> instance = created }
        }
    }
}
