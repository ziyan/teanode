package com.teanode.app.chats

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.automirrored.outlined.Reply
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Cached
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.CreditCard
import androidx.compose.material.icons.outlined.DateRange
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Flag
import androidx.compose.material.icons.outlined.GraphicEq
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.Mail
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.AssistChip
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.AvatarView
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.AgentConversation
import com.teanode.kit.chats.AgentReference
import com.teanode.kit.chats.AgentSpeaker
import com.teanode.kit.chats.AgentTodo
import com.teanode.kit.chats.AgentUsage
import com.teanode.kit.chats.ChatAuthor
import com.teanode.kit.chats.ChatMember
import com.teanode.kit.chats.CheckInOrigin
import com.teanode.kit.chats.TranscriptLine
import kotlinx.serialization.Serializable
import java.text.NumberFormat
import java.util.Currency
import java.util.Locale

/** A tool's name as words: "mail_draft" is "Mail draft", and a skill's or a server's tool is named by its own last part. */
fun toolWords(tool: String): String {
    val last = tool.split("__").last()
    val words = last.replace("_", " ").replace("-", " ")
    return words.take(1).uppercase() + words.drop(1)
}

/** What the agent's usage is called: a count of tokens and the money it cost. */
object UsageWords {
    fun count(count: Int): String = when {
        count >= 1_000_000 -> "%.1fM".format(count / 1_000_000.0).replace(".0M", "M")
        count >= 1_000 -> "%.1fK".format(count / 1_000.0).replace(".0K", "K")
        else -> count.toString()
    }

    fun money(cost: Double, currencyCode: String): String {
        val format = NumberFormat.getCurrencyInstance(Locale.getDefault())
        runCatching { format.currency = Currency.getInstance(currencyCode) }
        format.maximumFractionDigits = if (cost < 0.01) 4 else 2
        format.minimumFractionDigits = if (cost < 0.01) 4 else 2
        return format.format(cost)
    }

    @Composable
    fun turn(usage: AgentUsage, currencyCode: String): String =
        stringResource(R.string.chats_usage_turn, count(usage.promptTokens), count(usage.completionTokens), money(usage.cost, currencyCode))
}

/** ToolLine is a tool the agent called: its name and how it ended on one line, what it was given and what came back on a tap. */
@Composable
fun ToolLine(line: TranscriptLine) {
    var isExpanded by rememberSaveable(line.id) { mutableStateOf(false) }
    Column(Modifier.padding(start = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(
            Modifier.clickable { isExpanded = !isExpanded }.heightIn(min = 32.dp), verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            if (line.isDone) {
                Icon(Icons.Outlined.CheckCircle, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            } else {
                CircularProgressIndicator(Modifier.size(14.dp), strokeWidth = 2.dp)
            }
            Text(
                if (line.resolution.isEmpty()) toolWords(line.detail) else "${toolWords(line.detail)} · ${line.resolution}", maxLines = 1,
                overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.weight(1f, fill = false),
            )
            Icon(if (isExpanded) Icons.Outlined.ExpandLess else Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (isExpanded) {
            if (line.arguments.isNotEmpty()) CodeBlock(tidyToolText(line.arguments), maxHeightDp = 240)
            CodeBlock(if (line.text.isEmpty()) stringResource(R.string.chats_nothing_came_back) else tidyToolText(line.text), maxHeightDp = 240)
        }
    }
}

/** CheckInLine is a turn the agent took on its own, shown as where it came from; a tap shows what started it. */
@Composable
fun CheckInLine(line: TranscriptLine) {
    var isExpanded by rememberSaveable(line.id) { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(Modifier.clickable { isExpanded = !isExpanded }.heightIn(min = 32.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Icon(checkInIcon(line.detail), contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(checkInLabel(line.detail), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (isExpanded) CodeBlock(line.text, maxHeightDp = 240)
    }
}

@Composable
private fun checkInLabel(marker: String): String = when (CheckInOrigin.entries.firstOrNull { origin -> origin.marker == marker }) {
    CheckInOrigin.Goal -> stringResource(R.string.chats_check_in_goal)
    CheckInOrigin.GoalNeedsYou -> stringResource(R.string.chats_check_in_goal_needs_you)
    CheckInOrigin.GoalDone -> stringResource(R.string.chats_check_in_goal_done)
    CheckInOrigin.Background -> stringResource(R.string.chats_check_in_background)
    CheckInOrigin.BackgroundWork -> stringResource(R.string.chats_check_in_background_work)
    CheckInOrigin.Schedule -> stringResource(R.string.chats_check_in_schedule)
    CheckInOrigin.SpeakFirst -> stringResource(R.string.chats_check_in_speak_first)
    CheckInOrigin.Alert -> stringResource(R.string.chats_check_in_alert)
    CheckInOrigin.FileSent -> stringResource(R.string.chats_check_in_file_sent)
    CheckInOrigin.HerdrQuestion -> stringResource(R.string.chats_check_in_herdr_question)
    CheckInOrigin.HerdrSession -> stringResource(R.string.chats_check_in_herdr_session)
    CheckInOrigin.RestartResume -> stringResource(R.string.chats_check_in_restart_resume)
    CheckInOrigin.Approved -> stringResource(R.string.chats_check_in_approved)
    CheckInOrigin.Declined -> stringResource(R.string.chats_check_in_declined)
    null -> marker
}

private fun checkInIcon(marker: String): ImageVector = when (CheckInOrigin.entries.firstOrNull { origin -> origin.marker == marker }) {
    CheckInOrigin.Goal, CheckInOrigin.GoalNeedsYou, CheckInOrigin.GoalDone -> Icons.Outlined.Flag
    CheckInOrigin.Background, CheckInOrigin.HerdrSession -> Icons.Outlined.Terminal
    CheckInOrigin.HerdrQuestion, CheckInOrigin.Alert -> Icons.Outlined.Warning
    CheckInOrigin.BackgroundWork -> Icons.Outlined.Cached
    CheckInOrigin.Schedule -> Icons.Outlined.DateRange
    CheckInOrigin.SpeakFirst -> Icons.Outlined.AutoAwesome
    CheckInOrigin.FileSent -> Icons.Outlined.AttachFile
    CheckInOrigin.RestartResume -> Icons.Outlined.Cached
    CheckInOrigin.Approved -> Icons.Outlined.Check
    CheckInOrigin.Declined -> Icons.Outlined.Close
    null -> Icons.Outlined.Notifications
}

@Serializable
private class CircleInvitationNote(val invitedByName: String = "", val circleName: String = "")

@Serializable
private class CircleMemberAddedNote(val addedName: String = "", val circleName: String = "")

@Serializable
private class RelationshipNote(val personName: String = "", val relationshipKind: String? = null, val termGender: String? = null, val relationshipWord: String? = null)

@Serializable
private class ChatAgentNote(val agentName: String = "", val ownerName: String = "")

private inline fun <reified Note> readNote(detail: String, serializer: kotlinx.serialization.KSerializer<Note>): Note? = try {
    TeaNodeJson.decodeFromString(serializer, detail)
} catch (_: Exception) {
    null
}

/** NoteLine is something that happened, in the dashboard's words; the note the earlier chat was folded into opens on a tap. */
@Composable
fun NoteLine(line: TranscriptLine) {
    var isExpanded by rememberSaveable(line.id) { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (line.detail == "compacted" && line.text.isNotEmpty()) {
            Row(Modifier.clickable { isExpanded = !isExpanded }.heightIn(min = 32.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Icon(if (isExpanded) Icons.Outlined.ExpandMore else Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(noteWords(line), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (isExpanded) AgentBubble { MarkdownView(line.text) }
        } else {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (line.isPending) CircularProgressIndicator(Modifier.size(12.dp), strokeWidth = 2.dp)
                Text(noteWords(line), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** The words of a note, in the dashboard's words. */
@Composable
fun noteWords(line: TranscriptLine): String {
    val detail = line.text.trim()
    return when (line.detail) {
        "queued" -> stringResource(R.string.chats_note_queued)
        "stopped" -> if (detail.isEmpty()) stringResource(R.string.chats_note_stopped) else stringResource(R.string.chats_note_stopped_with, detail)
        "failed" -> if (detail.isEmpty()) stringResource(R.string.chats_note_failed) else stringResource(R.string.chats_note_failed_with, detail)
        "call_unreadable" -> stringResource(R.string.chats_note_call_unreadable)
        "repeated_failure" -> stringResource(R.string.chats_note_repeated_failure)
        "round_limit" -> stringResource(R.string.chats_note_round_limit)
        "compacting" -> stringResource(R.string.chats_note_compacting)
        "compacted" -> stringResource(R.string.chats_note_compacted)
        "depth" -> if (detail.isEmpty()) stringResource(R.string.chats_note_depth) else stringResource(R.string.chats_note_depth_with, detail)
        "goal_set" -> stringResource(R.string.chats_note_goal_set, detail)
        "goal_set_again" -> stringResource(R.string.chats_note_goal_set_again, detail)
        "goal_changed" -> stringResource(R.string.chats_note_goal_changed, detail)
        "goal_cleared" -> stringResource(R.string.chats_note_goal_cleared, detail)
        "goal_met" -> stringResource(R.string.chats_note_goal_met, detail)
        "goal_stalled" -> stringResource(R.string.chats_note_goal_stalled, detail)
        // What a person is told when they addressed an agent in a chat and no turn of it started. The detail is the agent's and its owner's names as JSON.
        "chat_agent_owner_only" -> {
            val note = readNote(detail, ChatAgentNote.serializer())
            if (note != null && note.ownerName.isNotEmpty()) {
                val agentName = note.agentName.trim()
                if (agentName.isEmpty() || agentName == AgentSpeaker.DEFAULT_NAME) stringResource(R.string.chats_note_owner_only, note.ownerName, note.ownerName)
                else stringResource(R.string.chats_note_owner_only_named, note.ownerName, agentName, note.ownerName)
            } else if (detail.isEmpty()) {
                stringResource(R.string.chats_note_owner_only_unknown)
            } else {
                stringResource(R.string.chats_note_owner_only_of, detail)
            }
        }
        "chat_agent_off" -> if (detail.isEmpty()) stringResource(R.string.chats_note_agent_off) else stringResource(R.string.chats_note_agent_off_named, detail)
        "chat_agent_absent" -> if (detail.isEmpty()) stringResource(R.string.chats_note_agent_absent) else stringResource(R.string.chats_note_agent_absent_named, detail)
        "circle_invited" -> readNote(detail, CircleInvitationNote.serializer())?.takeIf { note -> note.circleName.isNotEmpty() }
            ?.let { note -> stringResource(R.string.chats_note_circle_invited, note.invitedByName, note.circleName) } ?: detail
        "circle_member_added" -> readNote(detail, CircleMemberAddedNote.serializer())?.takeIf { note -> note.circleName.isNotEmpty() }
            ?.let { note -> stringResource(R.string.chats_note_circle_member_added, note.addedName, note.circleName) } ?: detail
        "relationship_set" -> readNote(detail, RelationshipNote.serializer())?.takeIf { note -> note.personName.isNotEmpty() }
            ?.let { note -> stringResource(R.string.chats_note_relationship_set, note.personName, note.personName, note.relationshipWord.orEmpty()) } ?: detail
        "relationship_removed" -> readNote(detail, RelationshipNote.serializer())?.takeIf { note -> note.personName.isNotEmpty() }
            ?.let { note -> stringResource(R.string.chats_note_relationship_removed, note.personName) } ?: detail
        else -> detail
    }
}

/** Where a goal stands, in the dashboard's words. */
@Composable
fun goalStateWords(goalState: String?): String = when (goalState) {
    "working" -> stringResource(R.string.chats_goal_working)
    "waiting" -> stringResource(R.string.chats_goal_waiting)
    "met" -> stringResource(R.string.chats_goal_met)
    "dropped" -> stringResource(R.string.chats_goal_dropped)
    else -> goalState.orEmpty()
}

/** The goal's first sentence, for the one line at the top. */
fun goalFirstSentence(goal: String): String {
    val flat = goal.lineSequence().firstOrNull() ?: goal
    val end = flat.indexOf(". ")
    return if (end >= 0) flat.substring(0, end) else flat
}

/** One line of what kind of conversation this is, when it is not an ordinary one. */
@Composable
fun Banner(icon: ImageVector, text: String, modifier: Modifier = Modifier, onClick: (() -> Unit)? = null) {
    Surface(
        modifier = modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier), shape = RoundedCornerShape(14.dp),
        color = MaterialTheme.colorScheme.surfaceContainerHighest,
    ) {
        Row(Modifier.padding(horizontal = 12.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(icon, contentDescription = null, modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** GoalHeader is a goal's conversation's one line at the top: where the goal stands and the start of what it is. */
@Composable
fun GoalHeader(conversation: AgentConversation, goal: String, onOpen: () -> Unit) {
    val state = goalStateWords(conversation.goalState)
    Surface(
        modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp).clickable(onClick = onOpen), shape = CircleShape, color = MaterialTheme.colorScheme.surfaceContainerHighest,
    ) {
        Row(Modifier.padding(horizontal = 14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(Icons.Outlined.Flag, contentDescription = null, modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(
                state, style = MaterialTheme.typography.labelLarge,
                color = if (conversation.goalState == "waiting") MaterialTheme.colorScheme.tertiary else MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(goalFirstSentence(goal), maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.outline)
        }
    }
}

/** The people in a chat: the other person's picture for a direct chat, two overlapped for a group. */
@Composable
fun ChatAvatars(chat: com.teanode.kit.chats.ChatSummary, ownUserId: String?, sizeDp: Int = 30) {
    val others = chat.otherMembers(ownUserId)
    val shown = others.take(if (chat.isGroup) 2 else 1)
    Row(horizontalArrangement = Arrangement.spacedBy((-sizeDp * 0.3f).dp)) {
        if (shown.isEmpty()) AvatarView(chat.title ?: "?", null, null, size = sizeDp.dp)
        for (member in shown) {
            Box(Modifier.clip(CircleShape).background(MaterialTheme.colorScheme.surface).padding(2.dp)) {
                AvatarView(member.displayName, member.avatarUrl, member.avatarColorIndex, size = (sizeDp - 4).dp)
            }
        }
    }
}

/** How many messages wait in a chat. */
@Composable
fun UnreadBadge(unreadCount: Int) {
    val label = stringResource(R.string.chats_unread_count, unreadCount)
    Box(
        Modifier.clip(CircleShape).background(MaterialTheme.colorScheme.primary).padding(horizontal = 7.dp, vertical = 2.dp)
            .semantics { contentDescription = label },
    ) {
        Text(if (unreadCount > 99) "99+" else unreadCount.toString(), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onPrimary)
    }
}

/** The small pictures under a message of the people who have read as far as it. */
@Composable
fun ReadMarks(readers: List<ChatMember>, words: ChatWords) {
    if (readers.isEmpty()) return
    val names = readers.joinToString(", ") { member -> com.teanode.kit.chats.ChatSummary.nameOf(member, words.deletedAccountNote) }
    val label = stringResource(R.string.chats_read_by, names)
    Row(Modifier.semantics { contentDescription = label }, verticalAlignment = Alignment.CenterVertically) {
        for (member in readers.take(MOST_READ_AVATARS)) {
            Box(Modifier.padding(end = 2.dp)) { AvatarView(member.displayName, member.avatarUrl, member.avatarColorIndex, size = 16.dp) }
        }
        if (readers.size > MOST_READ_AVATARS) Text("+${readers.size - MOST_READ_AVATARS}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

private const val MOST_READ_AVATARS = 4

/** Says who is writing, and which agent is thinking. */
@Composable
fun ChatTypingLine(names: List<String>, agentNames: List<String>, words: ChatWords) {
    Column(Modifier.fillMaxWidth().padding(start = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (names.isNotEmpty()) TypingWords(words.typing(names))
        if (agentNames.isNotEmpty()) TypingWords(words.thinking(agentNames))
    }
}

@Composable
private fun TypingWords(text: String) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        Text("…", color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
        Text(text, color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodySmall)
    }
}

/**
 * ChatLineFrame puts a message of a chat where it belongs: the person's own on the right, and everybody else's on the
 * left with their picture and, above the first message of a run, their name. The bubble is the content.
 */
@Composable
fun ChatLineFrame(
    author: ChatAuthor,
    isRunStart: Boolean,
    readers: List<ChatMember>,
    words: ChatWords,
    onOpenPerson: (String) -> Unit,
    content: @Composable () -> Unit,
) {
    val cardUserId = if (author.isPerson && !author.isMine && author.authorUserId.isNotEmpty()) author.authorUserId else null
    val name = if (author.isDeletedAccount) "${author.authorName} ${words.deletedAccountNote}" else author.speakerName(words::agentOf)
    if (author.isOwnWords) {
        Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(4.dp)) {
            content()
            Box(Modifier.padding(end = 6.dp)) { ReadMarks(readers, words) }
        }
    } else {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.Top) {
            if (isRunStart) {
                Box(Modifier.then(if (cardUserId != null) Modifier.clickable { onOpenPerson(cardUserId) } else Modifier)) {
                    AvatarView(name, author.authorAvatarUrl, author.authorAvatarColorIndex, size = 30.dp)
                }
            } else {
                Spacer(Modifier.width(30.dp))
            }
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                if (isRunStart) {
                    Row(
                        Modifier.padding(start = 6.dp).then(if (cardUserId != null) Modifier.clickable { onOpenPerson(cardUserId) } else Modifier),
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        Text(name, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (author.isAgent) Icon(Icons.Outlined.AutoAwesome, contentDescription = stringResource(R.string.chats_agent), modifier = Modifier.size(12.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                content()
                Box(Modifier.padding(start = 6.dp)) { ReadMarks(readers, words) }
            }
        }
    }
}

/** The replies the last answer offers, each sent as it is with a tap. */
@Composable
fun SuggestionsRow(suggestions: List<String>, onChoose: (String) -> Unit) {
    LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        items(suggestions) { suggestion ->
            AssistChip(onClick = { onChoose(suggestion) }, label = { Text(suggestion) })
        }
    }
}

/** What a conversation with nothing in it offers: what to ask, and a few of the agent's ideas. */
@Composable
fun EmptyConversation(model: ConversationModel, onOpenConversation: (String) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = 40.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Text(stringResource(R.string.chats_empty_prompt), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (model.ideas.isNotEmpty() && model.draft.isEmpty()) {
            Text(stringResource(R.string.chats_or_start_with), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.outline, modifier = Modifier.padding(top = 6.dp))
            for (idea in model.ideas) {
                Surface(
                    Modifier.fillMaxWidth().clickable { model.start(idea, onOpenConversation) }, shape = RoundedCornerShape(18.dp),
                    color = MaterialTheme.colorScheme.surfaceContainerHigh,
                ) {
                    Row(Modifier.padding(14.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.Top) {
                        Text(idea.emoji ?: "💡", style = MaterialTheme.typography.titleLarge)
                        Column(verticalArrangement = Arrangement.spacedBy(4.dp), modifier = Modifier.weight(1f)) {
                            Text(idea.headline, style = MaterialTheme.typography.titleSmall)
                            if (!idea.body.isNullOrEmpty()) Text(idea.body!!, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
            }
        }
    }
}

/** The agent's own list of steps for this conversation, while it works through them. */
@Composable
fun TodoPanel(todos: List<AgentTodo>) {
    Surface(Modifier.fillMaxWidth().padding(horizontal = 16.dp), shape = RoundedCornerShape(18.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh, border = BorderStroke(0.5.dp, MaterialTheme.colorScheme.outlineVariant)) {
        Column(
            Modifier.heightIn(max = 160.dp).verticalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(stringResource(R.string.chats_agent_steps), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.outline)
            for (todo in todos) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.Top) {
                    Icon(
                        if (todo.isDone) Icons.Outlined.CheckCircle else Icons.Outlined.RadioButtonUnchecked,
                        contentDescription = stringResource(if (todo.isDone) R.string.chats_done else R.string.chats_still_to_do),
                        modifier = Modifier.size(18.dp), tint = if (todo.isDone) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Text(
                        todo.text, style = MaterialTheme.typography.bodySmall, textDecoration = if (todo.isDone) TextDecoration.LineThrough else null,
                        color = if (todo.isDone) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                    )
                }
            }
        }
    }
}

/** What the next message refers to, above the composer: a small symbol, a line and a line under it, and a quiet way to drop it. */
@Composable
fun ReferenceStrip(icon: ImageVector, title: String, detail: String, onCancel: () -> Unit) {
    Surface(Modifier.fillMaxWidth().padding(horizontal = 16.dp), shape = RoundedCornerShape(20.dp), color = MaterialTheme.colorScheme.surfaceContainerHighest) {
        Row(Modifier.padding(start = 14.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(icon, contentDescription = null, modifier = Modifier.size(20.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Column(Modifier.weight(1f).padding(vertical = 6.dp)) {
                Text(title, style = MaterialTheme.typography.labelLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (detail.isNotEmpty()) Text(detail, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            IconButton(onClick = onCancel) { Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.chats_cancel), modifier = Modifier.size(18.dp)) }
        }
    }
}

/** The strip for each kind of reference the next message holds. */
@Composable
fun ReferenceBar(reference: AgentReference, model: ConversationModel, agentTitle: String) {
    val drop = { model.dropReference(reference) }
    when {
        reference.subject != null -> ReferenceStrip(Icons.Outlined.Mail, reference.subject!!.ifEmpty { stringResource(R.string.chats_no_subject) }, reference.from.orEmpty(), drop)
        reference.path != null -> ReferenceStrip(Icons.Outlined.Psychology, reference.name ?: reference.path!!, reference.path!!, drop)
        reference.recordingId != null -> ReferenceStrip(Icons.Outlined.GraphicEq, reference.label, "", drop)
        reference.photoId != null -> ReferenceStrip(Icons.Outlined.Image, reference.label.ifEmpty { stringResource(R.string.chats_photo) }, "", drop)
        reference.financeTransactionId != null -> ReferenceStrip(
            Icons.Outlined.CreditCard, reference.merchantName?.takeIf { name -> name.isNotEmpty() } ?: reference.description.orEmpty(),
            listOfNotNull(reference.amount?.let { amount -> "$amount ${reference.currencyCode.orEmpty()}".trim() }, reference.postedOn).filter { part -> part.isNotEmpty() }.joinToString(" · "),
            drop,
        )
        else -> {
            val title = if (reference.quotedRole == "user") stringResource(R.string.chats_replying_to_own)
            else stringResource(R.string.chats_replying_to, model.replyAuthorName(reference) ?: agentTitle)
            ReferenceStrip(Icons.AutoMirrored.Outlined.Reply, title, reference.quotedText.orEmpty(), drop)
        }
    }
}
