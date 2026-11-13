package com.teanode.app.recordings

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
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
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.AppSection
import com.teanode.kit.photos.PhotoTimeline
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.recordings.RecordingConversation
import com.teanode.kit.recordings.RecordingConversationBubble
import com.teanode.kit.recordings.RecordingLibrary
import com.teanode.kit.recordings.RecordingLimits
import com.teanode.kit.recordings.RecordingLink
import com.teanode.kit.recordings.RecordingRowStatus
import com.teanode.kit.recordings.RecordingServerProgress
import com.teanode.kit.recordings.RecordingSpeakersView
import com.teanode.kit.recordings.RecordingTranscriptTimeline
import com.teanode.kit.recordings.RecordingView
import com.teanode.kit.recordings.cancelRecordingTranscription
import com.teanode.kit.recordings.correctRecordingTranscriptText
import com.teanode.kit.recordings.correctRecordingSpeaker
import com.teanode.kit.recordings.deleteRecording
import com.teanode.kit.recordings.recording
import com.teanode.kit.recordings.recordingLimits
import com.teanode.kit.recordings.recordingSpeakers
import com.teanode.kit.recordings.recordingTranscriptTimeline
import com.teanode.kit.recordings.requestRecordingTranscription
import com.teanode.kit.recordings.retryRecordingTranscription
import com.teanode.kit.recordings.suggestRecordingSpeakerNames
import com.teanode.kit.recordings.updateRecordingMetadata
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.Instant
import java.util.UUID

/** What the detail screen reads of a recording. */
internal class DetailRead(
    val view: RecordingView,
    val limits: RecordingLimits?,
    val speakers: RecordingSpeakersView?,
    val timeline: RecordingTranscriptTimeline?,
)

/**
 * One recording (ios/TeaNode/Sources/RecordingDetailView.swift): its heading and summary, a player over the whole
 * timeline, the transcript as a conversation (name a speaker, correct the words, play from a passage), and what can be
 * done: rename, transcribe again, retry or cancel a transcription, share, delete. [link] opens it at a cited passage.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RecordingDetailScreen(recordingId: String, onBack: () -> Unit, link: RecordingLink? = null) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var read by remember { mutableStateOf<DetailRead?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var reloadCount by remember { mutableStateOf(0) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isRenaming by remember { mutableStateOf(false) }
    var isDeleting by remember { mutableStateOf(false) }
    var isReprocessing by remember { mutableStateOf(false) }
    var isDetailsOpen by remember { mutableStateOf(false) }
    var namingBubble by remember { mutableStateOf<RecordingConversationBubble?>(null) }
    var correcting by remember { mutableStateOf<com.teanode.kit.recordings.RecordingTranscriptSegment?>(null) }
    var loadedSegments by remember { mutableStateOf<List<com.teanode.kit.recordings.RecordingTranscriptSegment>>(emptyList()) }
    val player = remember(connection) { RecordingPlayerController(context, connection, scope) }
    DisposableEffect(player) { onDispose { player.release() } }

    LaunchedEffect(reloadCount) {
        try {
            val view = connection.recording(recordingId)
            val limits = runCatching { connection.recordingLimits() }.getOrNull()
            val speakers = runCatching { connection.recordingSpeakers(recordingId) }.getOrNull()
            val timeline = runCatching { connection.recordingTranscriptTimeline(recordingId) }.getOrNull()
            read = DetailRead(view, limits, speakers, timeline)
            loadedSegments = timeline?.segments.orEmpty()
            failure = null
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            if (read == null) failure = error else snackbar.showError(error)
        }
    }
    // While it is uploading or being transcribed the screen reads the recording again every few seconds.
    LaunchedEffect(read) {
        val current = read ?: return@LaunchedEffect
        val status = RecordingLibrary.status(current.view.recording, null, RecordingServerProgress.of(current.view))
        if (status == RecordingRowStatus.Transcribing || status is RecordingRowStatus.Uploading) {
            delay(4_000)
            reloadCount += 1
        }
    }

    fun act(message: Int?, work: suspend () -> Unit) {
        scope.launch {
            try { work(); message?.let { text -> snackbar.show(context.getString(text)) }; reloadCount += 1 } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) }
        }
    }

    val current = read
    TeaNodeScreen(
        title = current?.view?.recording?.recordingTitle?.ifEmpty { null } ?: stringResource(R.string.recordings_details_title), onBack = onBack,
        actions = {
            if (current != null) Box {
                IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.common_more)) }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_rename)) }, onClick = { isMenuOpen = false; isRenaming = true })
                    if (current.limits?.isTranscriptionAvailable == true && current.view.mediaFiles.any { media -> media.recordingUploadState == "complete" }) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.recordings_transcribe_again)) }, onClick = { isMenuOpen = false; isReprocessing = true })
                    }
                    current.view.transcriptRuns.orEmpty().firstOrNull { run -> run.isRunning }?.let { run ->
                        DropdownMenuItem(text = { Text(stringResource(R.string.recordings_cancel_transcription)) }, onClick = { isMenuOpen = false; act(null) { connection.cancelRecordingTranscription(run.id) } })
                    }
                    current.view.transcriptRuns.orEmpty().lastOrNull { run -> run.recordingProcessingState == "failed" }?.let { run ->
                        DropdownMenuItem(text = { Text(stringResource(R.string.recordings_retry_transcription)) }, onClick = { isMenuOpen = false; act(R.string.recordings_transcribing_message) { connection.retryRecordingTranscription(run.id) } })
                    }
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_share)) }, onClick = { isMenuOpen = false; navigator.navigate(Routes.child(AppSection.Recordings, "share/$recordingId")) })
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_details)) }, onClick = { isMenuOpen = false; isDetailsOpen = true })
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_delete), color = MaterialTheme.colorScheme.error) }, onClick = { isMenuOpen = false; isDeleting = true })
                }
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            when {
                current == null && failure != null -> ErrorState(stringResource(R.string.recordings_cannot_load_one), onRetry = { failure = null; reloadCount += 1 })
                current == null -> LoadingState()
                else -> DetailBody(
                    current, loadedSegments, player, link,
                    onName = { bubble -> namingBubble = bubble }, onCorrect = { segment -> correcting = segment },
                    onLoadMore = {
                        scope.launch {
                            try {
                                val more = connection.recordingTranscriptTimeline(recordingId, segmentOffset = loadedSegments.size)
                                loadedSegments = loadedSegments + more.segments
                                read = DetailRead(current.view, current.limits, current.speakers, more.copy(hasMoreSegments = more.hasMoreSegments, segments = loadedSegments))
                            } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) }
                        }
                    },
                )
            }
        }
    }
    if (isRenaming && current != null) {
        RenameDialog(
            current.view.recording.recordingTitle, current.view.recording.recordedAt,
            onSave = { title, recordedAt ->
                isRenaming = false
                act(R.string.recordings_saved) { connection.updateRecordingMetadata(recordingId, current.view.recording.sourceGeneration, title, recordedAt) }
            },
            onDismiss = { isRenaming = false },
        )
    }
    if (isReprocessing && current != null) {
        ConfirmationDialog(
            title = stringResource(R.string.recordings_transcribe_again_title), confirmLabel = stringResource(R.string.recordings_transcribe_again), text = stringResource(R.string.recordings_transcribe_again_text),
            onConfirm = {
                isReprocessing = false
                act(R.string.recordings_transcribing_message) {
                    for (media in current.view.mediaFiles.filter { piece -> piece.recordingUploadState == "complete" }) {
                        val hasPriorRun = current.view.transcriptRuns.orEmpty().any { run -> run.recordingMediaId == media.id }
                        connection.requestRecordingTranscription(media.id, if (hasPriorRun) UUID.randomUUID().toString() else "initial", hasPriorRun)
                    }
                }
            },
            onDismiss = { isReprocessing = false },
        )
    }
    if (isDeleting) {
        ConfirmationDialog(
            title = stringResource(R.string.recordings_delete_title), confirmLabel = stringResource(R.string.recordings_delete), isDestructive = true, text = stringResource(R.string.recordings_delete_text),
            onConfirm = { isDeleting = false; scope.launch { try { connection.deleteRecording(recordingId); onBack() } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) } } },
            onDismiss = { isDeleting = false },
        )
    }
    if (isDetailsOpen && current != null) {
        ModalBottomSheet(onDismissRequest = { isDetailsOpen = false }) { RecordingDetailsSheet(current.view) }
    }
    namingBubble?.let { bubble ->
        NameSpeakerSheet(
            bubble, recordingId, current?.view?.recording?.sourceGeneration ?: 0,
            onNamed = { namingBubble = null; reloadCount += 1 }, onDismiss = { namingBubble = null },
        )
    }
    correcting?.let { segment ->
        CorrectTextDialog(
            segment,
            onSave = { text ->
                correcting = null
                act(R.string.recordings_saved) { connection.correctRecordingTranscriptText(recordingId, segment.id, current?.view?.recording?.sourceGeneration ?: 0, UUID.randomUUID().toString(), text) }
            },
            onDismiss = { correcting = null },
        )
    }
}

@Composable
private fun DetailBody(
    read: DetailRead,
    segments: List<com.teanode.kit.recordings.RecordingTranscriptSegment>,
    player: RecordingPlayerController,
    link: RecordingLink?,
    onName: (RecordingConversationBubble) -> Unit,
    onCorrect: (com.teanode.kit.recordings.RecordingTranscriptSegment) -> Unit,
    onLoadMore: () -> Unit,
) {
    val recording = read.view.recording
    val progress = RecordingServerProgress.of(read.view)
    var status = RecordingLibrary.status(recording, null, progress)
    if (status == RecordingRowStatus.Transcribing && progress.transcriptProcessingStates.isEmpty() && read.limits?.isTranscriptionAvailable == false) status = RecordingRowStatus.Ready
    val playableFiles = read.view.mediaFiles.filter { media -> media.recordingUploadState == "complete" }
    val bubbles = RecordingConversation.bubbles(segments, read.timeline?.liveUtterances.orEmpty())
    val unnamedCount = bubbles.count { bubble -> bubble.confirmedSpeakerName == null && !bubble.isLive }
    LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 32.dp)) {
        item {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(recording.recordingTitle.ifEmpty { stringResource(R.string.recordings_untitled) }, style = MaterialTheme.typography.headlineSmall)
                val whenText = (ServerTime.instant(recording.recordedAt) ?: ServerTime.instant(recording.createdAt))?.let { instant ->
                    java.time.format.DateTimeFormatter.ofLocalizedDateTime(java.time.format.FormatStyle.MEDIUM, java.time.format.FormatStyle.SHORT).format(instant.atZone(java.time.ZoneId.systemDefault()))
                }
                Text(listOfNotNull(whenText, recording.timelineDurationMS.takeIf { length -> length > 0 }?.let { length -> PhotoTimeline.durationText(length.coerceAtMost(Int.MAX_VALUE.toLong()).toInt()) }).joinToString(" · "), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
                StatusLine(status, read.view)
                recording.recordingSummary?.takeIf { summary -> summary.isNotBlank() }?.let { summary -> Text(summary, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(top = 8.dp)) }
            }
        }
        if (playableFiles.isNotEmpty()) item { PlayerBar(playableFiles, player, recording.timelineDurationMS) }
        item {
            TeaNodeSectionHeader(stringResource(R.string.recordings_transcript))
            if (bubbles.isEmpty()) {
                Text(
                    stringResource(if (status == RecordingRowStatus.Transcribing) R.string.recordings_transcript_waiting else R.string.recordings_transcript_none), color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                )
            } else if (unnamedCount > 0) {
                Text(stringResource(R.string.recordings_passages_to_name, unnamedCount), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
            }
        }
        items(bubbles, key = { bubble -> bubble.id }) { bubble ->
            Bubble(bubble, onPlay = { player.playFrom(playableFiles, bubble.startMS) }, onName = { onName(bubble) }, onCorrect = { onCorrect(bubble.segments.first()) }, isHighlighted = link?.includes(bubble.segments.first()) == true && link.passageStartMS != null)
        }
        if (read.timeline?.hasMoreSegments == true) item {
            TextButton(onClick = onLoadMore, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.recordings_load_more)) }
        }
    }
}

@Composable
private fun StatusLine(status: RecordingRowStatus, view: RecordingView) {
    val text = when (status) {
        is RecordingRowStatus.Uploading -> stringResource(R.string.recordings_status_uploading, status.percent)
        RecordingRowStatus.Transcribing -> stringResource(R.string.recordings_status_transcribing)
        RecordingRowStatus.Failed -> view.transcriptRuns.orEmpty().lastOrNull { run -> run.processingErrorMessage.isNotEmpty() }?.processingErrorMessage?.takeIf { message -> message.isNotEmpty() } ?: stringResource(R.string.recordings_status_failed)
        else -> return
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        if (status != RecordingRowStatus.Failed) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
        Text(text, color = if (status == RecordingRowStatus.Failed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
private fun PlayerBar(files: List<com.teanode.kit.recordings.RecordingMedia>, player: RecordingPlayerController, timelineDurationMS: Long) {
    val durationMS = maxOf(timelineDurationMS, player.durationMS, 1L)
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = { player.toggle(files) }) {
            if (player.isPreparing) CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
            else Icon(if (player.isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow, contentDescription = stringResource(if (player.isPlaying) R.string.recordings_pause else R.string.recordings_play))
        }
        Text(com.teanode.kit.photos.VideoControls.timeText(player.timelineMS / 1000.0), style = MaterialTheme.typography.labelMedium)
        Slider(
            value = (player.timelineMS.toFloat() / durationMS).coerceIn(0f, 1f), onValueChange = { fraction -> player.seekTo(files, (fraction * durationMS).toLong()) },
            modifier = Modifier.weight(1f).padding(horizontal = 8.dp),
        )
        Text(com.teanode.kit.photos.VideoControls.timeText(durationMS / 1000.0), style = MaterialTheme.typography.labelMedium)
    }
    player.problem?.let { Text(stringResource(R.string.recordings_cannot_play), color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 16.dp)) }
}

@Composable
private fun Bubble(
    bubble: RecordingConversationBubble,
    onPlay: () -> Unit,
    onName: () -> Unit,
    onCorrect: () -> Unit,
    isHighlighted: Boolean,
) {
    var isMenuOpen by remember { mutableStateOf(false) }
    val speaker = bubble.confirmedSpeakerName ?: bubble.temporarySpeakerLabel ?: if (bubble.isLive) null else stringResource(R.string.recordings_speaker_letter, RecordingConversation.speakerLetter(bubble.unconfirmedSpeakerOrdinal))
    val container = when {
        isHighlighted -> MaterialTheme.colorScheme.tertiaryContainer
        bubble.isAccountOwner -> MaterialTheme.colorScheme.primaryContainer
        else -> MaterialTheme.colorScheme.surfaceVariant
    }
    Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
        Column(
            Modifier.fillMaxWidth().background(container, RoundedCornerShape(16.dp)).clickable { isMenuOpen = true }.padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (speaker != null) Text(speaker, style = MaterialTheme.typography.labelLarge, color = if (bubble.confirmedSpeakerName != null) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant)
                if (bubble.isLive) Text(stringResource(R.string.recordings_live), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.error)
                Text(com.teanode.kit.photos.VideoControls.timeText(bubble.startMS / 1000.0), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Text(bubble.bubbleText, style = MaterialTheme.typography.bodyLarge)
        }
        DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
            if (!bubble.isLive) {
                DropdownMenuItem(text = { Text(stringResource(R.string.recordings_play_from_here)) }, onClick = { isMenuOpen = false; onPlay() })
                DropdownMenuItem(text = { Text(stringResource(R.string.recordings_name_speaker)) }, onClick = { isMenuOpen = false; onName() })
                DropdownMenuItem(text = { Text(stringResource(R.string.recordings_correct_text)) }, onClick = { isMenuOpen = false; onCorrect() })
            }
            val clipboard = androidx.compose.ui.platform.LocalClipboardManager.current
            DropdownMenuItem(text = { Text(stringResource(R.string.recordings_copy)) }, onClick = { isMenuOpen = false; clipboard.setText(androidx.compose.ui.text.AnnotatedString(bubble.bubbleText)) })
        }
    }
}

@Composable
private fun RenameDialog(title: String, recordedAt: String?, onSave: (String?, String?) -> Unit, onDismiss: () -> Unit) {
    var text by remember { mutableStateOf(title) }
    var hasDate by remember { mutableStateOf(recordedAt != null) }
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.recordings_rename)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(value = text, onValueChange = { text = it }, singleLine = true, label = { Text(stringResource(R.string.recordings_title_field)) })
                Text(stringResource(R.string.recordings_rename_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.recordings_date_known), Modifier.weight(1f))
                    Switch(checked = hasDate, onCheckedChange = { hasDate = it })
                }
            }
        },
        confirmButton = { TextButton(onClick = { onSave(text.trim(), if (hasDate) (recordedAt ?: ServerTime.text(Instant.now())) else null) }) { Text(stringResource(R.string.recordings_save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

@Composable
private fun CorrectTextDialog(segment: com.teanode.kit.recordings.RecordingTranscriptSegment, onSave: (String) -> Unit, onDismiss: () -> Unit) {
    var text by remember { mutableStateOf(segment.displayedTranscriptText) }
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.recordings_correct_text)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(value = text, onValueChange = { text = it }, modifier = Modifier.fillMaxWidth())
                Text(stringResource(R.string.recordings_correct_text_note), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        confirmButton = { TextButton(onClick = { onSave(text.trim()) }, enabled = text.isNotBlank() && text.trim() != segment.displayedTranscriptText) { Text(stringResource(R.string.recordings_save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** Choose or type who is speaking; the name can apply to the whole voice of the recording. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NameSpeakerSheet(bubble: RecordingConversationBubble, recordingId: String, expectedSourceGeneration: Long, onNamed: () -> Unit, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf(bubble.confirmedSpeakerName.orEmpty()) }
    var isWholeGroup by remember { mutableStateOf(true) }
    var suggestions by remember { mutableStateOf(emptyList<com.teanode.kit.recordings.RecordingSpeakerNameSuggestion>()) }
    LaunchedEffect(name) {
        delay(250)
        suggestions = runCatching { connection.suggestRecordingSpeakerNames(name.trim()) }.getOrDefault(emptyList())
    }
    val first = bubble.segments.first()
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.recordings_name_speaker), style = MaterialTheme.typography.titleLarge)
            OutlinedTextField(value = name, onValueChange = { name = it }, singleLine = true, label = { Text(stringResource(R.string.recordings_confirmed_name)) }, modifier = Modifier.fillMaxWidth())
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.recordings_apply_to_group), Modifier.weight(1f))
                Switch(checked = isWholeGroup, onCheckedChange = { isWholeGroup = it })
            }
            Text(stringResource(R.string.recordings_apply_to_group_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (suggestions.isNotEmpty()) {
                Text(stringResource(R.string.recordings_suggestions), style = MaterialTheme.typography.titleSmall)
                for (suggestion in suggestions.take(5)) Text(suggestion.confirmedName, Modifier.fillMaxWidth().clickable { name = suggestion.confirmedName }.padding(vertical = 8.dp))
            }
            Button(
                onClick = {
                    scope.launch {
                        try {
                            connection.correctRecordingSpeaker(
                                recordingId, expectedSourceGeneration, UUID.randomUUID().toString(), name.trim(), null, suggestions.firstOrNull { suggestion -> suggestion.confirmedName == name.trim() }?.addressBookId,
                                suggestions.firstOrNull { suggestion -> suggestion.confirmedName == name.trim() }?.contactId, first.recordingTranscriptionAttemptId, first.localSpeakerId,
                                if (isWholeGroup) 0 else first.segmentStartMS, if (isWholeGroup) Long.MAX_VALUE / 4 else bubble.segments.last().segmentEndMS,
                            )
                            onNamed()
                        } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) }
                    }
                },
                enabled = name.isNotBlank(),
            ) { Text(stringResource(R.string.recordings_save)) }
        }
    }
}

@Composable
internal fun RecordingDetailsSheet(view: RecordingView) {
    Column(Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(stringResource(R.string.recordings_details), style = MaterialTheme.typography.titleLarge)
        if (view.recording.captureGaps.isNotEmpty()) {
            Text(stringResource(R.string.recordings_capture_gaps), style = MaterialTheme.typography.titleSmall)
            for (gap in view.recording.captureGaps) {
                Text("${com.teanode.kit.photos.VideoControls.timeText(gap.gapStartMS / 1000.0)} - ${com.teanode.kit.photos.VideoControls.timeText((gap.gapEndMS ?: gap.gapStartMS) / 1000.0)} · " + stringResource(if (gap.recordingGapReason == "interrupted") R.string.recordings_gap_interrupted else R.string.recordings_gap_paused))
            }
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
        }
        Text(stringResource(R.string.recordings_original_media), style = MaterialTheme.typography.titleSmall)
        for (media in view.mediaFiles) Text("${media.originalFilename} · ${com.teanode.app.photos.bytesText(media.originalSizeBytes)} · ${media.recordingUploadState}")
        view.transcriptRuns.orEmpty().takeIf { runs -> runs.isNotEmpty() }?.let { runs ->
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
            Text(stringResource(R.string.recordings_transcription_details), style = MaterialTheme.typography.titleSmall)
            for (run in runs) Text(stringResource(R.string.recordings_run_line, run.versionNumber, run.recordingProcessingState, run.providerModelName))
        }
    }
}
