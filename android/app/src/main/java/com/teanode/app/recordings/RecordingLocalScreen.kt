package com.teanode.app.recordings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.recordings.LocalRecording
import com.teanode.kit.recordings.RecordingLibrary
import com.teanode.kit.recordings.RecordingRowStatus

/**
 * A recording only the phone has so far (still uploading, waiting for the network, or failed): what the phone holds,
 * how far it went up, why it stopped, and retrying or removing it. Once the server has it, it opens as a recording.
 */
@Composable
fun RecordingLocalScreen(localRecordingId: String, onBack: () -> Unit, preview: LocalRecording? = null) {
    val context = LocalContext.current
    val journal = remember { RecordingJournal(context) }
    val recording = preview ?: journal.read(localRecordingId)
    var isRemoving by remember { mutableStateOf(false) }
    TeaNodeScreen(title = recording?.recordingTitle?.ifEmpty { null } ?: stringResource(R.string.recordings_local_original), onBack = onBack) { padding ->
        if (recording == null) {
            Text(stringResource(R.string.recordings_local_gone), modifier = Modifier.padding(padding).padding(16.dp))
        } else {
            val status = RecordingLibrary.status(null, recording, null)
            Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
                TeaNodeSectionHeader(stringResource(R.string.recordings_local_media))
                for (media in recording.localMedia) {
                    TeaNodeRow(
                        headline = media.originalFilename,
                        supporting = com.teanode.app.photos.bytesText(media.originalSizeBytes) + " · " + stringResource(if (media.isDurablyUploaded) R.string.recordings_local_on_server else R.string.recordings_local_on_phone),
                    )
                }
                if (status is RecordingRowStatus.Uploading) Text(stringResource(R.string.recordings_status_uploading, status.percent), modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
                recording.uploadErrorMessage?.let { message -> Text(message, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) }
                Button(onClick = { RecordingUploadScheduler.schedule(context) }, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) { Text(stringResource(R.string.recordings_retry_upload)) }
                OutlinedButton(onClick = { isRemoving = true }, modifier = Modifier.padding(horizontal = 16.dp)) { Text(stringResource(R.string.recordings_remove_from_phone), color = MaterialTheme.colorScheme.error) }
                Text(stringResource(R.string.recordings_remove_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
            }
            if (isRemoving) {
                ConfirmationDialog(
                    title = stringResource(R.string.recordings_remove_from_phone), confirmLabel = stringResource(R.string.recordings_remove_from_phone), isDestructive = true, text = stringResource(R.string.recordings_remove_text),
                    onConfirm = { isRemoving = false; journal.remove(recording.id); onBack() }, onDismiss = { isRemoving = false },
                )
            }
        }
    }
}
