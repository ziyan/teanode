package com.teanode.app.location.recording

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import com.google.android.gms.location.Geofence
import com.google.android.gms.location.GeofencingEvent
import java.time.Instant

/** Hears that the phone entered or left a place the recorder watches, and tells the recorder. */
class LocationGeofenceReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val event = GeofencingEvent.fromIntent(intent) ?: return
        if (event.hasError()) return
        val isEntering = event.geofenceTransition == Geofence.GEOFENCE_TRANSITION_ENTER
        val isLeaving = event.geofenceTransition == Geofence.GEOFENCE_TRANSITION_EXIT
        if (!isEntering && !isLeaving) return
        val recorder = LocationRecorder.get(context)
        for (geofence in event.triggeringGeofences.orEmpty()) recorder.onGeofence(geofence.requestId, isEntering, Instant.now())
    }
}

/** Starts recording again after the phone restarts, when it was on. */
class LocationBootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        LocationRecorder.get(context).applyRecording()
    }
}

/** Hears Android's activity transitions (driving, cycling, running, walking, still) and tells the recorder. */
class LocationActivityReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (!com.google.android.gms.location.ActivityTransitionResult.hasResult(intent)) return
        val result = com.google.android.gms.location.ActivityTransitionResult.extractResult(intent) ?: return
        val recorder = LocationRecorder.get(context)
        val now = Instant.now()
        val nowElapsedNanos = android.os.SystemClock.elapsedRealtimeNanos()
        for (event in result.transitionEvents) {
            val happened = com.teanode.kit.location.LocationMotion.transitionInstant(now, nowElapsedNanos, event.elapsedRealTimeNanos)
            recorder.onActivityTransition(event.activityType, event.transitionType == com.google.android.gms.location.ActivityTransition.ACTIVITY_TRANSITION_ENTER, happened)
        }
    }
}
