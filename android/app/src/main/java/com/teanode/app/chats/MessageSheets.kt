package com.teanode.app.chats

import android.content.Context
import android.content.Intent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Reply
import androidx.compose.material.icons.automirrored.outlined.VolumeUp
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material.icons.outlined.StopCircle
import androidx.compose.material.icons.outlined.TextFields
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.chats.EmojiReaction
import com.teanode.kit.chats.TranscriptLine

/** Shares text with another app. */
fun shareText(context: Context, text: String) {
    val intent = Intent(Intent.ACTION_SEND).apply {
        type = "text/plain"
        putExtra(Intent.EXTRA_TEXT, text)
    }
    context.startActivity(Intent.createChooser(intent, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
}

/**
 * MessageActionsSheet is a message held: the quick reactions with a way to choose any emoji, the message itself on its
 * own side, its whole text scrolling when it is long, and its actions below as full rows.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MessageActionsSheet(line: TranscriptLine, model: ConversationModel, reader: MessageReader, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    val isOwn = line.author?.isOwnWords ?: (line.kind == TranscriptLine.Kind.Person)
    var isChoosingEmoji by remember { mutableStateOf(false) }
    var emojiText by remember { mutableStateOf("") }
    val voiceMessageWords = stringResource(R.string.chats_voice_message)
    val shownText = if (line.isVoiceMessage) line.transcriptText.ifEmpty { voiceMessageWords } else line.text
    val hasWords = !line.isVoiceMessage || line.transcriptText.isNotEmpty()
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.navigationBarsPadding().padding(bottom = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            // Each side reacts to what the other said: the person to the agent's answers, not to their own messages. In a chat, to anybody's.
            if (line.messageId != null && (line.kind == TranscriptLine.Kind.Answer || line.author != null)) {
                if (isChoosingEmoji) {
                    Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedTextField(
                            value = emojiText, onValueChange = { typed ->
                                emojiText = typed
                                EmojiReaction.firstEmoji(typed)?.let { emoji ->
                                    onDismiss()
                                    model.react(line, emoji)
                                }
                            },
                            label = { Text(stringResource(R.string.chats_choose_any_emoji)) }, singleLine = true, modifier = Modifier.weight(1f),
                        )
                        IconButton(onClick = { isChoosingEmoji = false }) { Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.chats_cancel)) }
                    }
                } else {
                    val current = line.reaction("person", model.ownUserId)
                    val choices = ConversationModel.quickReactions.let { quick -> if (current != null && current !in quick) quick + current else quick }
                    LazyRow(Modifier.padding(horizontal = 12.dp), horizontalArrangement = Arrangement.spacedBy(2.dp), verticalAlignment = Alignment.CenterVertically) {
                        items(choices) { emoji ->
                            val label = if (current == emoji) stringResource(R.string.chats_take_away_reaction, emoji) else stringResource(R.string.chats_react_with, emoji)
                            Box(
                                Modifier.size(48.dp).clip(CircleShape).background(if (current == emoji) MaterialTheme.colorScheme.primary.copy(alpha = 0.3f) else androidx.compose.ui.graphics.Color.Transparent)
                                    .clickable {
                                        onDismiss()
                                        model.react(line, emoji)
                                    }.semantics { contentDescription = label },
                                contentAlignment = Alignment.Center,
                            ) { Text(emoji, style = MaterialTheme.typography.titleLarge) }
                        }
                        item {
                            IconButton(onClick = { isChoosingEmoji = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.chats_more_emoji)) }
                        }
                    }
                }
            }
            Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp).heightIn(max = 240.dp).verticalScrollBox(), contentAlignment = if (isOwn) Alignment.CenterEnd else Alignment.CenterStart) {
                Box(Modifier.clip(RoundedCornerShape(20.dp)).background(bubbleColor(!isOwn)).padding(horizontal = 14.dp, vertical = 10.dp)) {
                    if (line.kind == TranscriptLine.Kind.Answer) MarkdownView(line.text, conversationId = model.conversationId) else Text(shownText, style = MaterialTheme.typography.bodyLarge)
                }
            }
            HorizontalDivider()
            if (line.messageId != null) {
                ActionRow(Icons.AutoMirrored.Outlined.Reply, stringResource(R.string.chats_reply)) {
                    onDismiss()
                    model.reply(line, voiceMessageWords)
                }
            }
            if (model.canEdit(line)) {
                ActionRow(Icons.Outlined.Edit, stringResource(R.string.chats_edit)) {
                    onDismiss()
                    model.editingLine = line
                }
            }
            if (hasWords) {
                ActionRow(Icons.Outlined.ContentCopy, stringResource(R.string.chats_copy)) {
                    clipboard.setText(AnnotatedString(line.readableText))
                    onDismiss()
                }
                if (reader.isReading(line)) {
                    ActionRow(Icons.Outlined.StopCircle, stringResource(R.string.chats_stop_reading)) {
                        reader.stop()
                        onDismiss()
                    }
                } else {
                    ActionRow(Icons.AutoMirrored.Outlined.VolumeUp, stringResource(R.string.chats_read_aloud)) {
                        onDismiss()
                        reader.onProblem = { text -> model.voiceProblemText = text }
                        reader.read(line, model.connection)
                    }
                }
                ActionRow(Icons.Outlined.TextFields, stringResource(R.string.chats_select)) {
                    val text = line.readableText
                    onDismiss()
                    model.selectingText = text
                }
                ActionRow(Icons.Outlined.Share, stringResource(R.string.chats_share)) {
                    shareText(context, line.readableText)
                    onDismiss()
                }
            }
            if (model.canDelete(line)) {
                ActionRow(Icons.Outlined.DeleteOutline, stringResource(R.string.chats_delete), isDestructive = true) {
                    onDismiss()
                    model.deletingLine = line
                }
            }
        }
    }
}

@Composable
private fun Modifier.verticalScrollBox(): Modifier = this.verticalScroll(rememberScrollState())

@Composable
private fun ActionRow(icon: ImageVector, title: String, isDestructive: Boolean = false, onClick: () -> Unit) {
    val color = if (isDestructive) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface
    Row(
        Modifier.fillMaxWidth().heightIn(min = 52.dp).clickable(onClick = onClick).padding(horizontal = 20.dp), verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Icon(icon, contentDescription = null, tint = color)
        Text(title, color = color, style = MaterialTheme.typography.bodyLarge)
    }
}

/** A message opened to select part of its words, or the whole of one too long for its bubble. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SelectTextSheet(text: String, title: String? = null, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.navigationBarsPadding().padding(horizontal = 20.dp).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (title != null) Text(title, style = MaterialTheme.typography.titleMedium)
            SelectionContainer {
                Column(Modifier.heightIn(max = 520.dp).verticalScroll(rememberScrollState())) { Text(text, style = MaterialTheme.typography.bodyLarge) }
            }
        }
    }
}

/** A message of the person's own opened to change what it says. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EditMessageSheet(line: TranscriptLine, model: ConversationModel, onDismiss: () -> Unit) {
    var text by remember(line.id) { mutableStateOf(line.text) }
    val canSave = text.trim().isNotEmpty() && text.trim() != line.text
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.navigationBarsPadding().padding(horizontal = 20.dp).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.chats_edit_message), style = MaterialTheme.typography.titleMedium)
            OutlinedTextField(value = text, onValueChange = { text = it }, modifier = Modifier.fillMaxWidth().heightIn(min = 120.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.chats_cancel)) }
                TextButton(
                    onClick = {
                        val words = text
                        onDismiss()
                        model.edit(line, words)
                    },
                    enabled = canSave,
                ) { Text(stringResource(R.string.chats_save)) }
            }
        }
    }
}
