package com.teanode.app.chats

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.RadioButton
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.Alignment
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.chats.ShareDestination
import com.teanode.kit.chats.SharedFiles
import com.teanode.kit.finance.importStatement
import com.teanode.kit.finance.readTaxDocumentOfAttachment
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.TeaNodeApplication
import com.teanode.app.session.AppSession
import com.teanode.app.session.ErrorWords
import com.teanode.app.ui.theme.TeaNodeTheme
import com.teanode.kit.Connection
import com.teanode.kit.chats.ask
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * ShareReceiverActivity is where text, links, pictures and files shared from other apps arrive: a sheet over the app
 * that shared, with a note and the files, which goes to the person's main conversation with the agent. It is the
 * Android side of the iPhone app's share extension, less what belongs to other parts of the app (tax documents,
 * statements and recordings).
 */
class ShareReceiverActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val session = (application as TeaNodeApplication).session
        setContent {
            TeaNodeTheme(session.appearance.value) {
                ShareSheet(intent, session, session.connection.value, onFinish = { finish() })
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun ShareSheet(intent: Intent, session: AppSession, connection: Connection?, onFinish: () -> Unit) {
    val activity = androidx.compose.ui.platform.LocalContext.current
    val scope = rememberCoroutineScope()
    val environment = remember(connection) { connection?.let { signedIn -> ChatsEnvironment.of(activity, signedIn, session) } }
    val facts by session.facts.collectAsState()
    var note by remember { mutableStateOf(sharedWords(intent)) }
    var files by remember { mutableStateOf<List<PendingFile>>(emptyList()) }
    var isReading by remember { mutableStateOf(true) }
    var isSending by remember { mutableStateOf(false) }
    var problemText by remember { mutableStateOf<String?>(null) }
    var resultText by remember { mutableStateOf<String?>(null) }
    var chosen by remember { mutableStateOf(ShareDestination.Agent) }
    val tooLarge = stringResource(R.string.chats_files_too_large)
    LaunchedEffect(intent) {
        files = withContext(Dispatchers.IO) { sharedFiles(activity, intent) }
        isReading = false
        if (files.sumOf { file -> file.data.size } > SharedFiles.BYTES_MOST) problemText = tooLarge
    }
    LaunchedEffect(environment) { environment?.loadAgentName() }
    val offered = SharedFiles.destinations(files.map { file -> Triple(file.name, file.contentType, file.data) }, facts.isAvailable(AppSection.Recordings))
    // The destination chosen holds only while the files still fit it; and files that are statements and nothing else were shared to be imported.
    val destination = when {
        chosen in offered -> chosen
        files.isNotEmpty() && files.all { file -> SharedFiles.isStatement(file.name, file.data) } && note.isBlank() -> ShareDestination.Statements
        else -> ShareDestination.Agent
    }
    val usesNote = destination == ShareDestination.Agent
    val canSend = connection != null && !isSending && problemText != tooLarge && when (destination) {
        ShareDestination.Agent -> note.trim().isNotEmpty() || files.isNotEmpty()
        ShareDestination.TaxDocuments -> files.any { file -> SharedFiles.isTaxDocument(file.contentType) }
        ShareDestination.Statements -> files.any { file -> SharedFiles.isStatement(file.name, file.data) }
        ShareDestination.Recording -> files.any { file -> SharedFiles.isRecordingMedia(file.contentType) }
    }
    val sentWords = stringResource(R.string.chats_share_sent)
    val taxOne = stringResource(R.string.chats_share_tax_read_one)
    val leftOutTax = stringResource(R.string.chats_share_left_out_tax)
    val leftOutStatements = stringResource(R.string.chats_share_left_out_statements)
    val summaryWords: (Int, Int, Int) -> String = { added, updated, unchanged -> activity.getString(R.string.chats_share_statement_summary, added, updated, unchanged) }
    ModalBottomSheet(onDismissRequest = onFinish, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.navigationBarsPadding().verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            val agentName = environment?.agentName.orEmpty()
            Text(
                if (agentName.isEmpty()) stringResource(R.string.chats_share_send_to_agent) else stringResource(R.string.chats_share_send_to, agentName),
                style = MaterialTheme.typography.titleLarge,
            )
            if (connection == null) {
                Text(stringResource(R.string.chats_share_not_connected), color = MaterialTheme.colorScheme.error)
            } else if (isReading) {
                CircularProgressIndicator()
            } else {
                if (files.isNotEmpty()) {
                    LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        items(files, key = { file -> file.id }) { file -> PendingFileChip(file, isSending, false) { files = files.filter { kept -> kept.id != file.id } } }
                    }
                }
                if (offered.size > 1 && resultText == null) {
                    Column {
                        for (choice in offered) {
                            Row(Modifier.fillMaxWidth().heightIn(min = 48.dp).clickable { chosen = choice }, verticalAlignment = Alignment.CenterVertically) {
                                RadioButton(selected = destination == choice, onClick = { chosen = choice })
                                Text(
                                    stringResource(
                                        when (choice) {
                                            ShareDestination.Agent -> R.string.chats_share_to_agent
                                            ShareDestination.TaxDocuments -> R.string.chats_share_as_tax
                                            ShareDestination.Statements -> R.string.chats_share_as_statements
                                            ShareDestination.Recording -> R.string.chats_share_as_recording
                                        },
                                    ),
                                )
                            }
                        }
                    }
                }
                if (resultText == null) {
                    if (usesNote) {
                        OutlinedTextField(value = note, onValueChange = { note = it }, label = { Text(stringResource(R.string.chats_share_note)) }, modifier = Modifier.fillMaxWidth(), minLines = 2)
                        Text(stringResource(R.string.chats_share_goes_to_main), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    } else {
                        Text(
                            stringResource(
                                when (destination) {
                                    ShareDestination.TaxDocuments -> R.string.chats_share_tax_detail
                                    ShareDestination.Statements -> R.string.chats_share_statements_detail
                                    else -> R.string.chats_share_recording_detail
                                },
                            ),
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
            resultText?.let { text -> Text(text, style = MaterialTheme.typography.bodyMedium) }
            problemText?.let { text -> Text(text, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium) }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                if (resultText != null) {
                    Button(onClick = onFinish) { Text(stringResource(R.string.chats_done)) }
                } else {
                    TextButton(onClick = onFinish) { Text(stringResource(R.string.chats_cancel)) }
                    Button(
                        onClick = {
                            val signedIn = connection ?: return@Button
                            val sending = environment ?: return@Button
                            isSending = true
                            problemText = null
                            scope.launch {
                                var handled = 0
                                var total = 1
                                var currentName = ""
                                try {
                                    when (destination) {
                                        ShareDestination.Agent -> {
                                            val ids = files.map { file -> signedIn.upload(file.name, file.contentType, file.data) }
                                            signedIn.ask(note.trim().ifEmpty { "(files)" }, null, sending.drawerId, attachmentIds = ids)
                                            Toast.makeText(activity, sentWords, Toast.LENGTH_SHORT).show()
                                            onFinish()
                                        }
                                        ShareDestination.TaxDocuments -> {
                                            val documents = files.filter { file -> SharedFiles.isTaxDocument(file.contentType) }
                                            total = documents.size
                                            for (file in documents) {
                                                currentName = file.name
                                                val id = signedIn.upload(file.name, file.contentType, file.data, isTaxDocument = true)
                                                signedIn.readTaxDocumentOfAttachment(id)
                                                handled += 1
                                                files = files.filter { kept -> kept.id != file.id }
                                            }
                                            var said = if (documents.size == 1) taxOne else activity.getString(R.string.chats_share_tax_read_several, documents.size)
                                            if (files.isNotEmpty()) said += " $leftOutTax"
                                            resultText = said
                                        }
                                        ShareDestination.Statements -> {
                                            val statements = files.filter { file -> SharedFiles.isStatement(file.name, file.data) }
                                            val summaries = mutableListOf<String>()
                                            total = statements.size
                                            for (file in statements) {
                                                currentName = file.name
                                                val id = signedIn.upload(file.name, file.contentType, file.data)
                                                val imported = signedIn.importStatement(id)
                                                handled += 1
                                                summaries.add(imported?.importErrorMessage ?: summaryWords(imported?.addedTransactionCount ?: 0, imported?.updatedTransactionCount ?: 0, imported?.unchangedTransactionCount ?: 0))
                                            }
                                            if (files.any { file -> !SharedFiles.isStatement(file.name, file.data) }) summaries.add(leftOutStatements)
                                            resultText = summaries.joinToString("\n")
                                        }
                                        ShareDestination.Recording -> {
                                            // The recordings part makes the recording from a file the agent was given, so the file is handed in first and its screen opens.
                                            val media = files.first { file -> SharedFiles.isRecordingMedia(file.contentType) }
                                            currentName = media.name
                                            val id = signedIn.upload(media.name, media.contentType, media.data)
                                            val address = Uri.parse(Routes.deepLink(recordingImportRoute(id, media.name, media.contentType, media.data.size)))
                                            activity.startActivity(Intent(Intent.ACTION_VIEW, address).setPackage(activity.packageName).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                                            onFinish()
                                        }
                                    }
                                } catch (cancelled: kotlinx.coroutines.CancellationException) {
                                    throw cancelled
                                } catch (failure: Throwable) {
                                    val reason = if (ErrorWords.isCancellation(failure)) "" else session.describe(failure)
                                    problemText = SharedFiles.partialFailure(reason, handled, total, currentName)
                                } finally {
                                    isSending = false
                                }
                            }
                        },
                        enabled = canSend,
                    ) {
                        if (isSending) CircularProgressIndicator(Modifier.padding(2.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary)
                        else Text(
                            stringResource(
                                when (destination) {
                                    ShareDestination.Agent -> R.string.chats_send
                                    ShareDestination.TaxDocuments -> R.string.chats_share_read
                                    ShareDestination.Statements -> R.string.chats_share_import
                                    ShareDestination.Recording -> R.string.chats_share_save_recording
                                },
                            ),
                        )
                    }
                }
            }
        }
    }
}

/** The words shared: the text, with the page's title above a link when the sharing app sent one. */
private fun sharedWords(intent: Intent): String {
    val text = intent.getStringExtra(Intent.EXTRA_TEXT).orEmpty()
    return text.trim()
}

@Suppress("DEPRECATION")
private fun streamsOf(intent: Intent): List<Uri> {
    if (intent.action == Intent.ACTION_SEND_MULTIPLE) {
        return (if (Build.VERSION.SDK_INT >= 33) intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java) else intent.getParcelableArrayListExtra<Uri>(Intent.EXTRA_STREAM)) ?: emptyList()
    }
    val single = if (Build.VERSION.SDK_INT >= 33) intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java) else intent.getParcelableExtra<Uri>(Intent.EXTRA_STREAM)
    return listOfNotNull(single)
}

private fun sharedFiles(activity: android.content.Context, intent: Intent): List<PendingFile> =
    // Any app can send this activity any address: file addresses and the app's own providers would read its private files.
    streamsOf(intent).filter { address -> SharedFiles.isReadableAddress(address.scheme, address.authority, activity.packageName) }.mapIndexedNotNull { index, address ->
        val type = activity.contentResolver.getType(address).orEmpty()
        if (type.startsWith("image/")) PendingFiles.picture(activity, address, index) else PendingFiles.file(activity, address)
    }
