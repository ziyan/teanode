package com.teanode.app.recordings

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.accessSession
import com.teanode.kit.photos.VideoControls

/**
 * The recorder (ios/TeaNode/Sources/RecordingCaptureView.swift): how long it has run, a meter of the microphone's level,
 * pause and resume, and stop. The capture itself runs in [RecordingCaptureService], so it goes on with the screen off and
 * after this screen is left; coming back shows it as it is. [previewState] draws a given state (the screenshots).
 */
@Composable
fun RecordingCaptureScreen(onBack: () -> Unit, previewState: RecordingCaptureState? = null) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val liveState by RecordingCaptureService.state.collectAsState()
    val state = previewState ?: liveState
    LaunchedEffect(previewState) {
        if (previewState == null && !RecordingCaptureService.state.value.isRecording) {
            val userId = runCatching { connection.accessSession().userId }.getOrNull()
            RecordingCaptureService.start(context, connection.accountKey, userId, "")
        }
    }
    // A stopped capture leaves the screen: its recording is in the list, uploading.
    LaunchedEffect(state.isRecording, previewState) { if (previewState == null && !state.isRecording && state.localRecordingId == null && state.problemMessageId == null) { kotlinx.coroutines.delay(1500); if (!RecordingCaptureService.state.value.isRecording) onBack() } }
    TeaNodeScreen(title = stringResource(R.string.recordings_recording_title), onBack = onBack) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(32.dp, Alignment.CenterVertically)) {
            Text(VideoControls.timeText(state.elapsedMS / 1000.0), style = MaterialTheme.typography.displayLarge)
            Text(
                stringResource(if (state.isPaused) R.string.recordings_status_paused else if (state.isRecording) R.string.recordings_status_recording else R.string.recordings_starting),
                color = if (state.isPaused || !state.isRecording) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.error, style = MaterialTheme.typography.titleMedium,
            )
            LevelMeter(state.levelBars, Modifier.fillMaxWidth().height(72.dp))
            state.problemMessageId?.let { messageId -> Text(stringResource(messageId), color = MaterialTheme.colorScheme.error) }
            Row(horizontalArrangement = Arrangement.spacedBy(24.dp), verticalAlignment = Alignment.CenterVertically) {
                FilledIconButton(
                    onClick = { RecordingCaptureService.send(context, if (state.isPaused) RecordingCaptureService.ACTION_RESUME else RecordingCaptureService.ACTION_PAUSE) }, enabled = state.isRecording,
                    modifier = Modifier.size(72.dp), colors = IconButtonDefaults.filledTonalIconButtonColors(),
                ) { Icon(if (state.isPaused) Icons.Filled.Mic else Icons.Filled.Pause, contentDescription = stringResource(if (state.isPaused) R.string.recordings_resume else R.string.recordings_pause), modifier = Modifier.size(36.dp)) }
                FilledIconButton(
                    onClick = { RecordingCaptureService.send(context, RecordingCaptureService.ACTION_STOP); onBack() }, enabled = state.isRecording, modifier = Modifier.size(72.dp),
                    colors = IconButtonDefaults.filledIconButtonColors(containerColor = MaterialTheme.colorScheme.error, contentColor = MaterialTheme.colorScheme.onError),
                ) { Icon(Icons.Filled.Stop, contentDescription = stringResource(R.string.recordings_stop), modifier = Modifier.size(36.dp)) }
            }
            OutlinedButton(onClick = onBack) { Text(stringResource(R.string.recordings_close)) }
        }
    }
}

/** The last few seconds of the microphone's level as bars, the newest at the right. */
@Composable
fun LevelMeter(bars: List<Double>, modifier: Modifier = Modifier) {
    val color = MaterialTheme.colorScheme.primary
    val quiet = MaterialTheme.colorScheme.surfaceVariant
    Canvas(modifier) {
        val slot = size.width / maxOf(1, bars.size)
        bars.forEachIndexed { index, level ->
            val barHeight = maxOf(4f, (size.height * level).toFloat())
            drawRoundRect(
                if (level > 0.02) color else quiet, topLeft = Offset(index * slot + slot * 0.15f, (size.height - barHeight) / 2), size = Size(slot * 0.7f, barHeight), cornerRadius = CornerRadius(slot * 0.35f),
            )
        }
    }
}
