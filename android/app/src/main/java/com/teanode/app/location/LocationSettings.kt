package com.teanode.app.location

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.location.LocationPauseChoice
import kotlinx.coroutines.launch

/** Location's settings: recording, pausing, what the phone allows, and erasing what was recorded. */
@Composable
fun LocationSettingsScreen(model: LocationModel, onBack: () -> Unit) {
    val recording = LocalLocationRecording.current
    val text = rememberLocationText()
    val scope = rememberCoroutineScope()
    val isRecordingOn by recording.isRecordingOn.collectAsState()
    val unsentCount by recording.unsentCount.collectAsState()
    val activePause by recording.activePause.collectAsState()
    val hasPendingPauseChange by recording.hasPendingPauseChange.collectAsState()
    val refusalText by recording.refusalText.collectAsState()
    val permission by recording.permission.collectAsState()
    val isMotionAuthorized by recording.isMotionAuthorized.collectAsState()
    val pauseRefusal by recording.pauseRefusalText.collectAsState()
    val missing = missingPermission(permission, recording)
    var isConfirmingErase by remember { mutableStateOf(false) }
    var isErasing by remember { mutableStateOf(false) }
    var isPauseMenuOpen by remember { mutableStateOf(false) }
    val recordingResumed = stringResource(R.string.location_recording_resumed)
    val recordingPaused = stringResource(R.string.location_recording_paused)

    // The server refused a pause or a resume made here, which the phone had already applied and has now taken back.
    LaunchedEffect(pauseRefusal) {
        val refusal = pauseRefusal ?: return@LaunchedEffect
        model.say(refusal, true)
        recording.clearPauseRefusal()
    }
    LocationNoticeEffect(model)

    TeaNodeScreen(title = stringResource(R.string.location_settings_title), onBack = onBack) { padding ->
        LazyColumn(Modifier.padding(padding).fillMaxSize()) {
            val refusal = refusalText
            if (refusal != null) {
                item { Warning(stringResource(R.string.location_server_did_not_take, refusal)) }
                item { Footer(stringResource(R.string.location_turn_off_and_on)) }
            }
            if (missing != null) {
                item {
                    Warning(missing.explanation)
                    TextButton(onClick = missing.perform, modifier = Modifier.padding(horizontal = 8.dp)) {
                        Icon(missing.icon, contentDescription = null)
                        Text(missing.actionTitle, Modifier.padding(start = 8.dp))
                    }
                    HorizontalDivider()
                }
            }
            item {
                Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.location_record_where_i_go), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                    Switch(checked = isRecordingOn, onCheckedChange = { isOn -> recording.setRecording(isOn) })
                }
                if (unsentCount > 0) {
                    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
                        Text(stringResource(R.string.location_waiting_to_be_sent), Modifier.weight(1f))
                        Text(unsentCount.toString(), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                Footer(stringResource(R.string.location_record_footer))
            }
            item {
                val pause = activePause
                if (pause != null) {
                    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.PauseCircle, contentDescription = null)
                        Text(text.pausedUntil(pause), Modifier.padding(start = 12.dp))
                    }
                    TextButton(onClick = { recording.resumeRecording(); model.say(recordingResumed, false) }, modifier = Modifier.padding(horizontal = 8.dp)) {
                        Icon(Icons.Outlined.PlayCircle, contentDescription = null)
                        Text(stringResource(R.string.location_resume_recording), Modifier.padding(start = 8.dp))
                    }
                } else {
                    Box {
                        TextButton(onClick = { isPauseMenuOpen = true }, modifier = Modifier.padding(horizontal = 8.dp)) {
                            Icon(Icons.Outlined.PauseCircle, contentDescription = null)
                            Text(stringResource(R.string.location_pause_recording), Modifier.padding(start = 8.dp))
                        }
                        DropdownMenu(expanded = isPauseMenuOpen, onDismissRequest = { isPauseMenuOpen = false }) {
                            LocationPauseChoice.entries.forEach { choice ->
                                DropdownMenuItem(text = { Text(text.pauseChoiceTitle(choice)) }, onClick = { isPauseMenuOpen = false; recording.pauseRecording(choice); model.say(recordingPaused, false) })
                            }
                        }
                    }
                }
                Footer(
                    when {
                        pause != null && pause.isOpenEnded -> stringResource(R.string.location_pause_footer_open)
                        pause != null -> stringResource(R.string.location_pause_footer_timed)
                        else -> stringResource(R.string.location_pause_footer_off)
                    },
                )
                if (hasPendingPauseChange) Footer(stringResource(R.string.location_server_not_told_yet))
                HorizontalDivider()
            }
            if (recording.isMotionAvailable) {
                item {
                    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                        Text(stringResource(R.string.location_allow_motion_title), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                        Text(stringResource(if (isMotionAuthorized) R.string.location_on else R.string.location_off), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    if (!isMotionAuthorized) {
                        TextButton(onClick = { recording.requestMotionPermission() }, modifier = Modifier.padding(horizontal = 8.dp)) {
                            Icon(Icons.Outlined.Settings, contentDescription = null)
                            Text(stringResource(R.string.location_allow_motion), Modifier.padding(start = 8.dp))
                        }
                    }
                    Footer(stringResource(R.string.location_motion_footer))
                    HorizontalDivider()
                }
            }
            item {
                TextButton(onClick = { isConfirmingErase = true }, enabled = !isErasing, modifier = Modifier.padding(horizontal = 8.dp, vertical = 8.dp)) {
                    Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error)
                    Text(stringResource(R.string.location_erase_positions_and_visits), Modifier.padding(start = 8.dp), color = MaterialTheme.colorScheme.error)
                }
                Footer(stringResource(R.string.location_erase_footer))
            }
        }
    }
    if (isConfirmingErase) {
        ConfirmationDialog(
            title = stringResource(R.string.location_erase_everything_title), text = stringResource(R.string.location_erase_everything_explanation),
            confirmLabel = stringResource(R.string.location_erase_positions_and_visits), isDestructive = true, onDismiss = { isConfirmingErase = false },
            onConfirm = { isConfirmingErase = false; scope.launch { isErasing = true; model.eraseHistory(); isErasing = false } },
        )
    }
}

private class MissingLocationPermission(val explanation: String, val actionTitle: String, val icon: androidx.compose.ui.graphics.vector.ImageVector, val perform: () -> Unit)

@Composable
private fun missingPermission(permission: LocationPermissionState, recording: LocationRecording): MissingLocationPermission? = when (permission) {
    LocationPermissionState.Undecided -> MissingLocationPermission(
        stringResource(R.string.location_not_allowed_yet), stringResource(R.string.location_allow_location), Icons.Outlined.LocationOn,
    ) { recording.requestPermission() }
    LocationPermissionState.Denied -> MissingLocationPermission(
        stringResource(R.string.location_turned_off_for_app), stringResource(R.string.location_open_settings), Icons.Outlined.Settings,
    ) { recording.openSystemSettings() }
    LocationPermissionState.WhileUsing -> MissingLocationPermission(
        stringResource(R.string.location_only_while_open), stringResource(R.string.location_open_settings), Icons.Outlined.Settings,
    ) { recording.openSystemSettings() }
    LocationPermissionState.Always -> null
}

@Composable
private fun Warning(message: String) {
    Row(Modifier.fillMaxWidth().padding(16.dp), verticalAlignment = Alignment.Top) {
        Icon(Icons.Outlined.Warning, contentDescription = null)
        Text(message, Modifier.padding(start = 12.dp), style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
private fun Footer(message: String) {
    Text(message, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** What the phone shows before the person lets Location record while the app is closed: why, then the system's question. */
@Composable
fun AlwaysExplanation(onContinue: () -> Unit, onNotNow: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = androidx.compose.foundation.layout.Arrangement.spacedBy(16.dp)) {
        Icon(Icons.Outlined.LocationOn, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(8.dp))
        Text(stringResource(R.string.location_record_where_you_go), style = MaterialTheme.typography.titleLarge)
        Text(stringResource(R.string.location_always_explanation), style = MaterialTheme.typography.bodyMedium)
        androidx.compose.material3.Button(onClick = onContinue, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.location_continue)) }
        androidx.compose.material3.OutlinedButton(onClick = onNotNow, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.location_not_now)) }
    }
}
