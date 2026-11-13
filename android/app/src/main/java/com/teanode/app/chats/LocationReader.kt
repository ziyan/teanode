package com.teanode.app.chats

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationManager
import android.os.Build
import android.os.CancellationSignal
import androidx.core.content.ContextCompat
import com.teanode.app.R
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withTimeoutOrNull
import java.time.Instant
import kotlin.coroutines.resume

/** LocationReader reads where the phone is, once, for the agent asking where the person is. */
object LocationReader {
    fun hasPermission(context: Context): Boolean =
        ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED ||
            ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED

    /** The reading for a screenshot: the phone is at the demo park, where the demo's nearby places are. */
    fun demoReading(): LocationReading = LocationReading(10.5040, 20.5060, 10.0, Instant.now())

    @SuppressLint("MissingPermission")
    suspend fun current(context: Context): LocationReading {
        if (!hasPermission(context)) return LocationReading(problemText = context.getString(R.string.chats_location_not_allowed))
        val manager = context.getSystemService(Context.LOCATION_SERVICE) as LocationManager
        val providers = manager.getProviders(true)
        // A reading under a minute old answers without asking the phone again.
        val recent = providers.mapNotNull { provider -> runCatching { manager.getLastKnownLocation(provider) }.getOrNull() }
            .filter { location -> System.currentTimeMillis() - location.time < 60_000 }.minByOrNull { location -> location.accuracy }
        if (recent != null) return reading(recent)
        val provider = listOf(LocationManager.FUSED_PROVIDER, LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER).firstOrNull { name -> name in providers }
            ?: return LocationReading(problemText = context.getString(R.string.chats_location_unknown))
        val found = withTimeoutOrNull(20_000) {
            suspendCancellableCoroutine<Location?> { waiting ->
                val signal = CancellationSignal()
                waiting.invokeOnCancellation { signal.cancel() }
                if (Build.VERSION.SDK_INT >= 30) {
                    manager.getCurrentLocation(provider, signal, ContextCompat.getMainExecutor(context)) { location -> waiting.resume(location) }
                } else {
                    @Suppress("DEPRECATION")
                    manager.requestSingleUpdate(provider, { location -> waiting.resume(location) }, null)
                }
            }
        }
        return if (found != null) reading(found) else LocationReading(problemText = context.getString(R.string.chats_location_not_in_time))
    }

    private fun reading(location: Location): LocationReading =
        LocationReading(location.latitude, location.longitude, location.accuracy.toDouble(), Instant.ofEpochMilli(location.time))
}
