package com.teanode.app.recordings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.AppSection
import com.teanode.kit.photos.PhotoShareTargets
import com.teanode.kit.photos.PhotoTimeline
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.photos.photoShareTargets
import com.teanode.kit.recordings.RecordingShare
import com.teanode.kit.recordings.RecordingShareLevel
import com.teanode.kit.recordings.SharedRecording
import com.teanode.kit.recordings.recordingShares
import com.teanode.kit.recordings.shareRecording
import com.teanode.kit.recordings.sharedRecording
import com.teanode.kit.recordings.sharedRecordings
import com.teanode.kit.recordings.unshareRecording
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

private class SharesRead(val shares: List<RecordingShare>, val targets: PhotoShareTargets)

/** Who a recording is shared with, and sharing it with a person or a whole circle at a level (summary, or the transcript too). */
@Composable
fun RecordingShareScreen(recordingId: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val read = com.teanode.app.ui.components.rememberLoad(recordingId) { SharesRead(connection.recordingShares(recordingId), runCatching { connection.photoShareTargets(null) }.getOrDefault(PhotoShareTargets(emptyList(), emptyList()))) }
    var isAdding by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<RecordingShare?>(null) }
    var chosenUserId by remember { mutableStateOf<String?>(null) }
    var chosenCircleId by remember { mutableStateOf<String?>(null) }
    var level by remember { mutableStateOf(RecordingShareLevel.Summary) }
    TeaNodeScreen(
        title = stringResource(R.string.recordings_share_title), onBack = onBack,
        floatingActionButton = { FloatingActionButton(onClick = { chosenUserId = null; chosenCircleId = null; isAdding = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.recordings_share)) } },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            LoadedContent(read) { loaded ->
                LazyColumn(Modifier.fillMaxSize()) {
                    item {
                        Text(stringResource(R.string.recordings_share_warning), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
                        TeaNodeSectionHeader(stringResource(R.string.recordings_shared_with))
                        if (loaded.shares.isEmpty()) Text(stringResource(R.string.recordings_not_shared_yet), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    items(loaded.shares, key = { share -> share.id }) { share ->
                        TeaNodeRow(
                            headline = share.granteeDisplayName, supporting = stringResource(if (share.level == RecordingShareLevel.Transcript) R.string.recordings_level_transcript else R.string.recordings_level_summary),
                            leading = { if (share.isWithCircle) Icon(Icons.Outlined.Groups, contentDescription = null) else AvatarView(share.granteeDisplayName, null, null) },
                            trailing = { TextButton(onClick = { removing = share }) { Text(stringResource(R.string.recordings_stop_sharing), color = MaterialTheme.colorScheme.error) } },
                        )
                    }
                }
                if (isAdding) {
                    val available = loaded.targets
                    androidx.compose.material3.AlertDialog(
                        onDismissRequest = { isAdding = false },
                        title = { Text(stringResource(R.string.recordings_share_title)) },
                        text = {
                            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                if (available.circles.isEmpty() && available.people.isEmpty()) Text(stringResource(R.string.recordings_share_nobody))
                                for (circle in available.circles.filter { candidate -> loaded.shares.none { share -> share.granteeCircleId == candidate.id } }) {
                                    TeaNodeRow(headline = circle.name, leadingIcon = Icons.Outlined.Groups, trailing = { if (chosenCircleId == circle.id) Text("✓") }, onClick = { chosenCircleId = circle.id; chosenUserId = null })
                                }
                                for (person in available.people.filter { candidate -> loaded.shares.none { share -> share.granteeUserId == candidate.userId } }) {
                                    TeaNodeRow(headline = person.displayName, leading = { AvatarView(person.displayName, person.avatarUrl, person.avatarColorIndex, size = 32.dp) }, trailing = { if (chosenUserId == person.userId) Text("✓") }, onClick = { chosenUserId = person.userId; chosenCircleId = null })
                                }
                                Text(stringResource(R.string.recordings_how_much), style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 8.dp))
                                for (choice in RecordingShareLevel.grantable) {
                                    TeaNodeRow(
                                        headline = stringResource(if (choice == RecordingShareLevel.Transcript) R.string.recordings_level_transcript else R.string.recordings_level_summary),
                                        trailing = { if (level == choice) Text("✓") }, onClick = { level = choice },
                                    )
                                }
                            }
                        },
                        confirmButton = {
                            TextButton(
                                onClick = {
                                    scope.launch {
                                        try { connection.shareRecording(recordingId, chosenUserId, chosenCircleId, level); isAdding = false; read.refresh() } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) }
                                    }
                                },
                                enabled = chosenUserId != null || chosenCircleId != null,
                            ) { Text(stringResource(R.string.recordings_share)) }
                        },
                        dismissButton = { TextButton(onClick = { isAdding = false }) { Text(stringResource(R.string.common_cancel)) } },
                    )
                }
            }
        }
    }
    removing?.let { share ->
        ConfirmationDialog(
            title = stringResource(R.string.recordings_stop_sharing), confirmLabel = stringResource(R.string.recordings_stop_sharing), isDestructive = true, text = stringResource(R.string.recordings_stop_sharing_text, share.granteeDisplayName),
            onConfirm = { removing = null; scope.launch { try { connection.unshareRecording(share.id); read.refresh() } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) } } },
            onDismiss = { removing = null },
        )
    }
}

/** Recordings other people chose to share with the person. */
@Composable
fun SharedRecordingsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val read = com.teanode.app.ui.components.rememberLoad { connection.sharedRecordings() }
    TeaNodeScreen(title = stringResource(R.string.recordings_shared_with_you), onBack = onBack) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            LoadedContent(read, isEmpty = { list -> list.isEmpty() }, empty = { EmptyState(Icons.Outlined.GraphicEq, stringResource(R.string.recordings_shared_empty), description = stringResource(R.string.recordings_shared_empty_text)) }) { list ->
                LazyColumn(Modifier.fillMaxSize()) {
                    items(list, key = { shared -> shared.recordingId }) { shared ->
                        TeaNodeRow(
                            headline = shared.recordingTitle.ifEmpty { stringResource(R.string.recordings_untitled) }, supporting = shared.recordingSummary.lineSequence().firstOrNull { line -> line.isNotBlank() },
                            overline = shared.ownerDisplayName + (ServerTime.instant(shared.recordedAt ?: shared.createdAt)?.let { instant -> " · " + relativeTimeText(instant.toEpochMilli()) } ?: ""),
                            onClick = { navigator.navigate(Routes.child(AppSection.Recordings, "shared/${shared.recordingId}")) },
                        )
                    }
                }
            }
        }
    }
}

/** One shared recording: its summary and, when the owner shared the transcript too, what each speaker said. */
@Composable
fun SharedRecordingScreen(recordingId: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val read = com.teanode.app.ui.components.rememberLoad(recordingId) { connection.sharedRecording(recordingId) }
    TeaNodeScreen(title = stringResource(R.string.recordings_shared_recording), onBack = onBack) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            LoadedContent(read) { shared -> SharedBody(shared) }
        }
    }
}

@Composable
private fun SharedBody(shared: SharedRecording) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp)) {
        item {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(shared.recordingTitle.ifEmpty { stringResource(R.string.recordings_untitled) }, style = MaterialTheme.typography.headlineSmall)
                Text(stringResource(R.string.recordings_shared_by, shared.ownerDisplayName) + if (shared.timelineDurationMS > 0) " · " + PhotoTimeline.durationText(shared.timelineDurationMS.coerceAtMost(Int.MAX_VALUE.toLong()).toInt()) else "", color = MaterialTheme.colorScheme.onSurfaceVariant)
                shared.speakerNames?.takeIf { names -> names.isNotEmpty() }?.let { names -> Text(stringResource(R.string.recordings_speakers_list, names.joinToString(", ")), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                if (shared.recordingSummary.isNotBlank()) Text(shared.recordingSummary, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(top = 8.dp))
            }
        }
        if (shared.passages == null) {
            item { Text(stringResource(R.string.recordings_transcript_not_shared), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        } else {
            items(shared.turns, key = { turn -> turn.id }) { turn ->
                Column(Modifier.padding(horizontal = 16.dp, vertical = 6.dp)) {
                    Text(turn.speakerName.ifEmpty { stringResource(R.string.recordings_unnamed) }, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
                    Text(turn.text, style = MaterialTheme.typography.bodyLarge)
                }
            }
        }
    }
}
