package com.teanode.app.calls

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.VolumeUp
import androidx.compose.material.icons.filled.Bluetooth
import androidx.compose.material.icons.filled.CallEnd
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Cameraswitch
import androidx.compose.material.icons.filled.Headphones
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.MicOff
import androidx.compose.material.icons.filled.PhoneAndroid
import androidx.compose.material.icons.filled.Subtitles
import androidx.compose.material.icons.filled.Videocam
import androidx.compose.material.icons.filled.VideocamOff
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Density
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeSnackbarHost
import com.teanode.kit.calls.ChatCallAudioRoute
import com.teanode.kit.calls.ChatCallDuration
import com.teanode.kit.calls.ChatCallTranscriptionStatus

/** What the person can do on a call; the call center's, or a screenshot's that does nothing. */
class CallActions(
    val onMinimize: () -> Unit = {},
    val onMuteChange: (isMuted: Boolean) -> Unit = {},
    val onCameraChange: (isOn: Boolean) -> Unit = {},
    val onFlipCamera: () -> Unit = {},
    val onAudioRoute: (ChatCallAudioRoute) -> Unit = {},
    val onAskTranscription: () -> Unit = {},
    val onHangUp: () -> Unit = {},
    val onFeature: (userId: String?) -> Unit = {},
)

/**
 * ChatCallScreen is shown over the whole app while the phone is on a call: the people on it, and the controls. It is
 * the Android side of the iPhone app's `ChatCallScreen`. Everything of a call fits on one screen, the people and the
 * controls with it, as the Phone app's does: the head and the controls keep their size and the people give way first,
 * so hanging up is always on screen.
 */
@Composable
fun ChatCallScreen(
    state: CallSessionState,
    availableRoutes: List<ChatCallAudioRoute>,
    actions: CallActions,
    modifier: Modifier = Modifier,
) {
    // Everything of a call fits on one screen, as the Phone app's does: the largest text sizes would push the head and the controls off it.
    val density = LocalDensity.current
    CompositionLocalProvider(LocalDensity provides Density(density.density, minOf(density.fontScale, FONT_SCALE_MOST))) {
        ChatCallScreenContent(state, availableRoutes, actions, modifier)
    }
}

/** The largest text the call screen is drawn at, about the iPhone's xxxLarge. */
private const val FONT_SCALE_MOST = 1.3f

@Composable
private fun ChatCallScreenContent(
    state: CallSessionState,
    availableRoutes: List<ChatCallAudioRoute>,
    actions: CallActions,
    modifier: Modifier,
) {
    var isConfirmingTranscription by rememberSaveable { mutableStateOf(false) }
    // The call whose transcription notice the person closed.
    var noticeClosedCallId by rememberSaveable { mutableStateOf<String?>(null) }
    val transcriptionStatus = state.call?.let { call -> ChatCallTranscriptionStatus.of(call, state.ownUserId) } ?: ChatCallTranscriptionStatus.NotAsked
    Scaffold(
        modifier = modifier,
        containerColor = MaterialTheme.colorScheme.background,
        snackbarHost = { TeaNodeSnackbarHost() },
        contentWindowInsets = WindowInsets.safeDrawing,
    ) { contentPadding ->
        Column(
            Modifier.fillMaxSize().padding(contentPadding).padding(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            CallHeader(state, actions.onMinimize)
            when (transcriptionStatus) {
                ChatCallTranscriptionStatus.NotAsked -> Unit
                is ChatCallTranscriptionStatus.Transcribing ->
                    if (noticeClosedCallId != null && noticeClosedCallId == state.callId) {
                        TranscribingMark()
                    } else {
                        TranscriptionNotice(transcriptionStatus.askerName, transcriptionStatus.isAskedByPerson, onClose = { noticeClosedCallId = state.callId })
                    }
            }
            CallTiles(state, actions.onFeature, Modifier.weight(1f).fillMaxWidth())
            CallControls(state, availableRoutes, transcriptionStatus.canAsk, actions, onTranscribe = { isConfirmingTranscription = true })
        }
    }
    if (isConfirmingTranscription) {
        ConfirmationDialog(
            title = stringResource(R.string.calls_transcribe_title),
            text = stringResource(R.string.calls_transcribe_message),
            confirmLabel = stringResource(R.string.calls_transcribe),
            onConfirm = {
                isConfirmingTranscription = false
                actions.onAskTranscription()
            },
            onDismiss = { isConfirmingTranscription = false },
        )
    }
}

@Composable
private fun CallHeader(state: CallSessionState, onMinimize: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        // Puts the call away to the bar at the top of the app, to use the rest of it while on the call.
        IconButton(onClick = onMinimize) {
            Icon(Icons.Filled.KeyboardArrowDown, contentDescription = stringResource(R.string.calls_minimize))
        }
        Column(
            Modifier.weight(1f).semantics(mergeDescendants = true) {},
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(state.title, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            CallStatusLine(state, MaterialTheme.typography.bodyMedium)
        }
        // As wide as the button, so the title stays in the middle.
        Box(Modifier.size(48.dp))
    }
}

/** How a call stands, under its title: how long it has gone once answered, or that it is connecting or calling. */
@Composable
fun CallStatusLine(state: CallSessionState, style: androidx.compose.ui.text.TextStyle, modifier: Modifier = Modifier) {
    val answeredAt = state.call?.answeredEpochMS
    val text = if (state.phase == CallPhase.Connected && answeredAt != null) {
        ChatCallDuration.text(rememberCallNowMS() - answeredAt)
    } else {
        when (state.phase) {
            CallPhase.Connecting -> stringResource(R.string.calls_connecting)
            CallPhase.Reconnecting -> stringResource(R.string.calls_reconnecting)
            CallPhase.Connected -> if (state.isOutgoing) stringResource(R.string.calls_calling) else ""
        }
    }
    Text(text, style = style, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = modifier, maxLines = 1)
}

/** The mark under the title while a call is transcribed, once its notice was closed. */
@Composable
private fun TranscribingMark() {
    Row(
        Modifier.clip(RoundedCornerShape(50)).background(MaterialTheme.colorScheme.surfaceContainerHigh).padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Icon(Icons.Filled.Subtitles, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(stringResource(R.string.calls_transcribing), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/**
 * TranscriptionNotice tells the person the call is being transcribed, and who turned it on. Nobody is asked: it closes
 * with its button.
 */
@Composable
fun TranscriptionNotice(askerName: String, isAskedByPerson: Boolean, onClose: () -> Unit, modifier: Modifier = Modifier) {
    val title = when {
        isAskedByPerson -> stringResource(R.string.calls_you_turned_on_transcription)
        else -> stringResource(R.string.calls_turned_on_transcription, askerName.ifEmpty { stringResource(R.string.calls_somebody) })
    }
    Surface(modifier.fillMaxWidth(), shape = RoundedCornerShape(18.dp), color = MaterialTheme.colorScheme.surfaceContainer) {
        Row(Modifier.padding(start = 16.dp, top = 12.dp, bottom = 12.dp, end = 4.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Icon(Icons.Filled.Subtitles, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 2.dp))
            Column(Modifier.weight(1f).semantics(mergeDescendants = true) {}, verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(title, style = MaterialTheme.typography.titleSmall)
                Text(stringResource(R.string.calls_transcription_notice), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            IconButton(onClick = onClose) { Icon(Icons.Filled.Close, contentDescription = stringResource(R.string.common_dismiss)) }
        }
    }
}

@Composable
private fun CallControls(
    state: CallSessionState,
    availableRoutes: List<ChatCallAudioRoute>,
    canAskTranscription: Boolean,
    actions: CallActions,
    onTranscribe: () -> Unit,
) {
    // The controls break into two rows when they do not fit in one.
    FlowRow(
        Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(12.dp, Alignment.CenterHorizontally),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        CallControlButton(
            icon = if (state.isMuted) Icons.Filled.MicOff else Icons.Filled.Mic,
            label = stringResource(if (state.isMuted) R.string.calls_unmute else R.string.calls_mute),
            isActive = state.isMuted,
            onClick = { actions.onMuteChange(!state.isMuted) },
        )
        CallControlButton(
            icon = if (state.isCameraOn) Icons.Filled.Videocam else Icons.Filled.VideocamOff,
            label = stringResource(R.string.calls_camera),
            stateText = stringResource(if (state.isCameraOn) R.string.calls_on else R.string.calls_off),
            isActive = state.isCameraOn,
            onClick = { actions.onCameraChange(!state.isCameraOn) },
        )
        CallControlButton(icon = Icons.Filled.Cameraswitch, label = stringResource(R.string.calls_flip_camera), isEnabled = state.isCameraOn, onClick = actions.onFlipCamera)
        // Where the sound goes: the phone, the speaker or a headset.
        CallAudioRouteButton(state.audioRoute, availableRoutes, actions.onAudioRoute)
        CallControlButton(icon = Icons.Filled.Subtitles, label = stringResource(R.string.calls_transcribe), isEnabled = canAskTranscription, onClick = onTranscribe)
        CallControlButton(icon = Icons.Filled.CallEnd, label = stringResource(R.string.calls_hang_up), isDestructive = true, onClick = actions.onHangUp)
    }
}

/** CallControlButton is one round control of the call screen. */
@Composable
fun CallControlButton(
    icon: ImageVector,
    label: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    /** What a screen reader says of its state ("On"), when it has one. */
    stateText: String = "",
    isActive: Boolean = false,
    isDestructive: Boolean = false,
    isEnabled: Boolean = true,
) {
    val container = when {
        isDestructive -> MaterialTheme.colorScheme.error
        isActive -> MaterialTheme.colorScheme.primaryContainer
        else -> MaterialTheme.colorScheme.surfaceContainerHigh
    }
    val content = when {
        isDestructive -> MaterialTheme.colorScheme.onError
        isActive -> MaterialTheme.colorScheme.onPrimaryContainer
        else -> MaterialTheme.colorScheme.onSurface
    }
    Surface(
        onClick = onClick,
        enabled = isEnabled,
        shape = CircleShape,
        color = container,
        contentColor = content,
        modifier = modifier.size(56.dp).semantics {
            contentDescription = label
            if (stateText.isNotEmpty()) stateDescription = stateText
        },
    ) {
        Box(contentAlignment = Alignment.Center, modifier = Modifier.size(56.dp)) {
            Icon(icon, contentDescription = null, modifier = Modifier.size(26.dp), tint = if (isEnabled) content else content.copy(alpha = 0.38f))
        }
    }
}

/** The symbol of an audio route. */
fun iconOf(route: ChatCallAudioRoute): ImageVector = when (route) {
    ChatCallAudioRoute.Phone -> Icons.Filled.PhoneAndroid
    ChatCallAudioRoute.Speaker -> Icons.AutoMirrored.Filled.VolumeUp
    ChatCallAudioRoute.Headphones -> Icons.Filled.Headphones
    is ChatCallAudioRoute.Bluetooth -> Icons.Filled.Bluetooth
    is ChatCallAudioRoute.Other -> Icons.AutoMirrored.Filled.VolumeUp
}

/** The route's name, as the phone's own list of routes calls it. */
@Composable
fun nameOf(route: ChatCallAudioRoute): String = when (route) {
    ChatCallAudioRoute.Phone -> stringResource(R.string.calls_route_phone)
    ChatCallAudioRoute.Speaker -> stringResource(R.string.calls_route_speaker)
    ChatCallAudioRoute.Headphones -> stringResource(R.string.calls_route_headphones)
    is ChatCallAudioRoute.Bluetooth -> route.deviceName.ifEmpty { stringResource(R.string.calls_route_bluetooth) }
    is ChatCallAudioRoute.Other -> route.deviceName.ifEmpty { stringResource(R.string.calls_route_other) }
}

/**
 * CallAudioRouteButton chooses where a call's sound goes: the phone, the speaker, or a headset, from a menu of the
 * routes the phone offers now. It is drawn as the call's other buttons are, with the route's symbol, and follows the
 * route as it changes: headphones put in or taken out.
 */
@Composable
fun CallAudioRouteButton(route: ChatCallAudioRoute, availableRoutes: List<ChatCallAudioRoute>, onChoose: (ChatCallAudioRoute) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    Box {
        CallControlButton(
            icon = iconOf(route),
            label = stringResource(R.string.calls_audio_output),
            stateText = nameOf(route),
            isActive = route != ChatCallAudioRoute.Phone,
            onClick = { isOpen = true },
        )
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            (availableRoutes.ifEmpty { listOf(route) }).forEach { offered ->
                DropdownMenuItem(
                    text = { Text(nameOf(offered), fontWeight = if (offered == route) FontWeight.SemiBold else FontWeight.Normal) },
                    leadingIcon = { Icon(iconOf(offered), contentDescription = null) },
                    trailingIcon = { if (offered == route) Icon(Icons.Filled.Check, contentDescription = null) },
                    onClick = {
                        isOpen = false
                        onChoose(offered)
                    },
                )
            }
        }
    }
}
