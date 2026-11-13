package com.teanode.app.chats

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.FolderOpen
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material.icons.outlined.Keyboard
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.PhotoCamera
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.chats.MentionChoice
import com.teanode.kit.chats.ChatMention
import kotlinx.coroutines.launch
import java.io.File
import java.util.UUID

/**
 * Composer is where the person writes: a field with its buttons inside it, the words on top, adding files at the bottom
 * left, dictating and sending at the bottom right; and in a chat a button to hold to talk.
 */
@Composable
fun Composer(model: ConversationModel, environment: ChatsEnvironment, dictation: Dictation, placeholderOverride: String? = null) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var loadingPictureCount by remember { mutableIntStateOf(0) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var cameraFile by remember { mutableStateOf<File?>(null) }
    val isVoiceMode = model.isChat && model.isVoiceMessageAvailable && model.canWrite && environment.composerMode == "voice" && model.pendingFiles.isEmpty()

    val photoPicker = rememberLauncherForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(8)) { addresses ->
        if (addresses.isEmpty()) return@rememberLauncherForActivityResult
        scope.launch {
            loadingPictureCount += addresses.size
            for (address in addresses) {
                val file = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { PendingFiles.picture(context, address, model.pendingFiles.size) }
                loadingPictureCount -= 1
                if (file != null) model.addPendingFile(file)
            }
        }
    }
    val filePicker = rememberLauncherForActivityResult(ActivityResultContracts.OpenMultipleDocuments()) { addresses ->
        scope.launch {
            for (address in addresses) {
                val file = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { PendingFiles.file(context, address) }
                if (file != null) model.addPendingFile(file)
            }
        }
    }
    val camera = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { isTaken ->
        val taken = cameraFile
        cameraFile = null
        if (isTaken && taken != null) {
            scope.launch {
                val file = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { PendingFiles.picture(taken.readBytes(), model.pendingFiles.size) }
                taken.delete()
                if (file != null) model.addPendingFile(file)
            }
        }
    }
    fun takePicture() {
        val target = File(File(context.cacheDir, "chat-files").also { made -> made.mkdirs() }, "camera-${UUID.randomUUID()}.jpg")
        cameraFile = target
        camera.launch(FileProvider.getUriForFile(context, "${context.packageName}.chatfiles", target))
    }
    val cameraPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { isGranted -> if (isGranted) takePicture() }
    val microphonePermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { isGranted ->
        if (!isGranted) snackbar.showProblem(context.getString(R.string.chats_microphone_not_allowed))
    }

    DisposableEffect(Unit) {
        onDispose {
            dictation.stop()
            model.voiceRecorder.cancel()
        }
    }

    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        model.failedRecording?.let { recording ->
            FailedRecordingStrip(recording, onRetry = { model.retryVoiceMessage() }, onDiscard = { model.discardFailedRecording() })
        }
        if (isVoiceMode && (model.voiceRecorder.isRecording || model.voiceRecorder.hintText != null)) RecordingStrip(model.voiceRecorder)
        if (model.pendingFiles.isNotEmpty() || loadingPictureCount > 0) {
            LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                items(model.pendingFiles, key = { file -> file.id }) { file ->
                    PendingFileChip(file, model.uploadingFileIds.contains(file.id), model.uploadedFileIds.contains(file.id)) { model.removePendingFile(file.id) }
                }
                items(loadingPictureCount) { LoadingFileChip() }
            }
        }
        if (isVoiceMode) {
            Surface(shape = RoundedCornerShape(26.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh, tonalElevation = 2.dp) {
                Row(Modifier.padding(6.dp), horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                    IconButton(onClick = { environment.chooseComposerMode("keyboard") }) {
                        Icon(Icons.Outlined.Keyboard, contentDescription = stringResource(R.string.chats_use_keyboard))
                    }
                    HoldToTalkButton(
                        model.voiceRecorder, Modifier.weight(1f),
                        onStart = {
                            if (!model.voiceRecorder.hasPermission()) microphonePermission.launch(Manifest.permission.RECORD_AUDIO)
                            model.beginVoiceMessage()
                        },
                        onEnd = { isCancelled -> model.endVoiceMessage(isCancelled) },
                    )
                }
            }
        } else {
            Surface(shape = RoundedCornerShape(26.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh, tonalElevation = 2.dp) {
                Column(Modifier.fillMaxWidth().padding(top = 6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    val placeholder = placeholderOverride ?: composerPlaceholder(model, environment)
                    Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
                        if (model.draft.isEmpty()) Text(placeholder, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyLarge)
                        BasicTextField(
                            value = model.draft, onValueChange = { text -> model.updateDraft(text) }, enabled = model.canWrite,
                            textStyle = MaterialTheme.typography.bodyLarge.copy(color = MaterialTheme.colorScheme.onSurface),
                            cursorBrush = androidx.compose.ui.graphics.SolidColor(MaterialTheme.colorScheme.primary), maxLines = 8,
                            keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Sentences),
                            modifier = Modifier.fillMaxWidth().semantics { contentDescription = placeholder },
                        )
                    }
                    // The words being heard, until they are final and join the box.
                    if (dictation.captionText.isNotEmpty()) {
                        Text(dictation.captionText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, modifier = Modifier.padding(horizontal = 16.dp))
                    }
                    Row(Modifier.fillMaxWidth().padding(horizontal = 6.dp).padding(bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                        Box {
                            IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.chats_add_files)) }
                            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.chats_photo_library)) }, leadingIcon = { Icon(Icons.Outlined.PhotoLibrary, null) },
                                    onClick = {
                                        isMenuOpen = false
                                        photoPicker.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly))
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.chats_camera)) }, leadingIcon = { Icon(Icons.Outlined.PhotoCamera, null) },
                                    onClick = {
                                        isMenuOpen = false
                                        if (androidx.core.content.ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) == android.content.pm.PackageManager.PERMISSION_GRANTED) takePicture()
                                        else cameraPermission.launch(Manifest.permission.CAMERA)
                                    },
                                )
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.chats_files)) }, leadingIcon = { Icon(Icons.Outlined.FolderOpen, null) },
                                    onClick = {
                                        isMenuOpen = false
                                        filePicker.launch(arrayOf("*/*"))
                                    },
                                )
                            }
                        }
                        Box(Modifier.weight(1f))
                        if (model.canDictate && model.conversation?.kind != "run") {
                            IconButton(
                                onClick = {
                                    if (!dictation.hasPermission()) {
                                        microphonePermission.launch(Manifest.permission.RECORD_AUDIO)
                                    } else {
                                        dictation.toggle({ model.draft }, { text -> model.updateDraft(text) }, if (model.isChat) Dictation.Place.Chat else Dictation.Place.Agent, connection, model.voiceSampleRate ?: 24_000)
                                    }
                                },
                            ) {
                                Icon(
                                    Icons.Outlined.Mic, contentDescription = stringResource(if (dictation.isDictating) R.string.chats_stop_dictating else R.string.chats_dictate),
                                    tint = if (dictation.isDictating) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                        // In a chat the waveform is for speaking a message: it turns the box into a button to hold.
                        if (model.isChat && model.isVoiceMessageAvailable && model.canWrite && model.draft.isEmpty() && model.pendingFiles.isEmpty()) {
                            IconButton(onClick = {
                                dictation.stop()
                                environment.chooseComposerMode("voice")
                            }) { Icon(Icons.Outlined.GraphicEq, contentDescription = stringResource(R.string.chats_record_voice_messages), tint = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                        if (model.runningRunId != null && model.draft.isEmpty() && model.pendingFiles.isEmpty()) {
                            IconButton(onClick = { model.stop() }) {
                                Box(Modifier.size(36.dp).clip(CircleShape).background(MaterialTheme.colorScheme.onSurface), contentAlignment = Alignment.Center) {
                                    Icon(Icons.Outlined.Stop, contentDescription = stringResource(R.string.chats_stop), tint = MaterialTheme.colorScheme.surface, modifier = Modifier.size(18.dp))
                                }
                            }
                        } else {
                            IconButton(onClick = {
                                dictation.stop()
                                model.send()
                            }, enabled = model.canSend) {
                                Box(
                                    Modifier.size(36.dp).clip(CircleShape).background(if (model.canSend || model.isSending) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.outline),
                                    contentAlignment = Alignment.Center,
                                ) {
                                    if (model.isSending) {
                                        CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary)
                                    } else {
                                        Icon(Icons.AutoMirrored.Outlined.Send, contentDescription = stringResource(R.string.chats_send), tint = MaterialTheme.colorScheme.onPrimary, modifier = Modifier.size(18.dp))
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun composerPlaceholder(model: ConversationModel, environment: ChatsEnvironment): String {
    if (model.isChat) {
        // Where an agent can be asked, the box says how.
        return if (model.addressableAgents.isNotEmpty()) stringResource(R.string.chats_message_or_at) else stringResource(R.string.chats_message)
    }
    if (model.freshQuestion != null) return stringResource(R.string.chats_your_answer)
    return if (environment.agentName.isEmpty()) stringResource(R.string.chats_ask_your_agent) else stringResource(R.string.chats_ask_agent, environment.agentName)
}

/** The wide button held to talk: down starts recording, up sends it, and sliding up before letting go throws it away. */
@Composable
fun HoldToTalkButton(recorder: VoiceMessageRecorder, modifier: Modifier = Modifier, onStart: () -> Unit, onEnd: (isCancelled: Boolean) -> Unit) {
    val cancelDragPixels = with(LocalDensity.current) { 80.dp.toPx() }
    val fill = when {
        recorder.isRecording && recorder.isCancelArmed -> MaterialTheme.colorScheme.error
        recorder.isRecording -> MaterialTheme.colorScheme.primary
        else -> MaterialTheme.colorScheme.surfaceContainerHighest
    }
    val words = when {
        recorder.isRecording && recorder.isCancelArmed -> stringResource(R.string.chats_release_to_cancel)
        recorder.isRecording -> stringResource(R.string.chats_release_to_send)
        else -> stringResource(R.string.chats_hold_to_talk)
    }
    Row(
        modifier.heightIn(min = 48.dp).clip(CircleShape).background(fill)
            .pointerInput(Unit) {
                awaitEachGesture {
                    val down = awaitFirstDown()
                    onStart()
                    var isArmed = false
                    while (true) {
                        val event = awaitPointerEvent()
                        val change = event.changes.firstOrNull { pointer -> pointer.id == down.id } ?: break
                        if (!change.pressed) break
                        isArmed = change.position.y - down.position.y < -cancelDragPixels
                        if (recorder.isCancelArmed != isArmed) recorder.isCancelArmed = isArmed
                    }
                    onEnd(isArmed)
                }
            }
            .semantics { contentDescription = words },
        horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            if (recorder.isRecording && recorder.isCancelArmed) Icons.Outlined.DeleteOutline else Icons.Outlined.Mic, contentDescription = null,
            tint = if (recorder.isRecording) Color.White else MaterialTheme.colorScheme.onSurface,
        )
        Text(
            words, modifier = Modifier.padding(start = 8.dp), style = MaterialTheme.typography.labelLarge,
            color = if (recorder.isRecording) Color.White else MaterialTheme.colorScheme.onSurface,
        )
    }
}

/** Floats over the composer while a voice message is being recorded: the time, the level, and what letting go does. With nothing recording it holds a hint for a moment. */
@Composable
fun RecordingStrip(recorder: VoiceMessageRecorder) {
    Surface(shape = RoundedCornerShape(24.dp), color = MaterialTheme.colorScheme.surfaceContainerHighest, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (recorder.isRecording) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                    Icon(
                        if (recorder.isCancelArmed) Icons.Outlined.DeleteOutline else Icons.Outlined.Mic, contentDescription = null,
                        tint = if (recorder.isCancelArmed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary,
                    )
                    Text(
                        if (recorder.isCountingDown) stringResource(R.string.chats_time_left, VoiceMessageRecorder.clockText(recorder.secondsLeft)) else VoiceMessageRecorder.clockText(recorder.elapsedSeconds),
                        style = MaterialTheme.typography.bodyLarge,
                    )
                    Box(Modifier.size(width = (8 + 40 * recorder.level).dp, height = 6.dp).clip(CircleShape).background(MaterialTheme.colorScheme.primary))
                }
                Text(
                    stringResource(if (recorder.isCancelArmed) R.string.chats_release_to_cancel else R.string.chats_slide_up_to_cancel), style = MaterialTheme.typography.bodySmall,
                    color = if (recorder.isCancelArmed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else {
                recorder.hintText?.let { hint -> Text(hint, style = MaterialTheme.typography.bodyMedium) }
            }
        }
    }
}

/** A voice message that could not be sent, kept above the composer to send again or throw away. */
@Composable
fun FailedRecordingStrip(recording: VoiceRecording, onRetry: () -> Unit, onDiscard: () -> Unit) {
    Surface(shape = RoundedCornerShape(20.dp), color = MaterialTheme.colorScheme.surfaceContainerHighest, modifier = Modifier.fillMaxWidth()) {
        Row(Modifier.padding(start = 14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(Icons.Outlined.WarningAmber, contentDescription = null, modifier = Modifier.size(20.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Column(Modifier.weight(1f).padding(vertical = 6.dp)) {
                Text(stringResource(R.string.chats_voice_message_not_sent), style = MaterialTheme.typography.labelLarge, maxLines = 2)
                Text(VoiceMessageRecorder.clockText(recording.durationSeconds), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            OutlinedButton(onClick = onRetry) { Text(stringResource(R.string.chats_try_again)) }
            IconButton(onClick = onDiscard) { Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.chats_throw_away_recording), modifier = Modifier.size(18.dp)) }
        }
    }
}

/**
 * MentionMenu offers what an @ in the composer can mention or ask: a row for each with its symbol, its name and what it
 * is. The person's own agent comes first, then everybody in a group, then each of the others followed by their agent
 * when its owner lets the others ask. A person and their agent are two rows, named apart: mentioning a person tells
 * them and asks nothing of their agent.
 */
@Composable
fun MentionMenu(choices: List<MentionChoice>, words: ChatWords, onChoose: (MentionChoice) -> Unit) {
    val menuLabel = stringResource(R.string.chats_people_and_agents_to_mention)
    Surface(
        Modifier.fillMaxWidth().padding(horizontal = 16.dp).semantics { contentDescription = menuLabel }, shape = RoundedCornerShape(18.dp),
        color = MaterialTheme.colorScheme.surfaceContainerHighest, tonalElevation = 3.dp,
    ) {
        LazyColumn(Modifier.heightIn(max = 280.dp)) {
            items(choices, key = { choice -> choice.identity }) { choice ->
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 56.dp).clickable { onChoose(choice) }.padding(horizontal = 16.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Box(Modifier.size(34.dp).clip(CircleShape).background(MaterialTheme.colorScheme.onSurface.copy(alpha = 0.08f)), contentAlignment = Alignment.Center) {
                        Icon(
                            when (choice) {
                                is MentionChoice.Agent -> Icons.Outlined.AutoAwesome
                                is MentionChoice.Person -> Icons.Outlined.Person
                                MentionChoice.Everybody -> Icons.Outlined.People
                            },
                            contentDescription = null, modifier = Modifier.size(18.dp),
                        )
                    }
                    Column {
                        Text(ChatMention.word(choice, words.allMentionWord), style = MaterialTheme.typography.titleSmall)
                        Text(
                            when (choice) {
                                is MentionChoice.Agent -> if (choice.agent.isOwn) stringResource(R.string.chats_yours) else words.agentOf(choice.agent.ownerName)
                                is MentionChoice.Person -> stringResource(R.string.chats_tells_them)
                                MentionChoice.Everybody -> stringResource(R.string.chats_everyone_in_chat)
                            },
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                HorizontalDivider(Modifier.padding(start = 62.dp))
            }
        }
    }
}
