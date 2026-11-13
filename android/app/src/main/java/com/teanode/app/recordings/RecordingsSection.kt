package com.teanode.app.recordings

import android.Manifest
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PauseCircle
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.UploadFile
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.AppSection
import com.teanode.kit.photos.PhotoTimeline
import com.teanode.kit.recordings.LocalRecording
import com.teanode.kit.recordings.LocalRecordingMedia
import com.teanode.kit.recordings.Recording
import com.teanode.kit.recordings.RecordingLibrary
import com.teanode.kit.recordings.RecordingLibraryRow
import com.teanode.kit.recordings.RecordingRowStatus
import com.teanode.kit.recordings.RecordingServerProgress
import com.teanode.kit.recordings.deleteRecording
import com.teanode.kit.recordings.recording
import com.teanode.kit.recordings.recordings
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.util.UUID

/** What the library read: the server's recordings, and how far the ones still changing have come. */
private class RecordingsRead(val recordings: List<Recording>, val progress: Map<String, RecordingServerProgress>)

/**
 * The Recordings list (ios/TeaNode/Sources/RecordingsView.swift): the server's recordings joined with the ones the phone
 * still holds (uploading, waiting for the network, or failed), newest first, with a heading, the first line of the summary
 * and a status. Starting one, importing audio or video, and the recordings shared with the person are here.
 */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun RecordingsSection(initialLocalRecordings: List<LocalRecording>? = null, initialSearchText: String? = null) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val journal = remember { RecordingJournal(context) }
    var isSearching by remember { mutableStateOf(initialSearchText != null) }
    var searchText by remember { mutableStateOf(initialSearchText.orEmpty()) }
    var read by remember { mutableStateOf<RecordingsRead?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var localRecordings by remember { mutableStateOf(initialLocalRecordings ?: emptyList()) }
    var reloadCount by remember { mutableStateOf(0) }
    var menuRowId by remember { mutableStateOf<String?>(null) }
    var deleting by remember { mutableStateOf<RecordingLibraryRow?>(null) }
    var removing by remember { mutableStateOf<RecordingLibraryRow?>(null) }
    var isNewMenuOpen by remember { mutableStateOf(false) }
    var isMoreMenuOpen by remember { mutableStateOf(false) }

    LaunchedEffect(searchText, reloadCount) {
        if (searchText.isNotEmpty()) delay(350)
        try {
            val listed = connection.recordings(searchText = searchText.trim())
            read = RecordingsRead(listed, read?.progress.orEmpty())
            failure = null
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            if (read == null) failure = error else snackbar.showError(error)
        }
    }
    // While a recording is uploading or being transcribed its row is read again every few seconds, as the iPhone app does.
    LaunchedEffect(read?.recordings, localRecordings) {
        while (true) {
            if (initialLocalRecordings == null) localRecordings = journal.recordings(connection.accountKey)
            val current = read ?: break
            val rows = RecordingLibrary.rows(current.recordings, localRecordings, current.progress)
            val changing = rows.filter { row -> row.recording != null && row.isStillChanging(System.currentTimeMillis()) }
            val hasUploadsToWatch = localRecordings.any { local -> local.id in com.teanode.kit.recordings.RecordingLibraryProgress.pendingLocalRecordingIds(listOf(local)) }
            if (changing.isEmpty() && !hasUploadsToWatch) break
            val progress = current.progress.toMutableMap()
            for (row in changing) {
                val recordingId = row.recording?.id ?: continue
                runCatching { connection.recording(recordingId) }.getOrNull()?.let { view -> progress[recordingId] = RecordingServerProgress.of(view) }
            }
            if (progress != current.progress) read = RecordingsRead(current.recordings, progress)
            delay(4_000)
            runCatching { connection.recordings(searchText = searchText.trim()) }.getOrNull()?.let { listed -> read = RecordingsRead(listed, read?.progress ?: progress) }
        }
    }

    val importLauncher = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null) scope.launch {
            try {
                importRecording(context, connection.accountKey, journal, uri)
                RecordingUploadScheduler.schedule(context)
                localRecordings = journal.recordings(connection.accountKey)
                snackbar.show(context.getString(R.string.recordings_import_started))
            } catch (error: CancellationException) {
                throw error
            } catch (error: Throwable) {
                snackbar.showError(error)
            }
        }
    }
    val permissionLauncher = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { granted ->
        if (granted[Manifest.permission.RECORD_AUDIO] == true) navigator.navigate(Routes.child(AppSection.Recordings, "capture"))
        else snackbar.showProblem(context.getString(R.string.recordings_microphone_needed))
    }
    fun startRecording() {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) navigator.navigate(Routes.child(AppSection.Recordings, "capture"))
        else permissionLauncher.launch(arrayOf(Manifest.permission.RECORD_AUDIO, Manifest.permission.POST_NOTIFICATIONS))
    }

    TeaNodeScreen(
        title = stringResource(R.string.section_recordings),
        actions = {
            IconButton(onClick = { isSearching = !isSearching; if (!isSearching) searchText = "" }) { Icon(Icons.Outlined.Search, contentDescription = stringResource(R.string.common_search)) }
            Box {
                IconButton(onClick = { isMoreMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.common_more)) }
                DropdownMenu(expanded = isMoreMenuOpen, onDismissRequest = { isMoreMenuOpen = false }) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_shared_with_you)) }, onClick = { isMoreMenuOpen = false; navigator.navigate(Routes.child(AppSection.Recordings, "shared")) })
                }
            }
        },
        floatingActionButton = {
            Box {
                ExtendedFloatingActionButton(onClick = { isNewMenuOpen = true }, icon = { Icon(Icons.Outlined.Add, contentDescription = null) }, text = { Text(stringResource(R.string.recordings_new)) })
                DropdownMenu(expanded = isNewMenuOpen, onDismissRequest = { isNewMenuOpen = false }) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_start)) }, leadingIcon = { Icon(Icons.Outlined.Mic, contentDescription = null) }, onClick = { isNewMenuOpen = false; startRecording() })
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_import)) }, leadingIcon = { Icon(Icons.Outlined.UploadFile, contentDescription = null) }, onClick = { isNewMenuOpen = false; importLauncher.launch(arrayOf("audio/*", "video/*")) })
                }
            }
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            if (isSearching) {
                OutlinedTextField(value = searchText, onValueChange = { searchText = it }, singleLine = true, placeholder = { Text(stringResource(R.string.recordings_search_prompt)) }, modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp))
            }
            val current = read
            when {
                current == null && failure != null -> ErrorState(stringResource(R.string.recordings_cannot_load), onRetry = { failure = null; reloadCount += 1 })
                current == null -> LoadingState()
                else -> {
                    val rows = RecordingLibrary.rows(current.recordings, if (searchText.isEmpty()) localRecordings else emptyList(), current.progress)
                    if (rows.isEmpty()) {
                        EmptyState(Icons.Outlined.GraphicEq, stringResource(if (searchText.isEmpty()) R.string.recordings_empty else R.string.recordings_empty_search), description = if (searchText.isEmpty()) stringResource(R.string.recordings_empty_text) else null)
                    } else {
                        LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 96.dp)) {
                            items(rows, key = { row -> row.id }) { row ->
                                Box {
                                    RecordingRow(
                                        row,
                                        onClick = {
                                            when {
                                                row.isLiveSession -> navigator.navigate(Routes.child(AppSection.Recordings, "capture"))
                                                row.recording != null -> navigator.navigate(Routes.child(AppSection.Recordings, "detail/${row.recording!!.id}"))
                                                row.localRecording != null -> navigator.navigate(Routes.child(AppSection.Recordings, "local/${row.localRecording!!.id}"))
                                            }
                                        },
                                        onLongClick = { menuRowId = row.id },
                                    )
                                    DropdownMenu(expanded = menuRowId == row.id, onDismissRequest = { menuRowId = null }) {
                                        if (row.canRetryUpload) DropdownMenuItem(text = { Text(stringResource(R.string.recordings_retry_upload)) }, onClick = { menuRowId = null; RecordingUploadScheduler.schedule(context) })
                                        if (row.canPlayLocalOriginal) DropdownMenuItem(text = { Text(stringResource(R.string.recordings_play_local_original)) }, onClick = { menuRowId = null; navigator.navigate(Routes.child(AppSection.Recordings, "local/${row.localRecording!!.id}")) })
                                        if (row.canRemoveFromPhone) DropdownMenuItem(text = { Text(stringResource(R.string.recordings_remove_from_phone)) }, onClick = { menuRowId = null; removing = row })
                                        if (row.canDelete) DropdownMenuItem(text = { Text(stringResource(R.string.recordings_delete), color = MaterialTheme.colorScheme.error) }, onClick = { menuRowId = null; deleting = row })
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
    deleting?.let { row ->
        ConfirmationDialog(
            title = stringResource(R.string.recordings_delete_title), confirmLabel = stringResource(R.string.recordings_delete), isDestructive = true, text = stringResource(R.string.recordings_delete_text),
            onConfirm = {
                deleting = null
                scope.launch {
                    try { connection.deleteRecording(row.recording!!.id); row.localRecording?.let { local -> journal.remove(local.id) }; reloadCount += 1 } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) }
                }
            },
            onDismiss = { deleting = null },
        )
    }
    removing?.let { row ->
        ConfirmationDialog(
            title = stringResource(R.string.recordings_remove_from_phone), confirmLabel = stringResource(R.string.recordings_remove_from_phone), isDestructive = true, text = stringResource(R.string.recordings_remove_text),
            onConfirm = { removing = null; row.localRecording?.let { local -> journal.remove(local.id) }; localRecordings = journal.recordings(connection.accountKey) },
            onDismiss = { removing = null },
        )
    }
}

/** One row of the list: the heading, the first line of the summary, when and how long, and what became of it. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun RecordingRow(row: RecordingLibraryRow, onClick: () -> Unit, onLongClick: () -> Unit = {}, nowEpochMS: Long = System.currentTimeMillis()) {
    TeaNodeRow(
        headline = row.recordingTitle.ifEmpty { stringResource(R.string.recordings_untitled) },
        supporting = row.recordingSummaryFirstLine.ifEmpty { null },
        overline = relativeTimeText(row.recordedAtEpochMS, nowEpochMS) + if (row.timelineDurationMS > 0) " · " + PhotoTimeline.durationText(row.timelineDurationMS.coerceAtMost(Int.MAX_VALUE.toLong()).toInt()) else "",
        leading = { StatusIcon(row.status) },
        trailing = { StatusText(row.status) },
        modifier = Modifier.combinedClickable(onClick = onClick, onLongClick = onLongClick),
    )
}

@Composable
private fun StatusIcon(status: RecordingRowStatus) {
    when (status) {
        RecordingRowStatus.Recording -> Icon(Icons.Outlined.Mic, contentDescription = null, tint = MaterialTheme.colorScheme.error)
        RecordingRowStatus.Paused -> Icon(Icons.Outlined.PauseCircle, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        is RecordingRowStatus.Uploading, RecordingRowStatus.Transcribing -> CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
        RecordingRowStatus.WaitingForNetwork -> Icon(Icons.Outlined.CloudOff, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        RecordingRowStatus.Failed -> Icon(Icons.Outlined.ErrorOutline, contentDescription = null, tint = MaterialTheme.colorScheme.error)
        RecordingRowStatus.Ready -> Icon(Icons.Outlined.GraphicEq, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
    }
}

@Composable
private fun StatusText(status: RecordingRowStatus) {
    val text = when (status) {
        RecordingRowStatus.Recording -> stringResource(R.string.recordings_status_recording)
        RecordingRowStatus.Paused -> stringResource(R.string.recordings_status_paused)
        is RecordingRowStatus.Uploading -> stringResource(R.string.recordings_status_uploading, status.percent)
        RecordingRowStatus.WaitingForNetwork -> stringResource(R.string.recordings_status_waiting)
        RecordingRowStatus.Transcribing -> stringResource(R.string.recordings_status_transcribing)
        RecordingRowStatus.Failed -> stringResource(R.string.recordings_status_failed)
        RecordingRowStatus.Ready -> return
    }
    Text(text, style = MaterialTheme.typography.labelMedium, color = if (status == RecordingRowStatus.Failed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
}

/** Makes a local recording of a file the person chose, copies the file into the journal, and leaves the rest to the uploader. */
internal fun importRecording(context: android.content.Context, accountKey: String, journal: RecordingJournal, uri: Uri) {
    val resolver = context.contentResolver
    var name = "recording"
    resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor -> if (cursor.moveToFirst()) name = cursor.getString(0) ?: name }
    val contentType = resolver.getType(uri) ?: "audio/mp4"
    val local = LocalRecording(
        id = UUID.randomUUID().toString(), accountKey = accountKey, recordingTitle = name.substringBeforeLast('.'), recordingOrigin = "imported", startedAtEpochMS = System.currentTimeMillis(),
    )
    val media = LocalRecordingMedia(UUID.randomUUID().toString(), null, name, contentType, 0)
    journal.create(local.copy(localMedia = listOf(media)))
    val destination = journal.originalFile(local.id, media.id)
    (resolver.openInputStream(uri) ?: error("The file could not be read.")).use { input -> destination.outputStream().use { output -> input.copyTo(output) } }
    if (destination.length() == 0L) { journal.remove(local.id); error("The file is empty.") }
}
