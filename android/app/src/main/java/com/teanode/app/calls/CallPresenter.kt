package com.teanode.app.calls

import android.Manifest
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import com.teanode.app.ui.components.LocalSnackbar

/** The calls of the phone, where the signed-in app is drawn; null in a screenshot, which draws the call screens from state it holds. */
val LocalCallCenter = staticCompositionLocalOf<CallCenter?> { null }

/** What the call center does for the screens, as [CallActions]. */
fun CallCenter.asActions(): CallActions = CallActions(
    onMinimize = { setMinimized(!(session.value?.isMinimized ?: false)) },
    onMuteChange = { isMuted -> setMuted(isMuted) },
    onCameraChange = { isOn -> setCamera(isOn) },
    onFlipCamera = { flipCamera() },
    onAudioRoute = { route -> setAudioRoute(route) },
    onAskTranscription = { askTranscription() },
    onHangUp = { hangUp() },
    onFeature = { userId -> setFeatured(userId) },
)

/**
 * CallPresenter shows the call screen over the app while the phone is on a call, or the bar of a call put away above
 * it, and tells what went wrong when a call could not be started. It is the Android side of the iPhone app's
 * `ChatCallPresenter`.
 */
@Composable
fun CallPresenter(content: @Composable () -> Unit) {
    val center = LocalCallCenter.current
    if (center == null) {
        content()
        return
    }
    val session by center.session.collectAsState()
    val availableRoutes by center.audioRoutes.collectAsState()
    val snackbar = LocalSnackbar.current
    LaunchedEffect(center) { center.problems.collect { text -> snackbar.showProblem(text) } }
    val state = session
    val actions = remember(center) { center.asActions() }
    // The bar of a call put away takes its own row above the app, so the app's bars move down rather than hiding behind it.
    Column(Modifier.fillMaxSize()) {
        if (state != null && state.isMinimized) CallBar(state, actions)
        Box(Modifier.weight(1f)) { content() }
    }
    if (state != null && !state.isMinimized) {
        BackHandler { center.setMinimized(true) }
        Surface(Modifier.fillMaxSize()) { ChatCallScreen(state, availableRoutes, actions) }
    }
}

/**
 * Asks for the microphone, and the camera for a video call, then goes on: the permission prompt is the person's to
 * answer before a call starts. A refusal says so in the call's own words.
 */
@Composable
fun rememberCallPermissionRequester(): (needsCamera: Boolean, proceed: () -> Unit) -> Unit {
    val context = LocalContext.current
    val center = LocalCallCenter.current
    var pendingProceed by remember { mutableStateOf<(() -> Unit)?>(null) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { grants ->
        val proceed = pendingProceed
        pendingProceed = null
        // Whatever was granted, the call center says what it cannot do without: the microphone refused ends the call at the start.
        if (proceed != null && (grants[Manifest.permission.RECORD_AUDIO] == true || center != null)) proceed()
    }
    return { needsCamera, proceed ->
        val needed = buildList {
            if (ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) != android.content.pm.PackageManager.PERMISSION_GRANTED) add(Manifest.permission.RECORD_AUDIO)
            if (needsCamera && ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) != android.content.pm.PackageManager.PERMISSION_GRANTED) add(Manifest.permission.CAMERA)
        }
        if (needed.isEmpty()) {
            proceed()
        } else {
            pendingProceed = proceed
            launcher.launch(needed.toTypedArray())
        }
    }
}
