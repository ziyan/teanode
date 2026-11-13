package com.teanode.app.chats

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.Arrangement
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
    var note by remember { mutableStateOf(sharedWords(intent)) }
    var files by remember { mutableStateOf<List<PendingFile>>(emptyList()) }
    var isReading by remember { mutableStateOf(true) }
    var isSending by remember { mutableStateOf(false) }
    var problemText by remember { mutableStateOf<String?>(null) }
    val tooLarge = stringResource(R.string.chats_files_too_large)
    LaunchedEffect(intent) {
        files = withContext(Dispatchers.IO) { sharedFiles(activity, intent) }
        isReading = false
        if (files.sumOf { file -> file.data.size } > PendingFile.BYTES_MOST) problemText = tooLarge
    }
    LaunchedEffect(environment) { environment?.loadAgentName() }
    val canSend = connection != null && !isSending && (note.trim().isNotEmpty() || files.isNotEmpty()) && problemText != tooLarge
    val sentWords = stringResource(R.string.chats_share_sent)
    ModalBottomSheet(onDismissRequest = onFinish, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.navigationBarsPadding().padding(horizontal = 20.dp).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
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
                OutlinedTextField(value = note, onValueChange = { note = it }, label = { Text(stringResource(R.string.chats_share_note)) }, modifier = Modifier.fillMaxWidth(), minLines = 2)
                Text(stringResource(R.string.chats_share_goes_to_main), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            problemText?.let { text -> Text(text, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium) }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onFinish) { Text(stringResource(R.string.chats_cancel)) }
                Button(
                    onClick = {
                        val signedIn = connection ?: return@Button
                        val sending = environment ?: return@Button
                        isSending = true
                        problemText = null
                        scope.launch {
                            try {
                                val ids = files.map { file -> signedIn.upload(file.name, file.contentType, file.data) }
                                val words = note.trim()
                                signedIn.ask(words.ifEmpty { "(files)" }, null, sending.drawerId, attachmentIds = ids)
                                Toast.makeText(activity, sentWords, Toast.LENGTH_SHORT).show()
                                onFinish()
                            } catch (cancelled: kotlinx.coroutines.CancellationException) {
                                throw cancelled
                            } catch (failure: Throwable) {
                                problemText = if (ErrorWords.isCancellation(failure)) null else session.describe(failure)
                                isSending = false
                            }
                        }
                    },
                    enabled = canSend,
                ) {
                    if (isSending) CircularProgressIndicator(Modifier.padding(2.dp), strokeWidth = 2.dp, color = MaterialTheme.colorScheme.onPrimary) else Text(stringResource(R.string.chats_send))
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
    streamsOf(intent).mapIndexedNotNull { index, address ->
        val type = activity.contentResolver.getType(address).orEmpty()
        if (type.startsWith("image/")) PendingFiles.picture(activity, address, index) else PendingFiles.file(activity, address)
    }
