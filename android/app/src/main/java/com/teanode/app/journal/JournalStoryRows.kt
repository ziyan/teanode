package com.teanode.app.journal

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowOutward
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.DateWords
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.kit.calendar.ServerTime
import com.teanode.kit.journal.JournalNarrative
import com.teanode.kit.journal.JournalRecord
import com.teanode.kit.journal.JournalWords
import com.teanode.kit.journal.JournalWorkProject

/**
 * The person's words: the touched-up version or the original, with the choice between them when touching up is on, and
 * the notes from their notes folder (ios JournalStoryViews.swift, JournalWordsRows).
 */
fun LazyListScope.journalWordsRows(words: JournalWords?, isShowingOriginal: Boolean, onShowOriginal: (Boolean) -> Unit) {
    if (words != null && words.isTouchUpOn) {
        item(key = "words-version") {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                FilterChip(selected = !isShowingOriginal, onClick = { onShowOriginal(false) }, label = { Text(stringResource(R.string.journal_touched_up)) })
                FilterChip(selected = isShowingOriginal, onClick = { onShowOriginal(true) }, label = { Text(stringResource(R.string.journal_your_original)) })
            }
        }
    }
    if (words != null && words.touchUpPending) {
        item(key = "words-pending") {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.journal_touching_up_words), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    if (words != null && words.hasOriginal) {
        item(key = "words-text") {
            val isTouchedUpShown = words.isTouchedUpShown && !isShowingOriginal
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (isTouchedUpShown) Text(stringResource(R.string.journal_touched_up_by_teanode), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                SelectionContainer {
                    Text(if (isTouchedUpShown) words.touchedUp else words.original, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace))
                }
                if (isTouchedUpShown && words.isTouchedUpStale && !words.touchUpPending) {
                    Text(stringResource(R.string.journal_changed_original_after), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
    val notes = words?.journalNotes.orEmpty()
    items(notes, key = { note -> "note-${note.documentId}" }) { note ->
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(note.title, style = MaterialTheme.typography.titleSmall)
            Text(note.text, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace))
            Text(stringResource(R.string.journal_from_your_notes), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
    if (words?.hasOriginal != true && notes.isEmpty()) {
        item(key = "words-empty") { Text(stringResource(R.string.journal_nothing_written_yet), modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/**
 * What TeaNode wrote: the story with the records it mentions, when it was written and from how much, what the person
 * said was wrong with it (each to take back), and the buttons to say something is wrong, to have it written again or to
 * remove it (ios JournalStoryViews.swift, JournalNarrativeRows).
 */
fun LazyListScope.journalNarrativeRows(
    narrative: JournalNarrative?,
    emptyTextResource: Int,
    writeNowTitleResource: Int,
    isBusy: Boolean,
    onOpen: (JournalRecord) -> Unit,
    onCorrect: () -> Unit,
    onWriteAgain: () -> Unit,
    onRemoveStory: () -> Unit,
    onRemoveCorrection: (Int) -> Unit,
) {
    val hasNarrative = narrative?.hasNarrative ?: false
    val isWriting = narrative?.narrativePending ?: false
    if (isWriting) {
        item(key = "story-writing") {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                Text(stringResource(R.string.journal_writing), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    if (narrative != null && hasNarrative) {
        item(key = "story-text") {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                JournalNarrativeView(narrative, onOpen)
                ServerTime.instant(narrative.writtenAt)?.let { writtenAt ->
                    Text(
                        stringResource(R.string.journal_written_from, DateWords.abbreviatedDateTime(writtenAt), narrative.sourceCount),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    } else if (!isWriting) {
        item(key = "story-empty") { Text(stringResource(emptyTextResource), modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
    val failure = narrative?.failure.orEmpty()
    if (failure.isNotEmpty() && !hasNarrative && !isWriting) {
        item(key = "story-failure") { Text(failure, modifier = Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
    val corrections = narrative?.corrections.orEmpty()
    items(corrections.size, key = { index -> "correction-$index" }) { index -> CorrectionRow(corrections[index].correctionText) { onRemoveCorrection(index) } }
    if (hasNarrative) {
        item(key = "story-wrong") { JournalAction(stringResource(R.string.journal_something_wrong), Icons.Outlined.ErrorOutline, onClick = onCorrect) }
    }
    if (hasNarrative || !isWriting) {
        item(key = "story-again") {
            JournalAction(if (hasNarrative) stringResource(R.string.journal_write_again) else stringResource(writeNowTitleResource), Icons.Outlined.Refresh, isEnabled = !isBusy && !isWriting, onClick = onWriteAgain)
        }
    }
    if (hasNarrative) {
        item(key = "story-remove") { RemoveStoryAction(isEnabled = !isBusy && !isWriting, onRemove = onRemoveStory) }
    }
}

@Composable
private fun RemoveStoryAction(isEnabled: Boolean, onRemove: () -> Unit) {
    var isConfirming by remember { mutableStateOf(false) }
    JournalAction(stringResource(R.string.journal_remove_this_story), Icons.Outlined.Delete, isEnabled = isEnabled, isDestructive = true, onClick = { isConfirming = true })
    if (isConfirming) {
        ConfirmationDialog(
            title = stringResource(R.string.journal_remove_this_story_question),
            text = stringResource(R.string.journal_remove_story_message),
            confirmLabel = stringResource(R.string.journal_remove),
            isDestructive = true,
            onConfirm = {
                isConfirming = false
                onRemove()
            },
            onDismiss = { isConfirming = false },
        )
    }
}

@Composable
private fun CorrectionRow(correctionText: String, onRemove: () -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth().padding(start = 16.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(stringResource(R.string.journal_you_said, correctionText), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
        androidx.compose.foundation.layout.Box {
            IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.Delete, contentDescription = stringResource(R.string.journal_delete)) }
            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.journal_delete), color = MaterialTheme.colorScheme.error) }, onClick = { isMenuOpen = false; onRemove() })
            }
        }
    }
}

/** A button-like row of the journal's lists: a symbol and a verb, in the color of what can be pressed. */
@Composable
fun JournalAction(title: String, icon: androidx.compose.ui.graphics.vector.ImageVector, isEnabled: Boolean = true, isDestructive: Boolean = false, onClick: () -> Unit) {
    val color = when {
        !isEnabled -> MaterialTheme.colorScheme.outline
        isDestructive -> MaterialTheme.colorScheme.error
        else -> MaterialTheme.colorScheme.primary
    }
    Row(
        Modifier.fillMaxWidth().clickable(enabled = isEnabled, onClick = onClick).padding(horizontal = 16.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(24.dp))
        Text(title, color = color)
    }
}

/**
 * One project of the day as one row: its name, how long it took and when, what the agent wrote was done from what was
 * said in its sessions, the sessions' names, and the requests that were sent for review, each opening in the browser
 * (ios JournalWorkViews.swift).
 */
@Composable
fun JournalWorkProjectRow(project: JournalWorkProject, modifier: Modifier = Modifier) {
    val text = rememberJournalText()
    val uriHandler = LocalUriHandler.current
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(project.name, style = MaterialTheme.typography.titleMedium)
        val timeText = text.projectTimeText(project)
        if (timeText.isNotEmpty()) Text(timeText, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (project.summary.isNotEmpty()) Text(project.summary, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace))
        // The sessions' names, as one quiet line: labels kept for weeks, not what was done that day, which the summary above says.
        val listed = project.shownTopics()
        if (listed.shown.isNotEmpty()) {
            val more = if (listed.hiddenCount > 0) " " + stringResource(R.string.journal_and_n_more, listed.hiddenCount) else ""
            Text(stringResource(R.string.journal_sessions, listed.shown.joinToString(", ") + more), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        for (request in project.shipped) {
            val address = request.webAddress
            Row(
                Modifier.fillMaxWidth().then(if (address != null) Modifier.clickable { uriHandler.openUri(address) } else Modifier).padding(vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(request.title, modifier = Modifier.weight(1f, fill = false), color = if (address != null) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface)
                if (request.isDraft) {
                    Text(
                        stringResource(R.string.journal_draft), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.background(MaterialTheme.colorScheme.surfaceVariant, androidx.compose.foundation.shape.RoundedCornerShape(50)).padding(horizontal = 6.dp, vertical = 1.dp),
                    )
                }
                if (address != null) Icon(Icons.Outlined.ArrowOutward, contentDescription = null, modifier = Modifier.size(16.dp), tint = MaterialTheme.colorScheme.primary)
            }
        }
    }
}

