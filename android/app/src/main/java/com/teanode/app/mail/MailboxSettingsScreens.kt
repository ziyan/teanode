package com.teanode.app.mail

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Reply
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.CreateNewFolder
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Devices
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material.icons.outlined.Draw
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.mail.MailAppPassword
import com.teanode.kit.mail.MailAppPasswordCreated
import com.teanode.kit.mail.MailAppPasswords
import com.teanode.kit.mail.MailAutoReplyDraft
import com.teanode.kit.mail.MailFolder
import com.teanode.kit.mail.MailFolders
import com.teanode.kit.mail.ServerTime
import com.teanode.kit.mail.createMailAppPassword
import com.teanode.kit.mail.createMailFolder
import com.teanode.kit.mail.deleteMailAppPassword
import com.teanode.kit.mail.deleteMailFolder
import com.teanode.kit.mail.mailAppPasswords
import com.teanode.kit.mail.pinMailFolder
import com.teanode.kit.mail.renameMailFolder
import com.teanode.kit.mail.updateMailbox
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.ZoneId

/** The mailbox's settings page: its name and signature, auto-reply, folders, rules and the passwords mail programs use. */
@Composable
fun MailboxSettingsScreen(
    mail: MailModel,
    onBack: () -> Unit,
    onOpenGeneral: () -> Unit,
    onOpenAutoReply: () -> Unit,
    onOpenFolders: () -> Unit,
    onOpenRules: () -> Unit,
    onOpenPrograms: () -> Unit,
) {
    LaunchedEffect(mail) { if (!mail.isLoaded) mail.load() }
    val mailbox = mail.mailbox
    TeaNodeScreen(title = mailbox?.name ?: stringResource(R.string.mail_mailbox), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
            if (mailbox != null) {
                TeaNodeNavigationRow(stringResource(R.string.mail_name_and_signature), onClick = onOpenGeneral, leadingIcon = Icons.Outlined.Draw)
                TeaNodeNavigationRow(
                    stringResource(R.string.mail_auto_reply), onClick = onOpenAutoReply, leadingIcon = Icons.AutoMirrored.Outlined.Reply,
                    trailing = { Text(stringResource(if (mailbox.autoReply?.enabled == true) R.string.mail_on else R.string.mail_off), color = MaterialTheme.colorScheme.onSurfaceVariant) },
                )
                TeaNodeNavigationRow(stringResource(R.string.mail_folders), onClick = onOpenFolders, leadingIcon = Icons.Outlined.Folder)
                TeaNodeNavigationRow(
                    stringResource(R.string.mail_rules_title), onClick = onOpenRules, leadingIcon = Icons.Outlined.FilterList,
                    trailing = { Text((mailbox.rules?.size ?: 0).toString(), color = MaterialTheme.colorScheme.onSurfaceVariant) },
                )
                TeaNodeNavigationRow(stringResource(R.string.mail_programs), onClick = onOpenPrograms, leadingIcon = Icons.Outlined.Devices)
                Text(stringResource(R.string.mail_addresses, mailbox.sendingAddresses.joinToString(", ")), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

/** The name of the mailbox and the signature added to new messages. */
@Composable
fun MailboxGeneralScreen(mail: MailModel, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    LaunchedEffect(mail) { if (!mail.isLoaded) mail.load() }
    var name by remember(mail.mailbox?.id) { mutableStateOf(mail.mailbox?.name ?: "") }
    var signature by remember(mail.mailbox?.id) { mutableStateOf(mail.mailbox?.signatureText ?: "") }
    TeaNodeScreen(
        title = stringResource(R.string.mail_name_and_signature), onBack = onBack,
        actions = {
            TextButton(enabled = name.isNotBlank() && mail.mailbox != null, onClick = {
                val mailboxId = mail.mailbox?.id ?: return@TextButton
                scope.launch { if (mail.act(context.getString(R.string.mail_saved)) { mail.connection.updateMailbox(mailboxId, name = name.trim(), signature = signature) }) onBack() }
            }) { Text(stringResource(R.string.mail_save)) }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedTextField(name, { typed -> name = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_name)) }, singleLine = true)
            OutlinedTextField(signature, { typed -> signature = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_signature)) }, minLines = 3)
            Text(stringResource(R.string.mail_signature_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** The automatic reply: whether it is on, between which days, to whom, and what it says. */
@Composable
fun AutoReplyScreen(mail: MailModel, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    LaunchedEffect(mail) { if (!mail.isLoaded) mail.load() }
    var draft by remember(mail.mailbox?.id) { mutableStateOf(MailAutoReplyDraft.of(mail.mailbox?.autoReply)) }
    TeaNodeScreen(
        title = stringResource(R.string.mail_auto_reply), onBack = onBack,
        actions = {
            TextButton(enabled = !(draft.isEnabled && draft.text.isBlank()) && mail.mailbox != null, onClick = {
                val mailboxId = mail.mailbox?.id ?: return@TextButton
                scope.launch { if (mail.act(context.getString(R.string.mail_saved)) { mail.connection.updateMailbox(mailboxId, autoReply = draft) }) onBack() }
            }) { Text(stringResource(R.string.mail_save)) }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.mail_reply_automatically), Modifier.weight(1f)); Switch(draft.isEnabled, { checked -> draft = draft.copy(isEnabled = checked) }) }
            OptionalDayField(stringResource(R.string.mail_from), draft.from?.atZone(ZoneId.systemDefault())?.toLocalDate()) { day -> draft = draft.copy(from = day?.atStartOfDay(ZoneId.systemDefault())?.toInstant()) }
            OptionalDayField(stringResource(R.string.mail_until), draft.until?.atZone(ZoneId.systemDefault())?.toLocalDate()) { day -> draft = draft.copy(until = day?.atStartOfDay(ZoneId.systemDefault())?.toInstant()) }
            Row(verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.mail_only_to_my_domains), Modifier.weight(1f)); Switch(draft.isSameDomainOnly, { checked -> draft = draft.copy(isSameDomainOnly = checked) }) }
            OutlinedTextField(draft.subject, { typed -> draft = draft.copy(subject = typed) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_auto_reply_subject_hint)) }, singleLine = true)
            OutlinedTextField(draft.text, { typed -> draft = draft.copy(text = typed) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_message)) }, minLines = 5)
        }
    }
}

/** The folders of the mailbox with their message counts: added, renamed, pinned, deleted. */
@Composable
fun MailFoldersScreen(mail: MailModel, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    LaunchedEffect(mail) { if (!mail.isLoaded) mail.load() }
    var isAdding by remember { mutableStateOf(false) }
    var newName by remember { mutableStateOf("") }
    var renaming by remember { mutableStateOf<MailFolder?>(null) }
    var renamed by remember { mutableStateOf("") }
    var deleting by remember { mutableStateOf<MailFolder?>(null) }
    var menuFolderId by remember { mutableStateOf<String?>(null) }
    TeaNodeScreen(
        title = stringResource(R.string.mail_folders), onBack = onBack,
        actions = { IconButton(onClick = { newName = ""; isAdding = true }) { Icon(Icons.Outlined.CreateNewFolder, stringResource(R.string.mail_new_folder)) } },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
            for (row in MailFolders.rows(mail.folders)) {
                val folder = row.folder
                Row(Modifier.fillMaxWidth().padding(start = (16 + row.depth.coerceAtMost(3) * 16).dp, end = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                    Icon(MailNames.icon(folder), null, Modifier.padding(end = 16.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(MailNames.folder(folder), Modifier.weight(1f).padding(vertical = 16.dp))
                    if (folder.pinnedAt != null) Icon(Icons.Outlined.PushPin, stringResource(R.string.mail_pinned), Modifier.padding(end = 8.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(folder.total.toString(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Box {
                        IconButton(onClick = { menuFolderId = folder.id }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.mail_actions)) }
                        DropdownMenu(expanded = menuFolderId == folder.id, onDismissRequest = { menuFolderId = null }) {
                            if (folder.kind != "inbox") {
                                DropdownMenuItem(
                                    text = { Text(stringResource(if (folder.pinnedAt == null) R.string.mail_pin else R.string.mail_unpin)) }, leadingIcon = { Icon(Icons.Outlined.PushPin, null) },
                                    onClick = {
                                        menuFolderId = null
                                        scope.launch { mail.act(context.getString(if (folder.pinnedAt == null) R.string.mail_pinned_done else R.string.mail_unpinned_done)) { mail.connection.pinMailFolder(folder.id, folder.pinnedAt == null) } }
                                    },
                                )
                            }
                            if (folder.kind.isEmpty()) {
                                DropdownMenuItem(text = { Text(stringResource(R.string.mail_rename)) }, leadingIcon = { Icon(Icons.Outlined.Edit, null) }, onClick = { menuFolderId = null; renamed = folder.name; renaming = folder })
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.mail_delete), color = MaterialTheme.colorScheme.error) }, leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
                                    onClick = { menuFolderId = null; deleting = folder },
                                )
                            }
                        }
                    }
                }
                HorizontalDivider()
            }
        }
    }
    if (isAdding) {
        NameDialog(stringResource(R.string.mail_new_folder), stringResource(R.string.mail_name), newName, { typed -> newName = typed }, stringResource(R.string.mail_add), onDismiss = { isAdding = false }) {
            isAdding = false
            val mailboxId = mail.mailbox?.id
            val name = newName.trim()
            if (mailboxId != null && name.isNotEmpty()) scope.launch { mail.act(context.getString(R.string.mail_folder_added)) { mail.connection.createMailFolder(mailboxId, name, null) } }
        }
    }
    renaming?.let { folder ->
        NameDialog(stringResource(R.string.mail_rename), stringResource(R.string.mail_name), renamed, { typed -> renamed = typed }, stringResource(R.string.mail_save), onDismiss = { renaming = null }) {
            renaming = null
            val name = renamed.trim()
            if (name.isNotEmpty()) scope.launch { mail.act(context.getString(R.string.mail_saved)) { mail.connection.renameMailFolder(folder.id, name) } }
        }
    }
    deleting?.let { folder ->
        ConfirmationDialog(
            title = stringResource(R.string.mail_delete_folder_question, folder.name), text = stringResource(R.string.mail_folder_messages_deleted_with_it, folder.total), confirmLabel = stringResource(R.string.mail_delete_folder), isDestructive = true,
            onConfirm = { deleting = null; scope.launch { mail.act(context.getString(R.string.mail_folder_deleted)) { mail.connection.deleteMailFolder(folder.id) } } },
            onDismiss = { deleting = null },
        )
    }
}

/** A small dialog with one text field and a button to take it. */
@Composable
fun NameDialog(title: String, label: String, text: String, onChange: (String) -> Unit, confirmLabel: String, onDismiss: () -> Unit, onConfirm: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss, title = { Text(title) },
        text = { OutlinedTextField(text, onChange, label = { Text(label) }, singleLine = true) },
        confirmButton = { TextButton(onClick = onConfirm, enabled = text.isNotBlank()) { Text(confirmLabel) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The passwords mail programs use for the mailbox, and what a mail program needs to reach the server. */
@Composable
fun MailAppPasswordsScreen(mail: MailModel, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    LaunchedEffect(mail) { if (!mail.isLoaded) mail.load() }
    val mailboxId = mail.mailbox?.id
    var loaded by remember { mutableStateOf<MailAppPasswords?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var isAdding by remember { mutableStateOf(false) }
    var newName by remember { mutableStateOf("") }
    var created by remember { mutableStateOf<MailAppPasswordCreated?>(null) }
    var revoking by remember { mutableStateOf<MailAppPassword?>(null) }

    suspend fun load() {
        if (mailboxId == null) return
        try {
            loaded = mail.connection.mailAppPasswords(mailboxId)
            failure = null
        } catch (failed: kotlinx.coroutines.CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            if (loaded == null) failure = failed else mail.snackbar?.showError(failed)
        }
    }
    LaunchedEffect(mailboxId) { load() }

    TeaNodeScreen(
        title = stringResource(R.string.mail_programs), onBack = onBack,
        actions = { IconButton(onClick = { newName = ""; isAdding = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.mail_new_password)) } },
    ) { contentPadding ->
        val shown = loaded
        when {
            shown != null -> Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState())) {
                TeaNodeSectionHeader(stringResource(R.string.mail_devices))
                if (shown.passwords.isEmpty()) Text(stringResource(R.string.mail_no_program_has_password), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                for (password in shown.passwords.sortedByDescending { password -> password.createdAt }) {
                    val createdAt = ServerTime.instant(password.createdAt)
                    val lastUsedAt = ServerTime.instant(password.lastUsedAt)
                    TeaNodeRow(
                        headline = password.name,
                        supporting = buildString {
                            if (createdAt != null) append(stringResource(R.string.mail_password_created, MailTimes.dateAndTime(createdAt)))
                            append(" · ")
                            append(if (lastUsedAt != null) stringResource(R.string.mail_password_last_used, MailTimes.dateAndTime(lastUsedAt)) else stringResource(R.string.mail_password_never_used))
                        },
                        trailing = { IconButton(onClick = { revoking = password }) { Icon(Icons.Outlined.Delete, stringResource(R.string.mail_revoke), tint = MaterialTheme.colorScheme.error) } },
                    )
                }
                val settings = shown.settings
                if (settings != null) {
                    TeaNodeSectionHeader(stringResource(R.string.mail_settings_for_program))
                    SelectionContainer {
                        Column {
                            SettingLine(stringResource(R.string.mail_incoming_imap), "${settings.imapHost}:${settings.imapsPort}")
                            SettingLine(stringResource(R.string.mail_outgoing_smtp), "${settings.submissionHost}:${settings.submissionPort}")
                            if (settings.davHost.isNotEmpty()) SettingLine(stringResource(R.string.mail_contacts_and_calendar), settings.davHost)
                            SettingLine(stringResource(R.string.mail_user_name), mail.mailbox?.sendingAddresses?.firstOrNull() ?: "")
                        }
                    }
                }
            }
            failure != null -> ErrorState(failure?.message.orEmpty(), onRetry = { failure = null; scope.launch { load() } }, modifier = Modifier.padding(contentPadding))
            else -> LoadingState(Modifier.padding(contentPadding))
        }
    }
    if (isAdding) {
        NameDialog(stringResource(R.string.mail_new_app_password), stringResource(R.string.mail_device_name), newName, { typed -> newName = typed }, stringResource(R.string.mail_create), onDismiss = { isAdding = false }) {
            isAdding = false
            val name = newName.trim()
            if (mailboxId != null && name.isNotEmpty()) scope.launch {
                try {
                    created = mail.connection.createMailAppPassword(mailboxId, name)
                    load()
                } catch (failed: kotlinx.coroutines.CancellationException) {
                    throw failed
                } catch (failed: Throwable) {
                    mail.snackbar?.showError(failed)
                }
            }
        }
    }
    created?.let { shownPassword ->
        AlertDialog(
            onDismissRequest = {},
            title = { Text(stringResource(R.string.mail_app_password)) },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    com.teanode.app.ui.components.SecureWindow()
                    SettingLine(stringResource(R.string.mail_user_name), shownPassword.username)
                    SelectionContainer { SettingLine(stringResource(R.string.mail_password), shownPassword.password, isMonospace = true) }
                    TextButton(onClick = {
                        com.teanode.app.passwords.PasswordsClipboard.copy(context, shownPassword.password)
                        mail.snackbar?.show(context.getString(R.string.mail_copied))
                    }) { Text(stringResource(R.string.mail_copy_password)) }
                    Text(stringResource(R.string.mail_shown_this_once), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            },
            confirmButton = { TextButton(onClick = { created = null }) { Text(stringResource(R.string.common_done)) } },
        )
    }
    revoking?.let { password ->
        ConfirmationDialog(
            title = stringResource(R.string.mail_revoke_question, password.name), text = stringResource(R.string.mail_revoke_message), confirmLabel = stringResource(R.string.mail_revoke), isDestructive = true,
            onConfirm = {
                revoking = null
                scope.launch {
                    mail.act(context.getString(R.string.mail_revoked)) { mail.connection.deleteMailAppPassword(password.id) }
                    load()
                }
            },
            onDismiss = { revoking = null },
        )
    }
}

@Composable
private fun SettingLine(label: String, value: String, isMonospace: Boolean = false) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Text(label, Modifier.weight(0.45f), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyMedium)
        Text(value, Modifier.weight(0.55f), style = MaterialTheme.typography.bodyMedium, fontFamily = if (isMonospace) FontFamily.Monospace else null)
    }
}
