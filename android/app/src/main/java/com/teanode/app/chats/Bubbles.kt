package com.teanode.app.chats

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CreditCard
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.Mail
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material.icons.outlined.Pause
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.kit.chats.AgentAttachment
import com.teanode.kit.chats.AgentMessageReaction
import com.teanode.kit.chats.ChatMentionWord
import com.teanode.kit.chats.TranscriptLine
import kotlinx.coroutines.launch
import java.util.Base64

/** Whose side a bubble is on: the person's own words are on the right in the accent, everybody else's on the left in gray. */
@Composable
fun bubbleColor(isOthers: Boolean): Color =
    if (isOthers) MaterialTheme.colorScheme.surfaceContainerHigh else MaterialTheme.colorScheme.primaryContainer

/** DeletedMessageBubble is what stands where a message of a chat was taken back, whether a person or an agent said it. */
@Composable
fun DeletedMessageBubble() {
    Text(
        stringResource(R.string.chats_message_deleted), fontStyle = FontStyle.Italic, color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.border(BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant), RoundedCornerShape(20.dp)).padding(horizontal = 14.dp, vertical = 10.dp),
    )
}

/** ReactionBadges are the emoji on a message, the person's and the agent's, in a small capsule at the bubble's corner as Messages draws a tapback. */
@Composable
fun ReactionBadges(reactions: List<AgentMessageReaction>, modifier: Modifier = Modifier) {
    if (reactions.isEmpty()) return
    val agentWords = stringResource(R.string.chats_agent_reacted)
    val youWords = stringResource(R.string.chats_you_reacted)
    Row(
        modifier.clip(CircleShape).background(MaterialTheme.colorScheme.surfaceContainerHighest).border(0.5.dp, MaterialTheme.colorScheme.outlineVariant, CircleShape)
            .padding(horizontal = 6.dp, vertical = 2.dp),
        horizontalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        for (reaction in reactions) {
            val label = when {
                reaction.reactorKind == "agent" -> "$agentWords ${reaction.emoji}"
                !reaction.reactorName.isNullOrEmpty() -> "${reaction.reactorName}: ${reaction.emoji}"
                else -> "$youWords ${reaction.emoji}"
            }
            Text(reaction.emoji, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.semantics { contentDescription = label })
        }
    }
}

/** A bubble with its reactions at its lower corner, and the room under it they need. */
@Composable
private fun BubbleWithReactions(isOthers: Boolean, reactions: List<AgentMessageReaction>, content: @Composable () -> Unit) {
    Box(Modifier.padding(bottom = if (reactions.isEmpty()) 0.dp else 14.dp)) {
        content()
        ReactionBadges(reactions, Modifier.align(if (isOthers) Alignment.BottomEnd else Alignment.BottomStart).offset(x = if (isOthers) 10.dp else (-10).dp, y = 14.dp))
    }
}

/**
 * AgentBubble is something from the agent, in a bubble on the left as a person's reply sits in Messages: the
 * conversation reads as talking to someone, with a bubble on each side.
 */
@Composable
fun AgentBubble(modifier: Modifier = Modifier, reactions: List<AgentMessageReaction> = emptyList(), content: @Composable () -> Unit) {
    Row(modifier.fillMaxWidth()) {
        BubbleWithReactions(true, reactions) {
            Box(Modifier.clip(RoundedCornerShape(20.dp)).background(bubbleColor(true)).padding(horizontal = 14.dp, vertical = 10.dp)) { content() }
        }
        Spacer(Modifier.width(40.dp))
    }
}

/**
 * PersonBubble is what the person said: on the right, in the accent color, with what it replies to above it. Another
 * person's words in a chat are on the left, in the gray the agent's are in.
 */
@Composable
fun PersonBubble(
    line: TranscriptLine,
    modifier: Modifier = Modifier,
    mentions: List<ChatMentionWord> = emptyList(),
    onTranscribe: (suspend () -> Unit)? = null,
    onShowAll: () -> Unit = {},
) {
    val isOthers = line.author?.let { author -> !author.isOwnWords } ?: false
    val style = rememberInlineStyle(isOnAccent = !isOthers)
    Row(modifier.fillMaxWidth(), horizontalArrangement = if (isOthers) Arrangement.Start else Arrangement.End) {
        if (!isOthers) Spacer(Modifier.width(56.dp))
        Column(Modifier.weight(1f, fill = false), horizontalAlignment = if (isOthers) Alignment.Start else Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (line.isDeleted) {
                DeletedMessageBubble()
            } else {
                Column(
                    Modifier.alpha(if (line.author != null && line.isBeingSent) 0.6f else 1f),
                    horizontalAlignment = if (isOthers) Alignment.Start else Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp),
                ) {
                    if (line.quotedText.isNotEmpty()) {
                        Row(Modifier.padding(horizontal = 10.dp, vertical = 6.dp).height(IntrinsicSize.Min)) {
                            Box(Modifier.width(3.dp).fillMaxHeight().clip(CircleShape).background(MaterialTheme.colorScheme.outlineVariant))
                            Text(
                                line.quotedText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3,
                                overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 8.dp),
                            )
                        }
                    }
                    for (reference in line.references) {
                        val label = reference.label.ifEmpty { if (reference.photoId != null) stringResource(R.string.chats_photo) else "" }
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                            Icon(referenceIcon(reference), contentDescription = null, modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                            Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    val recording = line.voiceAttachment
                    if (recording != null && recording.isRemoved != true) {
                        VoiceMessageBubble(line, recording, isOthers, onTranscribe)
                    } else {
                        for (attachment in line.attachments) AttachmentView(attachment)
                    }
                    if (line.text.isNotEmpty()) {
                        val isTooLong = line.text.length > SHOWN_CHARACTER_COUNT || line.text.count { character -> character == '\n' } + 1 > SHOWN_LINE_COUNT
                        BubbleWithReactions(isOthers, line.reactions) {
                            Column(
                                Modifier.clip(RoundedCornerShape(20.dp)).background(bubbleColor(isOthers)).padding(horizontal = 14.dp, vertical = 10.dp),
                                verticalArrangement = Arrangement.spacedBy(8.dp),
                            ) {
                                if (isTooLong) {
                                    Text(line.text.take(SHOWN_CHARACTER_COUNT), maxLines = SHOWN_LINE_COUNT, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyLarge)
                                    OutlinedButton(onClick = onShowAll) { Text(stringResource(R.string.chats_show_all)) }
                                } else {
                                    val text = remember(line.text, mentions, style.linkColor) { inlineText(line.text, style, mentions, isPlain = true) }
                                    Text(
                                        text = text,
                                        style = MaterialTheme.typography.bodyLarge.copy(color = if (isOthers) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onPrimaryContainer),
                                    )
                                }
                            }
                        }
                    }
                    if (line.editedAt != null) {
                        Text(stringResource(R.string.chats_edited), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.outline, modifier = Modifier.padding(horizontal = 6.dp))
                    }
                }
            }
        }
        if (isOthers) Spacer(Modifier.width(40.dp))
    }
}

private const val SHOWN_LINE_COUNT = 30
private const val SHOWN_CHARACTER_COUNT = 3000

private fun referenceIcon(reference: com.teanode.kit.chats.AgentReference): androidx.compose.ui.graphics.vector.ImageVector = when {
    reference.recordingId != null -> Icons.Outlined.GraphicEq
    reference.photoId != null -> Icons.Outlined.Image
    reference.merchantName != null -> Icons.Outlined.CreditCard
    reference.path != null -> Icons.Outlined.Psychology
    else -> Icons.Outlined.Mail
}

/** CardSurface is something that waits for the person, or a file: one quiet surface with a hairline edge, the same for every card. */
@Composable
fun CardSurface(modifier: Modifier = Modifier, paddingDp: Int = 16, content: @Composable () -> Unit) {
    Surface(
        modifier = modifier.fillMaxWidth(), shape = RoundedCornerShape(18.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh,
        border = BorderStroke(0.5.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Box(Modifier.padding(paddingDp.dp)) { content() }
    }
}

/**
 * TypingBubble says the agent is working on an answer: three dots rising in turn inside a bubble, as a person typing
 * shows in Messages, so the wait reads as someone answering.
 */
@Composable
fun TypingBubble(modifier: Modifier = Modifier) {
    val transition = rememberInfiniteTransition(label = "typing")
    val beat by transition.animateFloat(0f, 1f, infiniteRepeatable(tween(1200, easing = LinearEasing), RepeatMode.Restart), label = "beat")
    val thinking = stringResource(R.string.chats_thinking)
    Row(modifier.fillMaxWidth().semantics { contentDescription = thinking }) {
        Row(
            Modifier.clip(RoundedCornerShape(20.dp)).background(bubbleColor(true)).padding(horizontal = 16.dp, vertical = 14.dp),
            horizontalArrangement = Arrangement.spacedBy(5.dp),
        ) {
            for (index in 0 until 3) {
                // Each dot peaks a third of a beat after the one before it.
                val phase = maxOf(0f, kotlin.math.sin(((beat - index / 6f) * 2 * Math.PI).toFloat()))
                Box(
                    Modifier.size(8.dp).offset(y = (-3 * phase).dp).alpha(0.4f + 0.6f * phase).clip(CircleShape).background(MaterialTheme.colorScheme.onSurfaceVariant),
                )
            }
        }
    }
}

/** The waveform of a voice message: its 48 levels as bars, the played part in the accent. */
@Composable
fun VoiceWaveform(levels: List<Int>, progress: Float, modifier: Modifier = Modifier) {
    val played = MaterialTheme.colorScheme.primary
    val rest = MaterialTheme.colorScheme.outline
    Canvas(modifier) {
        if (levels.isEmpty()) return@Canvas
        val step = size.width / levels.size
        val barWidth = maxOf(2.dp.toPx(), minOf(4.dp.toPx(), step * 0.6f))
        for ((index, level) in levels.withIndex()) {
            val barHeight = maxOf(3.dp.toPx(), size.height * level / 255f)
            val isPlayed = index.toFloat() / levels.size < progress
            drawRoundRect(
                color = if (isPlayed) played else rest, topLeft = Offset(index * step + (step - barWidth) / 2, (size.height - barHeight) / 2),
                size = Size(barWidth, barHeight), cornerRadius = CornerRadius(barWidth / 2),
            )
        }
    }
}

/** The 48 levels a voice message carries as base64, or a flat run when it carries none. */
fun decodedWaveformLevels(waveform: String?): List<Int> {
    if (!waveform.isNullOrEmpty()) {
        val bytes = runCatching { Base64.getDecoder().decode(waveform) }.getOrNull()
        if (bytes != null && bytes.isNotEmpty()) return bytes.map { byte -> byte.toInt() and 0xFF }
    }
    return List(48) { 60 }
}

/**
 * VoiceMessageBubble is a voice message in a chat: a button to play it, its waveform filling as it plays, the time
 * left, and its words under it once they are made.
 */
@Composable
fun VoiceMessageBubble(line: TranscriptLine, recording: AgentAttachment, isOthers: Boolean, onTranscribe: (suspend () -> Unit)?) {
    val connection = LocalConnection.current
    val scope = rememberCoroutineScope()
    var isTranscribing by remember { mutableStateOf(false) }
    val isLoaded = VoiceMessagePlayer.loadedAttachmentId == recording.id
    val isPlayingThis = isLoaded && VoiceMessagePlayer.isPlaying
    val isLoadingThis = VoiceMessagePlayer.loadingAttachmentId == recording.id
    val durationSeconds = (recording.audioDurationMS ?: 0) / 1000.0
    val progress = if (isLoaded) VoiceMessagePlayer.progress else 0f
    val timeText = if (isLoaded && (VoiceMessagePlayer.isPlaying || VoiceMessagePlayer.playedSeconds > 0)) {
        VoiceMessageRecorder.clockText(kotlin.math.ceil(maxOf(0.0, durationSeconds - VoiceMessagePlayer.playedSeconds)))
    } else {
        VoiceMessageRecorder.clockText(kotlin.math.ceil(durationSeconds))
    }
    val transcriptingWords = when {
        line.transcriptState == "waiting" || isTranscribing -> stringResource(R.string.chats_transcribing)
        line.transcriptState == "failed" -> stringResource(R.string.chats_could_not_transcribe)
        line.transcriptState == "done" && line.transcriptText.isNotEmpty() -> line.transcriptText
        else -> null
    }
    val canTranscribe = line.transcriptState in listOf("off", "failed", "") && line.messageId != null
    val playLabel = stringResource(if (isPlayingThis) R.string.chats_pause_voice_message else R.string.chats_play_voice_message)
    BubbleWithReactions(isOthers, line.reactions) {
        Column(
            Modifier.clip(RoundedCornerShape(20.dp)).background(bubbleColor(isOthers)).padding(start = 6.dp, end = 14.dp, top = 4.dp, bottom = 4.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                Box(
                    Modifier.size(48.dp).clip(CircleShape).semantics { contentDescription = playLabel }.clickable { VoiceMessagePlayer.toggle(recording, connection) },
                    contentAlignment = Alignment.Center,
                ) {
                    Box(Modifier.size(36.dp).clip(CircleShape).background(MaterialTheme.colorScheme.primary), contentAlignment = Alignment.Center) {
                        if (isLoadingThis) {
                            CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary)
                        } else {
                            Icon(if (isPlayingThis) Icons.Outlined.Pause else Icons.Outlined.PlayArrow, contentDescription = null, tint = MaterialTheme.colorScheme.onPrimary)
                        }
                    }
                }
                VoiceWaveform(decodedWaveformLevels(recording.audioWaveform), progress, Modifier.width(150.dp).height(28.dp))
                Text(timeText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (transcriptingWords != null) {
                Text(transcriptingWords, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.widthIn(max = 260.dp))
            }
            if (onTranscribe != null && canTranscribe && !isTranscribing) {
                FilledTonalButton(
                    onClick = {
                        isTranscribing = true
                        scope.launch {
                            onTranscribe()
                            isTranscribing = false
                        }
                    },
                ) { Text(stringResource(R.string.chats_transcribe)) }
            }
            if (VoiceMessagePlayer.failedAttachmentId == recording.id) {
                Text(stringResource(R.string.chats_could_not_play_voice_message), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            }
        }
    }
}
