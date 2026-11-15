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
import androidx.compose.foundation.verticalScroll
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
import com.teanode.app.chats.ChatsHooks
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.kit.chats.AgentReference
import com.teanode.kit.recordings.RecordingAttemptCost
import com.teanode.kit.recordings.RecordingOriginals
import com.teanode.kit.recordings.RecordingTranscriptionAttempt
import com.teanode.kit.agent.knowledgeSourceDetails
import androidx.compose.runtime.produceState
import com.teanode.kit.recordings.RecordingPassageInterval
import com.teanode.kit.recordings.RecordingSpeakerNaming
import com.teanode.kit.recordings.RecordingTemporarySpeakers
import com.teanode.kit.recordings.clearRecordingSpeakerNames
import com.teanode.kit.recordings.nameRecordingTemporarySpeaker
import com.teanode.kit.recordings.separateRecordingTemporarySpeaker
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.AppSection
import com.teanode.kit.asReasonSentence
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
import com.teanode.kit.recordings.nextRunEpochMS
import com.teanode.kit.recordings.recordingLearning
import com.teanode.kit.recordings.state
import com.teanode.kit.recordings.recordingLimits
import com.teanode.kit.recordings.recordingSpeakers
import com.teanode.kit.recordings.recordingTranscript
import com.teanode.kit.recordings.recordingTranscriptPassageOffset
import com.teanode.kit.recordings.recordingTranscriptTimeline
import com.teanode.kit.recordings.requestRecordingTranscription
import com.teanode.kit.recordings.resumeRecordingLearning
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
    val learning: com.teanode.kit.recordings.RecordingLearningView? = null,
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
    var isOriginalsMenuOpen by remember { mutableStateOf(false) }
    val agentName = com.teanode.app.ui.components.rememberAgentName()
    val agentTitle = agentName.ifEmpty { stringResource(R.string.chats_your_agent) }
    val untitledText = stringResource(R.string.recordings_untitled)
    var namingBubble by remember { mutableStateOf<RecordingConversationBubble?>(null) }
    var memorySegment by remember { mutableStateOf<com.teanode.kit.recordings.RecordingTranscriptSegment?>(null) }
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
            val learning = runCatching { connection.recordingLearning(recordingId) }.getOrNull()
            read = DetailRead(view, limits, speakers, timeline, learning)
            loadedSegments = timeline?.segments.orEmpty()
            // A cited passage that is not on the first page: read the page of that version that holds it.
            val runId = link?.recordingTranscriptRunId
            val start = link?.passageStartMS
            val end = link?.passageEndMS
            if (runId != null && start != null && end != null && loadedSegments.none { segment -> link.includes(segment) }) {
                runCatching {
                    val offset = connection.recordingTranscriptPassageOffset(runId, start, end)
                    loadedSegments = connection.recordingTranscript(runId, segmentOffset = offset).segments
                }
            }
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
        title = current?.view?.recording?.recordingTitle?.ifEmpty { null } ?: if (current != null) stringResource(R.string.recordings_untitled) else stringResource(R.string.recordings_details_title), onBack = onBack,
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
                    val originals = RecordingOriginals.completeFiles(current.view.mediaFiles)
                    if (originals.isNotEmpty()) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.recordings_share_original)) },
                            onClick = {
                                isMenuOpen = false
                                if (originals.size == 1) act(null) { connection.shareRecordingOriginal(context, originals.first()) } else isOriginalsMenuOpen = true
                            },
                        )
                    }
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_speakers_title)) }, onClick = { isMenuOpen = false; navigator.navigate(Routes.child(AppSection.Recordings, "speakers/$recordingId")) })
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_edit_history)) }, onClick = { isMenuOpen = false; navigator.navigate(Routes.child(AppSection.Recordings, "edits/$recordingId")) })
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.recordings_ask_agent, agentTitle)) },
                        onClick = {
                            isMenuOpen = false
                            ChatsHooks.askAbout(navigator, AgentReference.recording(recordingId, current.view.recording.recordingTitle.ifEmpty { untitledText }))
                        },
                    )
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_details)) }, onClick = { isMenuOpen = false; isDetailsOpen = true })
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_delete), color = MaterialTheme.colorScheme.error) }, onClick = { isMenuOpen = false; isDeleting = true })
                }
                // A recording made in pieces has an original for each piece: the person says which to share.
                DropdownMenu(expanded = isOriginalsMenuOpen, onDismissRequest = { isOriginalsMenuOpen = false }) {
                    val originals = RecordingOriginals.completeFiles(current.view.mediaFiles)
                    for (media in originals) {
                        DropdownMenuItem(
                            text = { Text(RecordingOriginals.title(media, originals.size) { start, end -> context.getString(R.string.recordings_original_range, start, end) }) },
                            onClick = { isOriginalsMenuOpen = false; act(null) { connection.shareRecordingOriginal(context, media) } },
                        )
                    }
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
                    onName = { bubble -> namingBubble = bubble }, onCorrect = { segment -> correcting = segment }, onMemory = { segment -> memorySegment = segment },
                    onThisIsMe = { label ->
                        act(R.string.recordings_marked_as_you) { connection.nameRecordingTemporarySpeaker(recordingId, label, "", true, null, null) }
                    },
                    onSeparate = { label -> act(R.string.recordings_speaker_separated) { connection.separateRecordingTemporarySpeaker(recordingId, label) } },
                    onClearName = { bubble -> act(R.string.recordings_name_cleared) { connection.clearRecordingSpeakerNames(recordingId, bubble.segments) } },
                    onLoadMore = {
                        scope.launch {
                            try {
                                val more = connection.recordingTranscriptTimeline(recordingId, segmentOffset = loadedSegments.size)
                                loadedSegments = loadedSegments + more.segments
                                read = DetailRead(current.view, current.limits, current.speakers, more.copy(hasMoreSegments = more.hasMoreSegments, segments = loadedSegments), current.learning)
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
        ModalBottomSheet(onDismissRequest = { isDetailsOpen = false }) {
            RecordingDetailsSheet(
                current.view, current.learning, onResumeLearning = { act(null) { current.learning?.let { learning -> connection.resumeRecordingLearning(recordingId, learning.sourceGeneration) } } },
                onOpenKnowledge = { isDetailsOpen = false; navigator.openSection(AppSection.Knowledge) },
                onLearningSourceSaved = { reloadCount += 1 },
            )
        }
    }
    memorySegment?.let { segment -> PassageMemorySheet(recordingId, segment, onDismiss = { memorySegment = null }, onOpen = { memorySegment = null; navigator.openSection(AppSection.Knowledge) }) }
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
    onMemory: (com.teanode.kit.recordings.RecordingTranscriptSegment) -> Unit,
    onThisIsMe: (String) -> Unit,
    onSeparate: (String) -> Unit,
    onClearName: (RecordingConversationBubble) -> Unit,
    onLoadMore: () -> Unit,
) {
    val recording = read.view.recording
    val progress = RecordingServerProgress.of(read.view)
    var status = RecordingLibrary.status(recording, null, progress)
    if (status == RecordingRowStatus.Transcribing && progress.transcriptProcessingStates.isEmpty() && read.limits?.isTranscriptionAvailable == false) status = RecordingRowStatus.Ready
    val playableFiles = read.view.mediaFiles.filter { media -> media.recordingUploadState == "complete" }
    val bubbles = RecordingConversation.bubbles(segments, read.timeline?.liveUtterances.orEmpty())
    val unnamedCount = bubbles.count { bubble -> bubble.confirmedSpeakerName == null && !bubble.isLive }
    val listState = androidx.compose.foundation.lazy.rememberLazyListState()
    // A cited passage is scrolled to: the header, the player and the transcript heading come before the bubbles.
    val highlightedIndex = if (link?.passageStartMS != null) bubbles.indexOfFirst { bubble -> link.includes(bubble.segments.first()) } else -1
    LaunchedEffect(highlightedIndex, bubbles.size) { if (highlightedIndex >= 0) listState.scrollToItem(highlightedIndex + 2 + if (playableFiles.isNotEmpty()) 1 else 0) }
    LazyColumn(Modifier.fillMaxSize(), state = listState, contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 32.dp)) {
        item {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
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
            Bubble(bubble, onPlay = { player.playFrom(playableFiles, bubble.startMS) }, onName = { onName(bubble) }, onCorrect = { onCorrect(bubble.segments.first()) }, onMemory = { onMemory(bubble.segments.first()) }, onThisIsMe = onThisIsMe, onSeparate = onSeparate, onClearName = { onClearName(bubble) }, isHighlighted = link?.includes(bubble.segments.first()) == true && link.passageStartMS != null)
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
    onMemory: () -> Unit,
    onThisIsMe: (String) -> Unit,
    onSeparate: (String) -> Unit,
    onClearName: () -> Unit,
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
                // A name someone gave can be taken back: "Not me" on the person's own bubble, "Clear name" on anyone else's.
                // The person's voice recognized with no naming has nothing to take back, so "Not me" asks who it was instead.
                val hasNaming = RecordingSpeakerNaming.hasNaming(bubble)
                if (bubble.isAccountOwner) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_not_me)) }, onClick = { isMenuOpen = false; if (hasNaming) onClearName() else onName() })
                } else {
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_name_speaker)) }, onClick = { isMenuOpen = false; onName() })
                    if (hasNaming) DropdownMenuItem(text = { Text(stringResource(R.string.recordings_clear_name)) }, onClick = { isMenuOpen = false; onClearName() })
                }
                // A speaker of a capture under way is the person's own voice or not by label, in every piece at once.
                val temporaryLabel = RecordingTemporarySpeakers.labelToName(bubble)
                if (temporaryLabel != null && bubble.confirmedSpeakerName == null) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.recordings_this_is_me)) }, onClick = { isMenuOpen = false; onThisIsMe(temporaryLabel) })
                }
                // A label found to be another speaker's voice reads as that speaker: it can be given back.
                RecordingTemporarySpeakers.labelToSeparate(bubble)?.let { mergedLabel ->
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.recordings_separate_from, speaker ?: mergedLabel)) },
                        onClick = { isMenuOpen = false; onSeparate(mergedLabel) },
                    )
                }
                DropdownMenuItem(text = { Text(stringResource(R.string.recordings_correct_text)) }, onClick = { isMenuOpen = false; onCorrect() })
                DropdownMenuItem(text = { Text(stringResource(R.string.recordings_in_memory)) }, onClick = { isMenuOpen = false; onMemory() })
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
    var startText by remember { mutableStateOf(bubble.segments.first().segmentStartMS.toString()) }
    var endText by remember { mutableStateOf(bubble.segments.last().segmentEndMS.toString()) }
    var suggestions by remember { mutableStateOf(emptyList<com.teanode.kit.recordings.RecordingSpeakerNameSuggestion>()) }
    LaunchedEffect(name) {
        delay(250)
        suggestions = runCatching { connection.suggestRecordingSpeakerNames(name.trim()) }.getOrDefault(emptyList())
    }
    val first = bubble.segments.first()
    val temporaryLabel = RecordingTemporarySpeakers.labelToName(bubble)
    val context = LocalContext.current
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.recordings_name_speaker), style = MaterialTheme.typography.titleLarge)
            OutlinedTextField(value = name, onValueChange = { name = it }, singleLine = true, label = { Text(stringResource(R.string.recordings_confirmed_name)) }, modifier = Modifier.fillMaxWidth())
            if (temporaryLabel == null) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.recordings_apply_to_group), Modifier.weight(1f))
                    Switch(checked = isWholeGroup, onCheckedChange = { isWholeGroup = it })
                }
                if (!isWholeGroup) {
                    OutlinedTextField(startText, { startText = it }, singleLine = true, label = { Text(stringResource(R.string.recordings_passage_start_ms)) }, keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number), modifier = Modifier.fillMaxWidth())
                    OutlinedTextField(endText, { endText = it }, singleLine = true, label = { Text(stringResource(R.string.recordings_passage_end_ms)) }, keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = androidx.compose.ui.text.input.KeyboardType.Number), modifier = Modifier.fillMaxWidth())
                }
                Text(stringResource(R.string.recordings_apply_to_group_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            } else {
                Text(stringResource(R.string.recordings_temporary_speaker_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (suggestions.isNotEmpty()) {
                Text(stringResource(R.string.recordings_suggestions), style = MaterialTheme.typography.titleSmall)
                for (suggestion in suggestions.take(5)) Text(suggestion.confirmedName, Modifier.fillMaxWidth().clickable { name = suggestion.confirmedName }.padding(vertical = 8.dp))
            }
            Button(
                onClick = {
                    scope.launch {
                        try {
                            val chosen = suggestions.firstOrNull { suggestion -> suggestion.confirmedName == name.trim() }
                            if (temporaryLabel != null) {
                                // Named by its label: every piece shows the name at once, so there is no passage to choose.
                                connection.nameRecordingTemporarySpeaker(recordingId, temporaryLabel, name.trim(), false, chosen?.addressBookId, chosen?.contactId)
                                snackbar.show(context.getString(R.string.recordings_speaker_named))
                                onNamed()
                                return@launch
                            }
                            val interval = if (isWholeGroup) 0L..(Long.MAX_VALUE / 4) else RecordingPassageInterval.parse(startText, endText, first.segmentStartMS, bubble.segments.last().segmentEndMS)
                            if (interval == null) {
                                snackbar.show(context.getString(R.string.recordings_choose_a_positive_interval))
                                return@launch
                            }
                            connection.correctRecordingSpeaker(
                                recordingId, expectedSourceGeneration, UUID.randomUUID().toString(), name.trim(), null, suggestions.firstOrNull { suggestion -> suggestion.confirmedName == name.trim() }?.addressBookId,
                                suggestions.firstOrNull { suggestion -> suggestion.confirmedName == name.trim() }?.contactId, first.recordingTranscriptionAttemptId, first.localSpeakerId,
                                interval.first, interval.last,
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
internal fun RecordingDetailsSheet(
    view: RecordingView,
    learning: com.teanode.kit.recordings.RecordingLearningView? = null,
    onResumeLearning: () -> Unit = {},
    onOpenKnowledge: () -> Unit = {},
    onLearningSourceSaved: () -> Unit = {},
    /** The attempts of the latest transcription, for a picture taken without a server; otherwise they are read when the sheet opens. */
    previewAttempts: List<RecordingTranscriptionAttempt>? = null,
) {
    val connection = LocalConnection.current
    val latestRun = view.transcriptRuns.orEmpty().maxByOrNull { run -> run.versionNumber }
    val attempts by produceState<List<RecordingTranscriptionAttempt>?>(previewAttempts, latestRun?.id) {
        if (previewAttempts == null && latestRun != null) value = runCatching { connection.recordingTranscript(latestRun.id, pageSize = 1).attempts }.getOrNull()
    }
    var isEditingSource by remember { mutableStateOf(false) }
    Column(Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp).verticalScroll(androidx.compose.foundation.rememberScrollState()), verticalArrangement = Arrangement.spacedBy(6.dp)) {
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
        attempts?.takeIf { read -> read.isNotEmpty() }?.let { read ->
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
            Text(stringResource(R.string.recordings_transcription_usage), style = MaterialTheme.typography.titleSmall)
            for (attempt in read) AttemptUsage(attempt)
            Text(stringResource(R.string.recordings_usage_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (learning != null) {
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
            LearningCard(learning, onResumeLearning, onOpenKnowledge, onEditSource = { isEditingSource = true })
        }
    }
    val sourceId = learning?.knowledgeSource?.id
    if (isEditingSource && sourceId != null) {
        LearningSourceEditor(sourceId, onDismiss = { isEditingSource = false }, onSaved = { isEditingSource = false; onLearningSourceSaved() })
    }
}

/** One transcription attempt: the model, what it cost (actual when billing says so, else the estimate), and whether usage was reported. */
@Composable
private fun AttemptUsage(attempt: RecordingTranscriptionAttempt) {
    val cost = RecordingAttemptCost.of(attempt)
    Column(Modifier.padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(attempt.providerModelName)
        Text(
            when (cost) {
                is RecordingAttemptCost.Actual -> stringResource(R.string.recordings_cost_actual, cost.amountText, cost.currencyCode)
                is RecordingAttemptCost.Estimated -> stringResource(R.string.recordings_cost_estimated, cost.amountText, cost.currencyCode)
                RecordingAttemptCost.Unavailable -> stringResource(R.string.recordings_cost_unavailable)
            },
        )
        if (!attempt.hasProviderUsage) Text(stringResource(R.string.recordings_usage_unavailable), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The editor of the knowledge source a recording is learned through, as Settings has it; the source is read first. */
@Composable
internal fun LearningSourceEditor(sourceId: String, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val actions = com.teanode.app.settings.rememberActionRunner()
    val outcome by produceState<Result<com.teanode.kit.agent.KnowledgeSourceRow?>?>(null, sourceId) {
        value = runCatching { connection.knowledgeSourceDetails().sources.firstOrNull { source -> source.id == sourceId } }
    }
    LaunchedEffect(outcome) {
        val read = outcome ?: return@LaunchedEffect
        read.exceptionOrNull()?.let { failure -> snackbar.showError(failure) }
        if (read.getOrNull() == null) onDismiss()
    }
    outcome?.getOrNull()?.let { source -> com.teanode.app.settings.KnowledgeSourceEditorDialog(source, actions, onDismiss = onDismiss, onSaved = onSaved) }
}

/** The Learning card: whether the agent reads this recording's transcript into its knowledge, and how far it has come. */
@Composable
internal fun LearningCard(learning: com.teanode.kit.recordings.RecordingLearningView, onResume: () -> Unit, onOpenKnowledge: () -> Unit, onEditSource: () -> Unit = {}) {
    val context = LocalContext.current
    Text(stringResource(R.string.recordings_learning), style = MaterialTheme.typography.titleSmall)
    when (learning.state) {
        com.teanode.kit.recordings.RecordingLearningState.StoppedReading -> {
            Text(stringResource(R.string.recordings_learning_stopped), color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton(onClick = onResume) { Text(stringResource(R.string.recordings_learning_resume)) }
            Text(stringResource(R.string.recordings_learning_stopped_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        com.teanode.kit.recordings.RecordingLearningState.NoAgent -> Text(stringResource(R.string.recordings_learning_no_agent), color = MaterialTheme.colorScheme.onSurfaceVariant)
        com.teanode.kit.recordings.RecordingLearningState.WaitingForTranscript -> Text(stringResource(R.string.recordings_learning_waiting), color = MaterialTheme.colorScheme.onSurfaceVariant)
        com.teanode.kit.recordings.RecordingLearningState.Reading -> {
            val source = learning.knowledgeSource!!
            LearningLine(stringResource(R.string.recordings_learning_pending), learning.pendingDocumentCount.toString())
            LearningLine(stringResource(R.string.recordings_learning_digested), learning.digestedDocumentCount.toString())
            ServerTime.instant(source.lastRunAt)?.let { instant -> LearningLine(stringResource(R.string.recordings_learning_last_pass), java.time.format.DateTimeFormatter.ofLocalizedDate(java.time.format.FormatStyle.MEDIUM).format(instant.atZone(java.time.ZoneId.systemDefault()))) }
            val next = source.nextRunEpochMS
            if (next != null) LearningLine(stringResource(R.string.recordings_learning_next_pass), com.teanode.app.ui.components.RelativeTime.text(context, next))
            else if (source.cron.isEmpty()) Text(stringResource(R.string.recordings_learning_only_when_asked), color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (source.lastError.isNotEmpty()) Text(source.lastError.asReasonSentence(), color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedButton(onClick = onOpenKnowledge) { Text(stringResource(R.string.recordings_learning_knowledge_source)) }
            OutlinedButton(onClick = onEditSource) { Text(stringResource(R.string.recordings_edit_source)) }
        }
    }
    if (learning.isCurationPending) Text(stringResource(R.string.recordings_learning_curation), color = MaterialTheme.colorScheme.onSurfaceVariant)
    Text(stringResource(R.string.recordings_learning_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
private fun LearningLine(label: String, text: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) { Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant); Text(text) }
}
