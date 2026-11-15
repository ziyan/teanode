package com.teanode.app.health

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.health.HealthConnectAvailability

/** Opens Health Connect's page in Google Play, where it is installed or updated; the web page when the phone has no Play app. */
fun openHealthConnectInPlayStore(context: Context) {
    val intents = listOf(HealthConnectAvailability.PLAY_STORE_URI, HealthConnectAvailability.PLAY_STORE_WEB_URL)
    for (uri in intents) {
        try {
            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(uri)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            return
        } catch (_: ActivityNotFoundException) {
            continue
        }
    }
}

@Composable
fun healthConnectInstallLabel(availability: HealthConnectAvailability): String =
    stringResource(if (availability == HealthConnectAvailability.NeedsUpdate) R.string.health_update_health_connect else R.string.health_install_health_connect)

@Composable
fun healthConnectUnavailableDescription(availability: HealthConnectAvailability): String =
    stringResource(if (availability == HealthConnectAvailability.NeedsUpdate) R.string.health_health_connect_needs_update else R.string.health_not_available_on_device)

/** The button that installs or updates Health Connect, shown where Health says it cannot read it. */
@Composable
fun HealthConnectInstallButton(availability: HealthConnectAvailability, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    OutlinedButton(onClick = { openHealthConnectInPlayStore(context) }, modifier = modifier.padding(horizontal = 16.dp)) { Text(healthConnectInstallLabel(availability)) }
}
