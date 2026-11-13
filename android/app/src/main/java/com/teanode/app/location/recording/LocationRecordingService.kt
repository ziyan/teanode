package com.teanode.app.location.recording

import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import com.google.android.gms.location.LocationCallback
import com.google.android.gms.location.LocationRequest
import com.google.android.gms.location.LocationResult
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import com.teanode.app.MainActivity
import com.teanode.app.R
import java.time.Instant

/**
 * LocationRecordingService keeps the phone's location updates coming while the app is closed, under a notification that
 * says so (Android requires one of a foreground service). It hands each position to [LocationRecorder], which decides what
 * to keep. It runs only while recording is on and the person has allowed location.
 */
class LocationRecordingService : Service() {
    private val callback = object : LocationCallback() {
        override fun onLocationResult(result: LocationResult) {
            val recorder = LocationRecorder.get(applicationContext)
            for (location in result.locations) {
                recorder.onLocation(
                    location.latitude, location.longitude, if (location.hasAccuracy()) location.accuracy.toDouble() else 100.0,
                    if (location.hasSpeed()) location.speed.toDouble() else null, if (location.hasAltitude()) location.altitude else null,
                    if (location.hasBearing()) location.bearing.toDouble() else null, Instant.ofEpochMilli(location.time),
                )
            }
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    @SuppressLint("MissingPermission")
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(CHANNEL_ID, getString(R.string.location_recording_channel), NotificationManager.IMPORTANCE_LOW))
        val opening = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val notification: Notification = NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.mipmap.ic_launcher).setContentTitle(getString(R.string.location_recording_notification_title))
            .setContentText(getString(R.string.location_recording_notification_text)).setContentIntent(opening).setOngoing(true).setSilent(true).build()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION) else startForeground(NOTIFICATION_ID, notification)
        val request = LocationRequest.Builder(Priority.PRIORITY_BALANCED_POWER_ACCURACY, 30_000L).setMinUpdateDistanceMeters(15f).setMinUpdateIntervalMillis(10_000L).build()
        val client = LocationServices.getFusedLocationProviderClient(this)
        client.removeLocationUpdates(callback)
        runCatching { client.requestLocationUpdates(request, callback, mainLooper) }
        return START_STICKY
    }

    override fun onDestroy() {
        LocationServices.getFusedLocationProviderClient(this).removeLocationUpdates(callback)
        super.onDestroy()
    }

    companion object {
        private const val CHANNEL_ID = "location-recording"
        private const val NOTIFICATION_ID = 7301

        fun start(context: Context) {
            runCatching { ContextCompat.startForegroundService(context, Intent(context, LocationRecordingService::class.java)) }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, LocationRecordingService::class.java))
        }
    }
}
