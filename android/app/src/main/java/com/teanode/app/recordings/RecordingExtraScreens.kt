package com.teanode.app.recordings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Psychology
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.AppSection
import com.teanode.kit.photos.photoNamingContacts
import com.teanode.kit.photos.PhotoNamingContact
import com.teanode.kit.photos.VideoControls
import com.teanode.kit.recordings.RecordingAttachment
import com.teanode.kit.recordings.RecordingAttachmentImportDraft
import com.teanode.kit.recordings.RecordingMemoryLink
import com.teanode.kit.recordings.RecordingSpeakerIdentity
import com.teanode.kit.recordings.RecordingSpeakersView
import com.teanode.kit.recordings.RecordingTranscriptSegment
import com.teanode.kit.recordings.RecordingTranscriptTextCorrection
import com.teanode.kit.recordings.deleteRecordingVoiceReference
import com.teanode.kit.recordings.importRecordingAttachment
import com.teanode.kit.recordings.recording
import com.teanode.kit.recordings.recordingPassageMemory
import com.teanode.kit.recordings.recordingSpeakers
import com.teanode.kit.recordings.recordingTextCorrections
import com.teanode.kit.recordings.removeRecordingSpeakerCorrection
import com.teanode.kit.recordings.removeRecordingTranscriptTextCorrection
import com.teanode.kit.recordings.renameRecordingSpeaker
import com.teanode.kit.recordings.setRecordingSpeakerContact
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.util.UUID

/** The pages of the agent's memory linked to what was said in a passage ("In memory"): asked for when the item is chosen. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PassageMemorySheet(recordingId: String, segment: RecordingTranscriptSegment, onDismiss: () -> Unit, onOpen: (RecordingMemoryLink) -> Unit, previewPages: List<RecordingMemoryLink>? = null) {
    val connection = LocalConnection.current
    val pages by produceState<List<RecordingMemoryLink>?>(previewPages, segment.id) {
        if (previewPages == null) value = runCatching { connection.recordingPassageMemory(recordingId, segment.segmentStartMS, segment.segmentEndMS) }.getOrDefault(emptyList())
    }
    ModalBottomSheet(onDismissRequest = onDismiss) { PassageMemoryBody(pages, onOpen) }
}

@Composable
internal fun PassageMemoryBody(pages: List<RecordingMemoryLink>?, onOpen: (RecordingMemoryLink) -> Unit) {
    Column(Modifier.padding(bottom = 32.dp)) {
        Text(stringResource(R.string.recordings_in_memory), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 24.dp))
        when {
            pages == null -> Text(stringResource(R.string.recordings_memory_reading), modifier = Modifier.padding(24.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            pages.isEmpty() -> Text(stringResource(R.string.recordings_memory_none), modifier = Modifier.padding(24.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            else -> {
                for (page in pages) {
                    TeaNodeRow(
                        headline = page.nodeName.ifEmpty { page.nodePath }, leadingIcon = Icons.Outlined.Psychology,
                        trailing = { if (page.isProposed) Text(stringResource(R.string.recordings_memory_guess), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) },
                        onClick = { onOpen(page) },
                    )
                }
                Text(stringResource(R.string.recordings_memory_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp))
            }
        }
    }
}

private class SpeakersRead(val speakers: RecordingSpeakersView, val generation: Long)

/**
 * The speakers of a recording and their voice references (ios/TeaNode/Sources/RecordingTranscriptView.swift): the names
 * the person confirmed, each tied to a contact or not, the corrections they made (removable), and the passages the server
 * keeps to recognize a voice again (deletable). Only confirmed names connect people across recordings.
 */
@Composable
fun RecordingSpeakersScreen(recordingId: String, onBack: () -> Unit, previewSpeakers: RecordingSpeakersView? = null) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad(recordingId) {
        SpeakersRead(previewSpeakers ?: connection.recordingSpeakers(recordingId), runCatching { connection.recording(recordingId).recording.sourceGeneration }.getOrDefault(0))
    }
    var renaming by remember { mutableStateOf<RecordingSpeakerIdentity?>(null) }
    var linking by remember { mutableStateOf<RecordingSpeakerIdentity?>(null) }
    var removingReference by remember { mutableStateOf<String?>(null) }

    fun act(work: suspend (Long) -> Unit) {
        scope.launch { try { work(read.value?.generation ?: 0); read.refresh() } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) } }
    }
    TeaNodeScreen(title = stringResource(R.string.recordings_speakers_title), onBack = onBack) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            LoadedContent(read) { loaded ->
                val view = loaded.speakers
                LazyColumn(Modifier.fillMaxSize()) {
                    item {
                        TeaNodeSectionHeader(stringResource(R.string.recordings_confirmed_speakers))
                        if (view.speakerIdentities.isEmpty()) Text(stringResource(R.string.recordings_no_speakers), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    items(view.speakerIdentities, key = { identity -> identity.id }) { identity ->
                        Column {
                            TeaNodeRow(
                                headline = identity.confirmedName,
                                supporting = if (identity.isTiedToContact) stringResource(R.string.recordings_linked_to, identity.contactName.orEmpty()) else if (identity.isAccountOwner == true) stringResource(R.string.recordings_this_is_you) else null,
                                trailing = { TextButton(onClick = { renaming = identity }) { Text(stringResource(R.string.recordings_rename)) } },
                            )
                            if (identity.isAccountOwner != true) TextButton(onClick = { linking = identity }, modifier = Modifier.padding(start = 8.dp)) { Text(stringResource(R.string.recordings_link_contact)) }
                        }
                    }
                    item { TeaNodeSectionHeader(stringResource(R.string.recordings_confirmed_passages)) }
                    items(view.speakerCorrections.filter { correction -> !correction.isRemoved }, key = { correction -> correction.id }) { correction ->
                        TeaNodeRow(
                            headline = view.speakerIdentities.firstOrNull { identity -> identity.id == correction.speakerIdentityId }?.confirmedName.orEmpty(),
                            supporting = VideoControls.timeText(correction.passageStartMS / 1000.0) + " - " + VideoControls.timeText(minOf(correction.passageEndMS, 1_000_000_000L) / 1000.0),
                            trailing = { TextButton(onClick = { act { generation -> connection.removeRecordingSpeakerCorrection(recordingId, generation, correction.id) } }) { Text(stringResource(R.string.recordings_remove_correction), color = MaterialTheme.colorScheme.error) } },
                        )
                    }
                    item { TeaNodeSectionHeader(stringResource(R.string.recordings_voice_references)) }
                    val references = view.voiceReferences.filter { reference -> !reference.isRevoked }
                    if (references.isEmpty()) item { Text(stringResource(R.string.recordings_no_voice_references), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    items(references, key = { reference -> reference.id }) { reference ->
                        TeaNodeRow(
                            headline = view.speakerIdentities.firstOrNull { identity -> identity.id == reference.speakerIdentityId }?.confirmedName.orEmpty(),
                            supporting = VideoControls.timeText(reference.passageStartMS / 1000.0) + " - " + VideoControls.timeText(reference.passageEndMS / 1000.0),
                            trailing = { TextButton(onClick = { removingReference = reference.id }) { Text(stringResource(R.string.recordings_remove_voice_reference), color = MaterialTheme.colorScheme.error) } },
                        )
                    }
                    item { Text(stringResource(R.string.recordings_speakers_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp)) }
                }
            }
        }
    }
    renaming?.let { identity ->
        var name by remember { mutableStateOf(identity.confirmedName) }
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { renaming = null },
            title = { Text(stringResource(R.string.recordings_rename_speaker)) },
            text = { OutlinedTextField(value = name, onValueChange = { name = it }, singleLine = true, label = { Text(stringResource(R.string.recordings_confirmed_name)) }) },
            confirmButton = { TextButton(enabled = name.isNotBlank(), onClick = { renaming = null; act { generation -> connection.renameRecordingSpeaker(recordingId, generation, identity.id, name.trim()) } }) { Text(stringResource(R.string.recordings_rename)) } },
            dismissButton = { TextButton(onClick = { renaming = null }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
    linking?.let { identity -> LinkContactSheet(onChosen = { contact -> linking = null; act { generation -> connection.setRecordingSpeakerContact(recordingId, generation, identity.id, contact.addressBookId, contact.id) } }, onDismiss = { linking = null }) }
    removingReference?.let { referenceId ->
        ConfirmationDialog(
            title = stringResource(R.string.recordings_remove_voice_reference), confirmLabel = stringResource(R.string.recordings_remove_voice_reference), isDestructive = true, text = stringResource(R.string.recordings_remove_voice_reference_text),
            onConfirm = { removingReference = null; act { generation -> connection.deleteRecordingVoiceReference(recordingId, generation, referenceId) } }, onDismiss = { removingReference = null },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun LinkContactSheet(onChosen: (PhotoNamingContact) -> Unit, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    var typed by remember { mutableStateOf("") }
    val contacts by produceState<List<PhotoNamingContact>>(emptyList(), typed) {
        kotlinx.coroutines.delay(250)
        value = runCatching { connection.photoNamingContacts(typed) }.getOrDefault(emptyList())
    }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(bottom = 32.dp)) {
            Text(stringResource(R.string.recordings_link_contact), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 24.dp))
            OutlinedTextField(value = typed, onValueChange = { typed = it }, singleLine = true, placeholder = { Text(stringResource(R.string.recordings_search_contacts)) }, modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 8.dp))
            for (contact in contacts.take(8)) TeaNodeRow(headline = contact.shownName, onClick = { onChosen(contact) })
        }
    }
}

/** The corrections the person made to the transcript's words: the generated text stays, each correction can be removed. */
@Composable
fun RecordingEditHistoryScreen(recordingId: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad(recordingId) { connection.recordingTextCorrections(recordingId).filter { correction -> !correction.isRemoved } to runCatching { connection.recording(recordingId).recording.sourceGeneration }.getOrDefault(0L) }
    TeaNodeScreen(title = stringResource(R.string.recordings_edit_history), onBack = onBack) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            LoadedContent(read, isEmpty = { pair -> pair.first.isEmpty() }, empty = { EmptyState(Icons.Outlined.Edit, stringResource(R.string.recordings_no_corrections), description = stringResource(R.string.recordings_correct_text_note)) }) { (corrections, generation) ->
                LazyColumn(Modifier.fillMaxSize()) {
                    items(corrections, key = { correction -> correction.id }) { correction ->
                        CorrectionRow(correction) {
                            scope.launch { try { connection.removeRecordingTranscriptTextCorrection(recordingId, correction.id, generation); read.refresh() } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) } }
                        }
                    }
                    item { Text(stringResource(R.string.recordings_correct_text_note), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp)) }
                }
            }
        }
    }
}

@Composable
private fun CorrectionRow(correction: RecordingTranscriptTextCorrection, onRemove: () -> Unit) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(VideoControls.timeText(correction.passageStartMS / 1000.0) + " - " + VideoControls.timeText(correction.passageEndMS / 1000.0), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(correction.originalTranscriptText, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textDecoration = androidx.compose.ui.text.style.TextDecoration.LineThrough)
        Text(correction.correctedTranscriptText, style = MaterialTheme.typography.bodyLarge)
        TextButton(onClick = onRemove) { Text(stringResource(R.string.recordings_remove_text_correction), color = MaterialTheme.colorScheme.error) }
    }
}

/**
 * Makes a recording from a file the agent was given in a chat (ios/TeaNode/Sources/RecordingAttachmentImportView.swift).
 * The chat opens it with the attachment's id, name, type and size; a retry after a lost answer is the same import.
 */
@Composable
fun RecordingAttachmentImportScreen(attachment: RecordingAttachment, onBack: () -> Unit, onOpenRecording: (String) -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val preferences = remember { context.getSharedPreferences("teanode.recordingImports", android.content.Context.MODE_PRIVATE) }
    val draftKey = connection.accountKey + "|" + attachment.id
    val draft = remember(draftKey) { RecordingAttachmentImportDraft(connection.accountKey, attachment.id, preferences.getString(draftKey, null) ?: UUID.randomUUID().toString().also { id -> preferences.edit().putString(draftKey, id).apply() }, "") }
    var title by remember { mutableStateOf(attachment.name.substringBeforeLast('.')) }
    var isImporting by remember { mutableStateOf(false) }
    var importedId by remember { mutableStateOf<String?>(null) }
    TeaNodeScreen(title = stringResource(R.string.recordings_import_title), onBack = onBack) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(attachment.name, style = MaterialTheme.typography.titleMedium)
            Text(com.teanode.app.photos.bytesText(attachment.sizeBytes) + " · " + attachment.contentType, color = MaterialTheme.colorScheme.onSurfaceVariant)
            OutlinedTextField(value = title, onValueChange = { title = it }, singleLine = true, label = { Text(stringResource(R.string.recordings_title_field)) }, modifier = Modifier.fillMaxWidth(), enabled = importedId == null)
            if (importedId == null) {
                Button(
                    onClick = {
                        isImporting = true
                        scope.launch {
                            try {
                                val view = connection.importRecordingAttachment(attachment, draft.copy(recordingTitle = title.trim()))
                                preferences.edit().remove(draftKey).apply()
                                importedId = view.recording.id
                            } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) } finally { isImporting = false }
                        }
                    },
                    enabled = !isImporting && title.isNotBlank(),
                ) { Text(stringResource(if (isImporting) R.string.recordings_importing else R.string.recordings_import_attachment)) }
            } else {
                Text(stringResource(R.string.recordings_imported), color = MaterialTheme.colorScheme.primary)
                Button(onClick = { onOpenRecording(importedId!!) }) { Text(stringResource(R.string.recordings_open_recording)) }
            }
        }
    }
}
