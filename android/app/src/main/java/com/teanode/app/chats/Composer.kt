package com.teanode.app.chats

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.animateContentSize
import androidx.compose.animation.core.LinearOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
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
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Call
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentPaste
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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.isShiftPressed
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.layout.layoutId
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import androidx.core.content.FileProvider
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.chats.ChatMention
import com.teanode.kit.chats.ComposerLayout
import com.teanode.kit.chats.ComposerShape
import com.teanode.kit.chats.MentionChoice
import com.teanode.kit.chats.VoiceMessageRules
import com.teanode.app.calls.OpeningRequests
import kotlinx.coroutines.launch
import java.io.File
import java.util.UUID

/**
 * Composer is where the person writes. At rest it is one line: the add button, the words, dictation and send. When the
 * words would wrap, or something is attached, the same box becomes a card with the words across the full width and the
 * buttons in a row under them. In a chat it can instead be a bar to hold and talk. Adding files, a call with the
 * agent and a voice message are all in the add button's menu.
 */
@Composable
fun Composer(model: ConversationModel, environment: ChatsEnvironment, dictation: Dictation, placeholderOverride: String? = null) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var loadingPictureCount by remember { mutableIntStateOf(0) }
    var isMenuOpen by remember { mutableStateOf(false) }
    // Looked at when the menu opens, as the clipboard can change at any time.
    val pastedPictureAddress = remember(isMenuOpen) { if (isMenuOpen) PendingFiles.pastedPictureAddress(context) else null }
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

    val isTalkOffered = !model.isChat && model.voiceSampleRate != null && model.conversation?.kind != "run"
    val isVoiceMessageOffered = model.isChat && model.isVoiceMessageAvailable && model.canWrite
    val isDictationOffered = model.canDictate && model.conversation?.kind != "run"

    Box(Modifier.fillMaxWidth().padding(horizontal = ComposerLayout.SIDE_MARGIN_DP.dp).padding(bottom = ComposerLayout.BOTTOM_MARGIN_DP.dp), contentAlignment = Alignment.BottomCenter) {
        Column(Modifier.widthIn(max = ComposerLayout.MAX_WIDTH_DP.dp).fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            model.failedRecording?.let { recording ->
                FailedRecordingStrip(recording, onRetry = { model.retryVoiceMessage() }, onDiscard = { model.discardFailedRecording() })
            }
            if (isVoiceMode) {
                VoiceHint(model.voiceRecorder, environment)
                Surface(shape = RoundedCornerShape(ComposerLayout.CORNER_DP.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh, tonalElevation = 2.dp) {
                    VoiceMessageBar(
                        model.voiceRecorder, environment,
                        onKeyboard = { environment.chooseComposerMode("keyboard") },
                        onStart = {
                            if (!model.voiceRecorder.hasPermission()) microphonePermission.launch(Manifest.permission.RECORD_AUDIO)
                            model.beginVoiceMessage()
                        },
                        onEnd = { isCancelled -> model.endVoiceMessage(isCancelled) },
                    )
                }
            } else {
                // The words being heard, until they are final and join the box.
                if (dictation.captionText.isNotEmpty()) {
                    Text(dictation.captionText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, modifier = Modifier.padding(horizontal = 16.dp))
                }
                TextComposer(
                    model = model,
                    placeholder = placeholderOverride ?: composerPlaceholder(model, environment),
                    hasAttachments = model.pendingFiles.isNotEmpty() || loadingPictureCount > 0,
                    isDictationOffered = isDictationOffered,
                    isDictating = dictation.isDictating,
                    attachments = {
                        LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            items(model.pendingFiles, key = { file -> file.id }) { file ->
                                PendingFileChip(file, model.uploadingFileIds.contains(file.id), model.uploadedFileIds.contains(file.id)) { model.removePendingFile(file.id) }
                            }
                            items(loadingPictureCount) { LoadingFileChip() }
                        }
                    },
                    plusMenu = { buttonDp, targetDp ->
                        if (model.canWrite) {
                            Box {
                                ComposerRoundButton(
                                    onClick = { isMenuOpen = true }, label = stringResource(R.string.chats_add_files), buttonDp = buttonDp, targetDp = targetDp,
                                    fill = MaterialTheme.colorScheme.surfaceContainerHighest,
                                ) { Icon(Icons.Outlined.Add, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant) }
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
                                    // A screenshot just taken, pasted as the dashboard takes one pasted into its box.
                                    if (pastedPictureAddress != null) {
                                        DropdownMenuItem(
                                            text = { Text(stringResource(R.string.chats_paste_picture)) }, leadingIcon = { Icon(Icons.Outlined.ContentPaste, null) },
                                            onClick = {
                                                isMenuOpen = false
                                                pastedPictureAddress?.let { address ->
                                                    scope.launch {
                                                        val file = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { PendingFiles.picture(context, address, model.pendingFiles.size) }
                                                        if (file != null) model.addPendingFile(file)
                                                    }
                                                }
                                            },
                                        )
                                    }
                                    if (isTalkOffered || isVoiceMessageOffered) HorizontalDivider()
                                    if (isTalkOffered) {
                                        DropdownMenuItem(
                                            text = { Text(stringResource(R.string.chats_talk)) }, leadingIcon = { Icon(Icons.Outlined.Call, null) },
                                            onClick = {
                                                isMenuOpen = false
                                                dictation.stop()
                                                OpeningRequests.requestCall()
                                            },
                                        )
                                    }
                                    if (isVoiceMessageOffered) {
                                        DropdownMenuItem(
                                            text = { Text(stringResource(R.string.chats_voice_message)) }, leadingIcon = { Icon(Icons.Outlined.GraphicEq, null) },
                                            onClick = {
                                                isMenuOpen = false
                                                dictation.stop()
                                                environment.chooseComposerMode("voice")
                                            },
                                        )
                                    }
                                }
                            }
                        }
                    },
                    micButton = { buttonDp, targetDp ->
                        ComposerRoundButton(
                            onClick = {
                                if (!dictation.hasPermission()) {
                                    microphonePermission.launch(Manifest.permission.RECORD_AUDIO)
                                } else {
                                    dictation.toggle({ model.draft }, { text -> model.updateDraft(text) }, if (model.isChat) Dictation.Place.Chat else Dictation.Place.Agent, connection, model.voiceSampleRate ?: 24_000)
                                }
                            },
                            label = stringResource(if (dictation.isDictating) R.string.chats_stop_dictating else R.string.chats_dictate), buttonDp = buttonDp, targetDp = targetDp,
                            fill = if (dictation.isDictating) MaterialTheme.colorScheme.primary.copy(alpha = 0.18f) else MaterialTheme.colorScheme.surfaceContainerHighest,
                        ) {
                            Icon(
                                Icons.Outlined.Mic, contentDescription = null,
                                tint = if (dictation.isDictating) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    },
                    sendButton = { buttonDp, targetDp ->
                        if (model.runningRunId != null && model.draft.isEmpty() && model.pendingFiles.isEmpty()) {
                            ComposerRoundButton(
                                onClick = { model.stop() }, label = stringResource(R.string.chats_stop), buttonDp = buttonDp, targetDp = targetDp,
                                fill = MaterialTheme.colorScheme.onSurface,
                            ) { Icon(Icons.Outlined.Stop, contentDescription = null, tint = MaterialTheme.colorScheme.surface, modifier = Modifier.size(18.dp)) }
                        } else {
                            ComposerRoundButton(
                                onClick = {
                                    dictation.stop()
                                    model.send()
                                },
                                label = stringResource(R.string.chats_send), buttonDp = buttonDp, targetDp = targetDp, isEnabled = model.canSend,
                                fill = if (model.canSend || model.isSending) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.12f),
                            ) {
                                if (model.isSending) {
                                    CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary)
                                } else {
                                    Icon(Icons.Outlined.ArrowUpward, contentDescription = null, tint = if (model.canSend) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.38f), modifier = Modifier.size(20.dp))
                                }
                            }
                        }
                    },
                    onSend = {
                        dictation.stop()
                        model.send()
                    },
                )
            }
        }
    }
}

/** A round button: a circle of [buttonDp] in a touch target of [targetDp]. */
@Composable
fun ComposerRoundButton(
    onClick: () -> Unit,
    label: String,
    buttonDp: Float,
    targetDp: Float,
    fill: androidx.compose.ui.graphics.Color,
    isEnabled: Boolean = true,
    content: @Composable () -> Unit,
) {
    Box(
        Modifier.size(targetDp.dp).clip(CircleShape).semantics { contentDescription = label }.clickable(enabled = isEnabled, role = Role.Button, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Box(Modifier.size(buttonDp.dp).clip(CircleShape).background(fill), contentAlignment = Alignment.Center) {
            // The symbol grows with the circle.
            Box(Modifier.graphicsLayer { scaleX = buttonDp / ComposerLayout.BUTTON_DP; scaleY = buttonDp / ComposerLayout.BUTTON_DP }, contentAlignment = Alignment.Center) { content() }
        }
    }
}

/** The part of the text composer's layout the slots go by. */
private enum class ComposerSlot { Attachments, Plus, Field, Mic, Send }

/**
 * The words and their buttons, in one of two shapes of the same box. The text field is the same one in both, so the
 * caret, the keyboard and a word being composed survive the change. It is a pill until the words would wrap at the pill's
 * width, and a card then, or while something is attached.
 */
@Composable
private fun TextComposer(
    model: ConversationModel,
    placeholder: String,
    hasAttachments: Boolean,
    isDictationOffered: Boolean,
    isDictating: Boolean,
    attachments: @Composable () -> Unit,
    plusMenu: @Composable (buttonDp: Float, targetDp: Float) -> Unit,
    micButton: @Composable (buttonDp: Float, targetDp: Float) -> Unit,
    sendButton: @Composable (buttonDp: Float, targetDp: Float) -> Unit,
    onSend: () -> Unit,
) {
    val density = LocalDensity.current
    val fontScale = density.fontScale
    val buttonDp = ComposerLayout.buttonDp(fontScale)
    val targetDp = ComposerLayout.targetDp(fontScale)
    val screenWidthDp = LocalConfiguration.current.screenWidthDp.toFloat()
    val textStyle = MaterialTheme.typography.bodyLarge.copy(color = MaterialTheme.colorScheme.onSurface)
    val textMeasurer = rememberTextMeasurer()
    val focusRequester = remember { FocusRequester() }
    val fieldPaddingDp = 8f
    val besideCount = (if (model.canWrite) 1 else 0) + (if (isDictationOffered) 1 else 0) + 1
    // The width the words have as a pill, measured only while it is one; until then, estimated.
    var measuredPillWidthDp by remember { mutableStateOf<Float?>(null) }
    val pillFieldWidthDp = measuredPillWidthDp ?: ComposerLayout.estimatedPillFieldWidthDp(screenWidthDp, besideCount)
    val doesDraftWrap = remember(model.draft, pillFieldWidthDp, textStyle, density) {
        if (model.draft.isEmpty()) {
            false
        } else if (model.draft.contains('\n')) {
            true
        } else {
            val textWidthPixels = with(density) { maxOf(0f, pillFieldWidthDp - 2 * fieldPaddingDp).dp.roundToPx() }
            textMeasurer.measure(model.draft, textStyle, constraints = Constraints(maxWidth = textWidthPixels)).lineCount > 1
        }
    }
    val shape = ComposerLayout.shapeFor(hasAttachments, doesDraftWrap)
    val isCard = shape == ComposerShape.Card
    val targetPixels = with(density) { targetDp.dp.roundToPx() }
    val insetPixels = with(density) { ComposerLayout.INSET_DP.dp.roundToPx() }

    Surface(
        shape = RoundedCornerShape(ComposerLayout.CORNER_DP.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh, tonalElevation = 2.dp,
        modifier = Modifier.pointerInput(Unit) { detectTapGestures { focusRequester.requestFocus() } },
    ) {
        Layout(
            content = {
                if (hasAttachments) Box(Modifier.layoutId(ComposerSlot.Attachments)) { attachments() }
                Box(Modifier.layoutId(ComposerSlot.Plus)) { plusMenu(buttonDp, targetDp) }
                Box(
                    Modifier.layoutId(ComposerSlot.Field).onSizeChanged { size ->
                        // Only a pill's width is the pill's width; a card's is wider.
                        if (!isCard) measuredPillWidthDp = ComposerLayout.acceptedPillFieldWidthDp(measuredPillWidthDp, with(density) { size.width.toDp().value })
                    },
                ) {
                    Box(Modifier.fillMaxWidth().heightIn(min = targetDp.dp).padding(horizontal = fieldPaddingDp.dp, vertical = 12.dp), contentAlignment = Alignment.CenterStart) {
                        if (model.draft.isEmpty()) {
                            Text(placeholder, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        BasicTextField(
                            value = model.draft, onValueChange = { text -> model.updateDraft(text) }, enabled = model.canWrite,
                            textStyle = textStyle, cursorBrush = SolidColor(MaterialTheme.colorScheme.primary), maxLines = 8,
                            keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Sentences),
                            modifier = Modifier.fillMaxWidth().focusRequester(focusRequester).semantics { contentDescription = placeholder }.onPreviewKeyEvent { event ->
                                // A keyboard: Return sends, Shift and Return makes a new line, Escape drops the reply.
                                if (event.type != KeyEventType.KeyDown) return@onPreviewKeyEvent false
                                when {
                                    (event.key == Key.Enter || event.key == Key.NumPadEnter) && !event.isShiftPressed -> {
                                        if (model.canSend) onSend()
                                        true
                                    }
                                    event.key == Key.Escape -> {
                                        val reply = model.replyingTo
                                        if (reply != null) model.dropReference(reply)
                                        reply != null
                                    }
                                    else -> false
                                }
                            },
                        )
                    }
                }
                if (isDictationOffered) Box(Modifier.layoutId(ComposerSlot.Mic)) { micButton(buttonDp, targetDp) }
                Box(Modifier.layoutId(ComposerSlot.Send)) { sendButton(buttonDp, targetDp) }
            },
            modifier = Modifier.padding(ComposerLayout.INSET_DP.dp).animateContentSize(tween(150, easing = LinearOutSlowInEasing)),
        ) { measurables, constraints ->
            fun slot(which: ComposerSlot) = measurables.firstOrNull { measurable -> measurable.layoutId == which }
            val width = constraints.maxWidth
            val buttonConstraints = Constraints.fixed(targetPixels, targetPixels)
            val plus = slot(ComposerSlot.Plus)?.measure(buttonConstraints)
            val mic = slot(ComposerSlot.Mic)?.measure(buttonConstraints)
            val send = slot(ComposerSlot.Send)?.measure(buttonConstraints)
            val plusWidth = plus?.width ?: 0
            val micWidth = mic?.width ?: 0
            val sendWidth = send?.width ?: 0
            if (!isCard) {
                val field = slot(ComposerSlot.Field)!!.measure(Constraints(minWidth = maxOf(0, width - plusWidth - micWidth - sendWidth), maxWidth = maxOf(0, width - plusWidth - micWidth - sendWidth)))
                val height = maxOf(targetPixels, field.height)
                layout(width, height) {
                    plus?.placeRelative(0, height - plus.height)
                    field.placeRelative(plusWidth, height - field.height)
                    mic?.placeRelative(width - sendWidth - micWidth, height - mic.height)
                    send?.placeRelative(width - sendWidth, height - send.height)
                }
            } else {
                val strip = slot(ComposerSlot.Attachments)?.measure(Constraints(maxWidth = width))
                val field = slot(ComposerSlot.Field)!!.measure(Constraints(minWidth = width, maxWidth = width))
                val stripHeight = if (strip != null) strip.height + insetPixels else 0
                val height = stripHeight + field.height + targetPixels
                layout(width, height) {
                    strip?.placeRelative(0, 0)
                    field.placeRelative(0, stripHeight)
                    val rowTop = stripHeight + field.height
                    plus?.placeRelative(0, rowTop)
                    mic?.placeRelative(width - sendWidth - micWidth, rowTop)
                    send?.placeRelative(width - sendWidth, rowTop)
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

/** A word of guidance over the voice bar: the first time it explains hold, let go and slide; later it only shows what just went wrong. */
@Composable
private fun VoiceHint(recorder: VoiceMessageRecorder, environment: ChatsEnvironment) {
    val words = recorder.hintText ?: if (!environment.hasSeenVoiceHint && !recorder.isRecording) stringResource(R.string.chats_hold_to_record_hint) else null
    if (words != null) {
        Text(words, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp))
    }
}

/**
 * The bar of a voice message, the same box as the text composer: a button back to the keyboard, a thin separator and
 * the area to hold. Holding records and letting go sends; sliding up 80 dp and letting go throws the recording away.
 */
@Composable
fun VoiceMessageBar(
    recorder: VoiceMessageRecorder,
    environment: ChatsEnvironment,
    onKeyboard: () -> Unit,
    onStart: () -> Unit,
    onEnd: (isCancelled: Boolean) -> Unit,
) {
    val fontScale = LocalDensity.current.fontScale
    val buttonDp = ComposerLayout.buttonDp(fontScale)
    val targetDp = ComposerLayout.targetDp(fontScale)
    Row(Modifier.padding(ComposerLayout.INSET_DP.dp), verticalAlignment = Alignment.CenterVertically) {
        ComposerRoundButton(
            onClick = onKeyboard, label = stringResource(R.string.chats_use_keyboard), buttonDp = buttonDp, targetDp = targetDp,
            fill = MaterialTheme.colorScheme.surfaceContainerHighest,
        ) { Icon(Icons.Outlined.Keyboard, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant) }
        Box(Modifier.padding(horizontal = 8.dp).width(1.dp).height((buttonDp * 0.7f).dp).background(MaterialTheme.colorScheme.outlineVariant))
        HoldToTalkArea(recorder, environment, Modifier.weight(1f), targetDp, onStart, onEnd)
    }
}

/** The area held to talk: down starts recording, up sends it, and sliding up before letting go throws it away. */
@Composable
fun HoldToTalkArea(
    recorder: VoiceMessageRecorder,
    environment: ChatsEnvironment,
    modifier: Modifier = Modifier,
    minimumHeightDp: Float = 48f,
    onStart: () -> Unit,
    onEnd: (isCancelled: Boolean) -> Unit,
) {
    val density = LocalDensity.current
    val haptics = LocalHapticFeedback.current
    val tint by animateColorAsState(
        if (recorder.isRecording && recorder.isCancelArmed) MaterialTheme.colorScheme.error.copy(alpha = 0.18f) else androidx.compose.ui.graphics.Color.Transparent,
        tween(150, easing = LinearOutSlowInEasing), label = "cancel tint",
    )
    val idleWords = stringResource(R.string.chats_hold_to_talk)
    val recordingWords = if (recorder.isCancelArmed) stringResource(R.string.chats_let_go_to_cancel) else stringResource(R.string.chats_slide_up_to_cancel)
    Box(
        modifier.heightIn(min = minimumHeightDp.dp).clip(RoundedCornerShape(20.dp)).background(tint)
            .pointerInput(Unit) {
                awaitEachGesture {
                    val down = awaitFirstDown()
                    environment.markVoiceHintSeen()
                    haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove)
                    onStart()
                    var isArmed = false
                    while (true) {
                        val event = awaitPointerEvent()
                        val change = event.changes.firstOrNull { pointer -> pointer.id == down.id } ?: break
                        if (!change.pressed) break
                        val dragUpDp = (down.position.y - change.position.y) / density.density
                        val isNowArmed = VoiceMessageRules.isCancelArmed(dragUpDp)
                        if (isNowArmed != isArmed) {
                            isArmed = isNowArmed
                            recorder.isCancelArmed = isArmed
                            if (isArmed) haptics.performHapticFeedback(HapticFeedbackType.LongPress)
                        }
                    }
                    onEnd(isArmed)
                }
            }
            .semantics { contentDescription = if (recorder.isRecording) recordingWords else idleWords },
        contentAlignment = Alignment.Center,
    ) {
        if (recorder.isRecording) {
            RecordingContent(recorder, recordingWords)
        } else {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Outlined.Mic, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(idleWords, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** What the hold area shows while recording: a red dot, the time or the countdown, live bars, and what letting go does. */
@Composable
private fun RecordingContent(recorder: VoiceMessageRecorder, hintWords: String) {
    val isStacked = LocalDensity.current.fontScale >= 1.5f
    val timeColor = if (recorder.isCountingDown) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface
    val timeWords = if (recorder.isCountingDown) {
        stringResource(R.string.chats_time_left, VoiceMessageRecorder.clockText(kotlin.math.ceil(recorder.secondsLeft)))
    } else {
        VoiceMessageRecorder.clockText(recorder.elapsedSeconds)
    }
    val hintColor = if (recorder.isCancelArmed) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant
    @Composable fun Timing() {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Box(Modifier.size(8.dp).clip(CircleShape).background(MaterialTheme.colorScheme.error))
            Text(timeWords, style = MaterialTheme.typography.labelLarge.copy(fontFamily = FontFamily.Monospace, fontWeight = FontWeight.SemiBold), color = timeColor)
            LevelBars(recorder.recentLevels, Modifier.width(48.dp).height(20.dp))
        }
    }
    if (isStacked) {
        Column(Modifier.fillMaxWidth().padding(vertical = 6.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Timing()
            Text(hintWords, style = MaterialTheme.typography.bodySmall, color = hintColor)
        }
    } else {
        Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Timing()
            Text(hintWords, Modifier.weight(1f), style = MaterialTheme.typography.bodySmall, color = hintColor, textAlign = androidx.compose.ui.text.style.TextAlign.End)
        }
    }
}

/** The live bars of the microphone's level: the most recent levels, the newest at the right. */
@Composable
private fun LevelBars(levels: List<Float>, modifier: Modifier = Modifier) {
    val color = MaterialTheme.colorScheme.primary
    androidx.compose.foundation.Canvas(modifier) {
        val count = VoiceMessageRecorder.LEVEL_BAR_COUNT
        val step = size.width / count
        val barWidth = maxOf(1.dp.toPx(), step * 0.55f)
        val missing = count - levels.size
        for (index in 0 until count) {
            val level = if (index < missing) 0f else levels[index - missing]
            val barHeight = maxOf(2.dp.toPx(), size.height * level)
            drawRoundRect(
                color = color, topLeft = androidx.compose.ui.geometry.Offset(index * step + (step - barWidth) / 2, (size.height - barHeight) / 2),
                size = androidx.compose.ui.geometry.Size(barWidth, barHeight), cornerRadius = androidx.compose.ui.geometry.CornerRadius(barWidth / 2),
            )
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
                    Modifier.fillMaxWidth().heightIn(min = 56.dp).clickable(role = Role.Button) { onChoose(choice) }.padding(horizontal = 16.dp, vertical = 8.dp),
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
