package com.teanode.app.location.recording

import android.Manifest
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.navigation.NamedNavArgument
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavDeepLink
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.location.DemoLocationRecording
import com.teanode.app.location.AlwaysExplanation
import com.teanode.app.location.LocalLocationRecording
import com.teanode.app.location.LocationPermissionRequest
import com.teanode.app.session.LocalAppSession

/** Gives the screens inside the recorder that follows the phone, or a recorder that does nothing when the session is the demo. */
@Composable
fun ProvideLocationRecording(content: @Composable () -> Unit) {
    val session = LocalAppSession.current
    val context = LocalContext.current
    val recording = remember(session.isDemo) { if (session.isDemo) DemoLocationRecording() else LocationRecorder.get(context) }
    CompositionLocalProvider(LocalLocationRecording provides recording) {
        content()
        if (recording is LocationRecorder) LocationPermissionAsker(recording)
    }
}

/**
 * Shows the system's questions the recorder asks for, one at a time. Android asks for "while the app is open" first
 * and for "all the time" in a second question (the system's settings page from Android 11), so the second is preceded by
 * a screen saying why, and a person who said "Not now" there is sent to the settings instead of asked again.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun LocationPermissionAsker(recorder: LocationRecorder) {
    val context = LocalContext.current
    var isExplaining by remember { mutableStateOf(false) }
    val answered = { recorder.permissionAnswered() }
    val foregroundLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { answered() }
    val singleLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { answered() }
    val settingsLauncher = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) { answered() }
    LaunchedEffect(recorder) {
        recorder.permissionRequests.collect { request ->
            when (request) {
                LocationPermissionRequest.Foreground -> foregroundLauncher.launch(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION))
                LocationPermissionRequest.ExplainAlways -> isExplaining = true
                LocationPermissionRequest.Background -> if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) singleLauncher.launch(Manifest.permission.ACCESS_BACKGROUND_LOCATION)
                LocationPermissionRequest.Activity -> if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) singleLauncher.launch(Manifest.permission.ACTIVITY_RECOGNITION)
                LocationPermissionRequest.Settings -> settingsLauncher.launch(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", context.packageName, null)))
            }
        }
    }
    if (isExplaining) {
        ModalBottomSheet(onDismissRequest = { isExplaining = false; recorder.declineAlwaysExplanation() }) {
            AlwaysExplanation(
                onContinue = { isExplaining = false; recorder.requestAlways() },
                onNotNow = { isExplaining = false; recorder.declineAlwaysExplanation() },
            )
        }
    }
}

/** A route of Location, drawn inside [ProvideLocationRecording]. */
fun NavGraphBuilder.locationComposable(
    route: String, arguments: List<NamedNavArgument> = emptyList(), deepLinks: List<NavDeepLink> = emptyList(), content: @Composable (NavBackStackEntry) -> Unit,
) = composable(route, arguments, deepLinks) { entry -> ProvideLocationRecording { content(entry) } }
