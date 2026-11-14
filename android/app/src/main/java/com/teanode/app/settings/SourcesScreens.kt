package com.teanode.app.settings

import androidx.compose.foundation.layout.Row
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.NightsStay
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.chats.ChatFilesCache
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.agent.DeclinedAttachment
import com.teanode.kit.agent.KnowledgeSourceAttachments
import com.teanode.kit.agent.KnowledgeSourceRow
import com.teanode.kit.agent.SourceShape
import com.teanode.kit.agent.declinedAttachments
import com.teanode.kit.agent.deleteKnowledgeSource
import com.teanode.kit.agent.hasAny
import com.teanode.kit.agent.isFinance
import com.teanode.kit.agent.knowledgeSourceDetails
import com.teanode.kit.agent.shape
import com.teanode.kit.agent.switchKnowledgeSource
import com.teanode.kit.agent.syncKnowledgeSource
import com.teanode.kit.chats.download
import com.teanode.kit.agent.whereItReads
import com.teanode.kit.mail.mailboxes
import com.teanode.kit.photos.ServerTime
import kotlinx.coroutines.delay

@Composable
private fun shapeName(shape: SourceShape): String = stringResource(
    when (shape) {
        SourceShape.Files -> R.string.settings_sources_shape_files
        SourceShape.Journal -> R.string.settings_sources_shape_journal
        SourceShape.Records -> R.string.settings_sources_shape_records
        SourceShape.Sent -> R.string.settings_sources_shape_sent
    },
)

/** What became of the pictures and files a source carried: waiting for a decision, decided against, or opened and read. */
@Composable
private fun filesText(attachments: KnowledgeSourceAttachments?): String? {
    if (attachments == null || !attachments.hasAny) return null
    val parts = mutableListOf<String>()
    if (attachments.undecided > 0) parts += stringResource(R.string.settings_sources_files_waiting, attachments.undecided)
    if (attachments.declined > 0) parts += stringResource(R.string.settings_sources_files_declined, attachments.declined)
    if (attachments.described > 0) parts += stringResource(R.string.settings_sources_files_read, attachments.described)
    return parts.joinToString(" · ")
}

/**
 * The sources the agent reads: each with its kind and state, where it reads, how much it read, what became of its
 * files and why it stopped; paused, read again now, edited, opened for the files it decided against, or forgotten.
 */
@Composable
fun KnowledgeSourcesScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.knowledgeSourceDetails() }
    val mailboxNames = rememberLoad { runCatching { connection.mailboxes() }.getOrDefault(emptyList()).mapNotNull { entry -> entry.mailbox }.associate { mailbox -> mailbox.id to mailbox.name.ifEmpty { mailbox.addresses.firstOrNull()?.address ?: mailbox.id } } }
    var removing by remember { mutableStateOf<KnowledgeSourceRow?>(null) }
    var isAddingSource by remember { mutableStateOf(false) }
    var editingSource by remember { mutableStateOf<KnowledgeSourceRow?>(null) }
    var declinedSource by remember { mutableStateOf<KnowledgeSourceRow?>(null) }
    val syncedText = stringResource(R.string.settings_sources_syncing)
    val pausedText = stringResource(R.string.settings_sources_paused_said)
    val resumedText = stringResource(R.string.settings_sources_resumed_said)
    val stoppedText = stringResource(R.string.settings_sources_stopped_said)

    // Read again while it is open, as the dashboard does: a source still reading says so until its pass ends.
    LaunchedEffect(Unit) {
        while (true) {
            delay(15_000)
            loaded.refresh()
        }
    }
    declinedSource?.let { source ->
        DeclinedFilesScreen(source, onBack = { declinedSource = null })
        return
    }
    LoadedSettingsPage(
        stringResource(R.string.settings_sources_title), onBack, loaded, isEmpty = { details -> details.sources.none { source -> !source.isFinance } },
        floatingActionButton = { FloatingActionButton(onClick = { isAddingSource = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_sources_add)) } },
        empty = { EmptyState(Icons.Outlined.NightsStay, stringResource(R.string.settings_sources_none), description = stringResource(R.string.settings_sources_none_detail)) },
    ) { details ->
        val shown = details.sources.filter { source -> !source.isFinance }
        items(shown.size) { index ->
            val source = shown[index]
            val attachments = details.attachments.firstOrNull { candidate -> candidate.sourceId == source.id }
            val lines = buildList {
                source.whereItReads(mailboxNames.value.orEmpty()).takeIf { text -> text.isNotEmpty() }?.let { text -> add(text) }
                add(stringResource(R.string.settings_sources_counts, source.documentCount, source.chunkCount) + if (source.refusedCount > 0) " · " + stringResource(R.string.settings_sources_refused, source.refusedCount) else "")
                filesText(attachments)?.let { text -> add(text) }
                if (source.checkoutsKeptToProfile > 0) add(stringResource(R.string.settings_sources_profile_line, source.checkoutsKeptToProfile, source.filesKeptToProfile))
                if (!source.enabled) add(stringResource(R.string.settings_sources_paused_line))
                source.lastRunAt?.let { stamp -> ServerTime.instant(stamp)?.let { instant -> add(stringResource(R.string.settings_last_used, relativeTimeText(instant.toEpochMilli()))) } }
                source.lastError.ifEmpty { null }?.let { text -> add(text) }
            }
            val kindText = when {
                source.kind == "recording" -> stringResource(R.string.settings_sources_tag_recording)
                source.specification.type.isEmpty() -> shapeName(source.shape)
                else -> source.specification.type
            }
            val menu = buildList {
                add(stringResource(R.string.settings_sources_edit_action) to { editingSource = source })
                add(stringResource(R.string.settings_sources_sync) to { actions.run(syncedText) { connection.syncKnowledgeSource(source.id) } })
                add(
                    stringResource(if (source.enabled) R.string.settings_sources_pause else R.string.settings_sources_resume) to {
                        actions.run(if (source.enabled) pausedText else resumedText, onSuccess = { loaded.refresh() }) { connection.switchKnowledgeSource(source.id, !source.enabled) }
                    },
                )
                if ((attachments?.declined ?: 0) > 0) add(stringResource(R.string.settings_sources_declined_open) to { declinedSource = source })
                add(stringResource(R.string.settings_sources_stop_reading) to { removing = source })
            }
            val tags = listOfNotNull(
                kindText, if (source.more) stringResource(R.string.settings_sources_tag_reading) else null, if (!source.enabled) stringResource(R.string.settings_sources_tag_paused) else null,
            ).joinToString(" · ")
            TeaNodeRow(
                onClick = { editingSource = source },
                headline = source.name, overline = tags, supporting = lines.joinToString("\n"), supportingMaxLines = 12,
                trailing = { RowMenu(menu) },
            )
        }
        item { FooterText(stringResource(R.string.settings_sources_page_footer)) }
    }
    if (isAddingSource || editingSource != null) {
        KnowledgeSourceEditorDialog(editingSource, actions, onDismiss = { isAddingSource = false; editingSource = null }, onSaved = { isAddingSource = false; editingSource = null; loaded.refresh() })
    }
    removing?.let { source ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_sources_remove, source.name), confirmLabel = stringResource(R.string.settings_sources_stop_reading), isDestructive = true, text = stringResource(R.string.settings_sources_remove_detail),
            onConfirm = { removing = null; actions.run(stoppedText, onSuccess = { loaded.refresh() }) { connection.deleteKnowledgeSource(source.id) } }, onDismiss = { removing = null },
        )
    }
}

/** The files one source's reading decided against opening, gathered under the reason it gave, each opened where its bytes are kept so a person who disagrees can look. */
@Composable
private fun DeclinedFilesScreen(source: KnowledgeSourceRow, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val context = LocalContext.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.declinedAttachments(source.id) }
    LoadedSettingsPage(
        stringResource(R.string.settings_sources_declined_open), onBack, loaded, isEmpty = { files -> files.isEmpty() },
        empty = { EmptyState(Icons.Outlined.NightsStay, stringResource(R.string.settings_sources_declined_none)) },
    ) { files ->
        DeclinedAttachment.groupedByReason(files).forEach { group ->
            item { com.teanode.app.ui.components.TeaNodeSectionHeader(group.declinedReason) }
            items(group.files.size) { index ->
                val file = group.files[index]
                TeaNodeRow(
                    headline = file.name.ifEmpty { file.documentId }, supporting = file.placeText.ifEmpty { null },
                    onClick = if (file.path.isNotEmpty()) ({ actions.run { openDeclinedFile(context, connection, file) } }) else null,
                )
            }
        }
    }
}

/** Downloads the file where the server keeps it and opens it in whatever shows it. */
private suspend fun openDeclinedFile(context: android.content.Context, connection: com.teanode.kit.Connection, file: DeclinedAttachment) {
    val bytes = connection.download(file.path)
    val target = java.io.File(ChatFilesCache.directory(context), ChatFilesCache.safeName(file.name.ifEmpty { "file" }))
    target.writeBytes(bytes)
    val address = androidx.core.content.FileProvider.getUriForFile(context, "${context.packageName}.chatfiles", target)
    val intent = android.content.Intent(android.content.Intent.ACTION_VIEW).apply {
        setDataAndType(address, file.contentType.ifEmpty { "application/octet-stream" })
        addFlags(android.content.Intent.FLAG_GRANT_READ_URI_PERMISSION or android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
    }
    context.startActivity(intent)
}
