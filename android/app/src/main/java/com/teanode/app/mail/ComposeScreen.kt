package com.teanode.app.mail

import android.graphics.Bitmap
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.PhotoCamera
import androidx.compose.material.icons.outlined.Send
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.contacts.addressBooks
import com.teanode.kit.contacts.contacts
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.io.ByteArrayOutputStream

/** ComposeScreen is a message being written: from, to, subject, the body, its quote and attachments, with its draft kept saved. */
@Composable
fun ComposeScreen(mail: MailModel, start: ComposeStart, onClose: () -> Unit) {
    val model = remember(start) { ComposeModel(mail, start) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var isConfirmingClose by remember { mutableStateOf(false) }
    var isFromMenuOpen by remember { mutableStateOf(false) }
    var isAttachMenuOpen by remember { mutableStateOf(false) }

    LaunchedEffect(model, mail.isLoaded) {
        if (!mail.isLoaded) mail.load()
        model.load()
    }
    // The draft is kept on the server every half minute, and when the person leaves the screen.
    LaunchedEffect(model) {
        while (true) {
            delay(30_000)
            model.save()
        }
    }
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner, model) {
        val observer = LifecycleEventObserver { _, event -> if (event == Lifecycle.Event.ON_STOP) scope.launch { model.save() } }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }

    fun read(uris: List<Uri>): List<PendingFile> = uris.mapNotNull { address ->
        runCatching {
            val resolver = context.contentResolver
            val name = resolver.query(address, null, null, null, null)?.use { cursor ->
                val column = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                if (cursor.moveToFirst() && column >= 0) cursor.getString(column) else null
            } ?: address.lastPathSegment ?: "file"
            val data = resolver.openInputStream(address)?.use { input -> input.readBytes() } ?: return@runCatching null
            PendingFile(name, resolver.getType(address) ?: "application/octet-stream", data)
        }.getOrNull()
    }

    val pickFiles = rememberLauncherForActivityResult(ActivityResultContracts.GetMultipleContents()) { uris -> scope.launch { model.uploadFiles(read(uris)) } }
    val pickPhotos = rememberLauncherForActivityResult(ActivityResultContracts.GetMultipleContents()) { uris -> scope.launch { model.uploadFiles(read(uris)) } }
    val takePicture = rememberLauncherForActivityResult(ActivityResultContracts.TakePicturePreview()) { bitmap: Bitmap? ->
        if (bitmap != null) {
            val output = ByteArrayOutputStream()
            bitmap.compress(Bitmap.CompressFormat.JPEG, 90, output)
            scope.launch { model.uploadFiles(listOf(PendingFile("camera.jpg", "image/jpeg", output.toByteArray()))) }
        }
    }

    val title = stringResource(
        when (start.kind) {
            ComposeKind.New -> R.string.mail_new_message
            ComposeKind.Reply, ComposeKind.ReplyAll -> R.string.mail_reply
            ComposeKind.Forward -> R.string.mail_forward
            ComposeKind.Draft -> R.string.mail_draft_title
        },
    )
    TeaNodeScreen(
        title = title,
        onBack = { isConfirmingClose = true },
        actions = {
            if (model.isSaving) {
                Text(stringResource(R.string.mail_saving), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            } else if (model.savedAt != null) {
                Text(stringResource(R.string.mail_draft_saved, MailTimes.timeOfDay(model.savedAt!!)), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            IconButton(onClick = { scope.launch { if (model.send()) onClose() } }, enabled = model.canSend) { Icon(Icons.Outlined.Send, stringResource(R.string.mail_send)) }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            model.warningText?.let { warning ->
                Row(verticalAlignment = Alignment.Top) {
                    Icon(Icons.Outlined.WarningAmber, null, tint = androidx.compose.ui.graphics.Color(0xFFE08A00), modifier = Modifier.padding(end = 8.dp, top = 2.dp))
                    Text(warning, style = MaterialTheme.typography.bodyMedium)
                }
                if (model.pending != null) {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Button(onClick = { scope.launch { if (model.send()) onClose() } }) { Text(stringResource(R.string.mail_try_again)) }
                        OutlinedButton(onClick = { scope.launch { if (model.returnToEditing()) onClose() } }) { Text(stringResource(R.string.mail_back_to_editing)) }
                    }
                }
            }
            val addresses = model.mailbox?.sendingAddresses ?: emptyList()
            val isLocked = model.pending != null
            if (addresses.size > 1) {
                Box {
                    OutlinedButton(onClick = { isFromMenuOpen = true }, enabled = !isLocked, modifier = Modifier.fillMaxWidth()) {
                        Text(stringResource(R.string.mail_from) + "  " + model.from, modifier = Modifier.weight(1f))
                    }
                    DropdownMenu(expanded = isFromMenuOpen, onDismissRequest = { isFromMenuOpen = false }) {
                        for (address in addresses) DropdownMenuItem(text = { Text(address) }, onClick = { isFromMenuOpen = false; model.from = address })
                    }
                }
            } else {
                Row(Modifier.padding(vertical = 4.dp)) {
                    Text(stringResource(R.string.mail_from), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(72.dp))
                    Text(model.from.ifEmpty { stringResource(R.string.mail_no_address_to_send_from) })
                }
            }
            AddressField(stringResource(R.string.mail_to), model.to, { text -> model.to = text }, mail, isLocked)
            if (model.isShowingCopies) {
                AddressField(stringResource(R.string.mail_cc), model.cc, { text -> model.cc = text }, mail, isLocked)
                AddressField(stringResource(R.string.mail_bcc), model.bcc, { text -> model.bcc = text }, mail, isLocked)
            } else {
                TextButton(onClick = { model.isShowingCopies = true }) { Text(stringResource(R.string.mail_add_cc_bcc)) }
            }
            OutlinedTextField(model.subject, { text -> model.subject = text }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_subject)) }, singleLine = true, enabled = !isLocked)
            if (model.isReply && model.mailbox?.agent?.granted == true && model.mailbox?.agent?.draftReplies == true) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    OutlinedTextField(model.agentInstructions, { text -> model.agentInstructions = text }, Modifier.weight(1f), label = { Text(stringResource(R.string.mail_reply_instructions)) }, singleLine = true)
                    if (model.isDrafting) {
                        CircularProgressIndicator(Modifier.padding(start = 12.dp).size(24.dp))
                    } else {
                        TextButton(onClick = { scope.launch { model.draftWithAgent() } }) { Text(stringResource(R.string.mail_draft_action)) }
                    }
                }
                Text(stringResource(R.string.mail_agent_writes_reply), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            OutlinedTextField(model.body, { text -> model.changeBody(text) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_message)) }, minLines = 8, enabled = !isLocked)
            if (model.quoteText.isNotEmpty()) {
                Text(stringResource(if (start.kind == ComposeKind.Forward) R.string.mail_forwarded_message else R.string.mail_quoted_text), style = MaterialTheme.typography.titleSmall)
                SelectionContainer { Text(model.quoteText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            HorizontalDivider()
            for (file in model.carried) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Outlined.AttachFile, null, Modifier.padding(end = 8.dp))
                    Text(file.filename.ifEmpty { stringResource(R.string.mail_attachment) }, Modifier.weight(1f), maxLines = 1)
                    Text(fileSize(file.size.toLong()), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    IconButton(onClick = { model.remove(file) }, enabled = !isLocked) { Icon(Icons.Outlined.Close, stringResource(R.string.mail_remove)) }
                }
            }
            if (model.uploadingCount > 0) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    CircularProgressIndicator(Modifier.size(20.dp))
                    Spacer(Modifier.width(12.dp))
                    Text(pluralStringResource(R.plurals.mail_uploading_files, model.uploadingCount, model.uploadingCount), color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            Box {
                OutlinedButton(onClick = { isAttachMenuOpen = true }, enabled = !isLocked) {
                    Icon(Icons.Outlined.AttachFile, null, Modifier.size(18.dp))
                    Spacer(Modifier.width(8.dp))
                    Text(stringResource(R.string.mail_attach))
                }
                DropdownMenu(expanded = isAttachMenuOpen, onDismissRequest = { isAttachMenuOpen = false }) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.mail_photo_library)) }, leadingIcon = { Icon(Icons.Outlined.Image, null) }, onClick = { isAttachMenuOpen = false; pickPhotos.launch("image/*") })
                    DropdownMenuItem(text = { Text(stringResource(R.string.mail_camera)) }, leadingIcon = { Icon(Icons.Outlined.PhotoCamera, null) }, onClick = { isAttachMenuOpen = false; takePicture.launch(null) })
                    DropdownMenuItem(text = { Text(stringResource(R.string.mail_files)) }, leadingIcon = { Icon(Icons.Outlined.Folder, null) }, onClick = { isAttachMenuOpen = false; pickFiles.launch("*/*") })
                }
            }
            if (model.isLoaded && model.mailbox?.sendingAddresses?.isEmpty() == true) {
                Text(stringResource(R.string.mail_no_address_to_send_from_yet), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Spacer(Modifier.size(24.dp))
        }
    }

    if (isConfirmingClose) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { isConfirmingClose = false },
            title = { Text(stringResource(R.string.mail_close_message_question)) },
            text = { Text(stringResource(R.string.mail_discarded_draft_warning)) },
            confirmButton = {
                TextButton(onClick = {
                    isConfirmingClose = false
                    scope.launch { model.save(isForced = true); onClose() }
                }) { Text(stringResource(R.string.mail_save_draft)) }
            },
            dismissButton = {
                Row {
                    TextButton(onClick = { isConfirmingClose = false }) { Text(stringResource(R.string.common_cancel)) }
                    TextButton(onClick = {
                        isConfirmingClose = false
                        scope.launch { model.discard(); onClose() }
                    }) { Text(stringResource(R.string.mail_discard_draft), color = MaterialTheme.colorScheme.error) }
                }
            },
        )
    }
}

/** An address field that offers the contacts matching what is being typed after the last comma. */
@Composable
fun AddressField(title: String, text: String, onChange: (String) -> Unit, mail: MailModel, isLocked: Boolean) {
    var suggestions by remember { mutableStateOf(emptyList<String>()) }
    var isFocused by remember { mutableStateOf(false) }
    val typing = text.substringAfterLast(',').substringAfterLast(';').trim()
    LaunchedEffect(text, isFocused) {
        if (!isFocused || typing.length < 2) {
            suggestions = emptyList()
            return@LaunchedEffect
        }
        delay(150)
        val book = runCatching { mail.connection.addressBooks().firstOrNull() }.getOrNull() ?: return@LaunchedEffect
        val found = runCatching { mail.connection.contacts(book.id, typing, 10) }.getOrNull() ?: return@LaunchedEffect
        suggestions = found.flatMap { contact -> (contact.emails ?: emptyList()).map { email -> if (contact.name.isNullOrEmpty()) email else "${contact.name} <$email>" } }
    }
    Column {
        OutlinedTextField(
            text, onChange, Modifier.fillMaxWidth().onFocusChanged { state -> isFocused = state.isFocused }, label = { Text(title) }, enabled = !isLocked,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false),
        )
        if (isFocused && suggestions.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (suggestion in suggestions) {
                    AssistChip(onClick = {
                        val keptText = if (text.contains(',') || text.contains(';')) text.substring(0, maxOf(text.lastIndexOf(','), text.lastIndexOf(';')) + 1).trim() else ""
                        onChange((if (keptText.isEmpty()) "" else "$keptText ") + suggestion + ", ")
                        suggestions = emptyList()
                    }, label = { Text(suggestion) })
                }
            }
        }
    }
}
