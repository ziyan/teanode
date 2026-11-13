package com.teanode.app.mail

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.MoveToInbox
import androidx.compose.material.icons.outlined.Newspaper
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Star
import androidx.compose.material.icons.outlined.AdminPanelSettings
import androidx.compose.material.icons.outlined.Rule
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.CountBadge
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.mail.MailFolder
import com.teanode.kit.mail.MailFolderRow
import com.teanode.kit.mail.MailFolders

/**
 * The first screen of Mail: the Inbox, Starred, Priority, the pinned folders, the rest of the tree and the lists, each
 * opening its conversations, with the mailbox picker when there is more than one.
 */
@Composable
fun MailSection() {
    val navigator = LocalAppNavigator.current
    val mail = rememberMailModel()
    MailboxesScreen(
        mail = mail,
        onOpenPlace = { place -> navigator.navigate(MailRoutes.place(place)) },
        onCompose = { navigator.navigate(MailRoutes.compose(ComposeKind.New)) },
        onOpenSettings = { navigator.navigate(MailRoutes.SETTINGS) },
        onOpenRules = { navigator.navigate(MailRoutes.RULES) },
        onOpenManagement = { navigator.navigate(MailRoutes.MANAGE) },
    )
}

@Composable
fun MailboxesScreen(
    mail: MailModel,
    onOpenPlace: (MailPlace) -> Unit,
    onCompose: () -> Unit,
    onOpenSettings: () -> Unit,
    onOpenRules: () -> Unit,
    onOpenManagement: () -> Unit,
) {
    LaunchedEffect(mail) { mail.load() }
    var isMailboxMenuOpen by remember { mutableStateOf(false) }
    var isMoreMenuOpen by remember { mutableStateOf(false) }
    TeaNodeScreen(
        title = stringResource(R.string.mail_title),
        actions = {
            if (mail.entries.size > 1) {
                Box {
                    IconButton(onClick = { isMailboxMenuOpen = true }) { Icon(Icons.Outlined.MoveToInbox, stringResource(R.string.mail_mailboxes)) }
                    DropdownMenu(expanded = isMailboxMenuOpen, onDismissRequest = { isMailboxMenuOpen = false }) {
                        for (entry in mail.entries) {
                            DropdownMenuItem(
                                text = { Text("${entry.mailbox?.name.orEmpty()} (${entry.unread})") },
                                leadingIcon = { RadioButton(selected = entry.mailbox?.id == mail.current?.mailbox?.id, onClick = null) },
                                onClick = { isMailboxMenuOpen = false; mail.chooseMailbox(entry.mailbox?.id) },
                            )
                        }
                    }
                }
            }
            Box {
                IconButton(onClick = { isMoreMenuOpen = true }) { Icon(Icons.Outlined.Settings, stringResource(R.string.mail_mailbox_settings_title)) }
                DropdownMenu(expanded = isMoreMenuOpen, onDismissRequest = { isMoreMenuOpen = false }) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.mail_mailbox_settings_title)) }, leadingIcon = { Icon(Icons.Outlined.Settings, null) }, enabled = mail.mailbox != null, onClick = { isMoreMenuOpen = false; onOpenSettings() })
                    DropdownMenuItem(text = { Text(stringResource(R.string.mail_rules_title)) }, leadingIcon = { Icon(Icons.Outlined.Rule, null) }, enabled = mail.mailbox != null, onClick = { isMoreMenuOpen = false; onOpenRules() })
                    DropdownMenuItem(text = { Text(stringResource(R.string.mail_manage_title)) }, leadingIcon = { Icon(Icons.Outlined.AdminPanelSettings, null) }, onClick = { isMoreMenuOpen = false; onOpenManagement() })
                }
            }
        },
        floatingActionButton = {
            ExtendedFloatingActionButton(onClick = { if (mail.mailbox != null) onCompose() }, icon = { Icon(Icons.Outlined.Edit, null) }, text = { Text(stringResource(R.string.mail_new_message)) })
        },
    ) { contentPadding ->
        val entry = mail.current
        val inbox = mail.folderOfKind("inbox")
        Box(Modifier.padding(contentPadding).fillMaxSize()) {
            when {
                mail.loadFailure != null && !mail.isLoaded -> ErrorState(mail.loadFailure?.message.orEmpty(), onRetry = { mail.reload() })
                !mail.isLoaded -> LoadingState()
                mail.entries.isEmpty() -> EmptyState(Icons.Outlined.Inbox, stringResource(R.string.mail_no_mailbox_yet), description = stringResource(R.string.mail_no_mailbox_description))
                entry != null && inbox != null -> LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 88.dp)) {
                    item { FolderLink(Icons.Outlined.Inbox, stringResource(R.string.mail_inbox), inbox.unread, 0) { onOpenPlace(MailPlace.Folder(inbox.id)) } }
                    for (row in childRows(mail, inbox.id)) {
                        item(key = "child-" + row.folder.id) { FolderLink(MailNames.icon(row.folder), row.folder.name, row.folder.unread, row.depth) { onOpenPlace(MailPlace.Folder(row.folder.id)) } }
                    }
                    item { FolderLink(Icons.Outlined.Star, stringResource(R.string.mail_starred), entry.starredUnread, 0) { onOpenPlace(MailPlace.Starred) } }
                    if (mail.mailbox?.agent?.isSorting == true) {
                        item { FolderLink(Icons.Outlined.ErrorOutline, stringResource(R.string.mail_priority), entry.priorityUnread, 0) { onOpenPlace(MailPlace.Priority) } }
                    }
                    val rows = mail.railRows
                    if (rows.pinned.isNotEmpty()) {
                        item { TeaNodeSectionHeader(stringResource(R.string.mail_pinned)) }
                        for (row in rows.pinned) item(key = "pinned-" + row.folder.id) { FolderRowLink(row, onOpenPlace) }
                    }
                    item { TeaNodeSectionHeader(stringResource(R.string.mail_folders)) }
                    for (row in rows.rest.filter { row -> row.folder.kind != "inbox" && !isUnderInbox(mail, row.folder) }) {
                        item(key = "folder-" + row.folder.id) { FolderRowLink(row, onOpenPlace) }
                    }
                    item { TeaNodeSectionHeader("") }
                    item { FolderLink(Icons.Outlined.Newspaper, stringResource(R.string.mail_subscriptions), 0, 0) { onOpenPlace(MailPlace.Subscriptions) } }
                }
            }
        }
    }
}

@Composable
private fun FolderRowLink(row: MailFolderRow, onOpenPlace: (MailPlace) -> Unit) {
    FolderLink(MailNames.icon(row.folder), MailNames.folder(row.folder), if (row.folder.kind == "archive") 0 else row.folder.unread, row.depth) { onOpenPlace(MailPlace.Folder(row.folder.id)) }
}

/** A row of the list of folders: its name, its symbol and its unread count, opening the place. */
@Composable
fun FolderLink(icon: androidx.compose.ui.graphics.vector.ImageVector, name: String, count: Long, depth: Int, onClick: () -> Unit) {
    TeaNodeRow(
        headline = name,
        modifier = Modifier.padding(start = (depth.coerceAtMost(3) * 16).dp),
        leadingIcon = icon,
        trailing = { if (count > 0) CountBadge(count.toInt()) },
        onClick = onClick,
    )
}

private fun childRows(mail: MailModel, folderId: String): List<MailFolderRow> {
    val rows = MailFolders.rows(mail.folders)
    val start = rows.indexOfFirst { row -> row.folder.id == folderId }
    if (start < 0) return emptyList()
    val children = mutableListOf<MailFolderRow>()
    var index = start + 1
    while (index < rows.size && rows[index].depth > rows[start].depth) {
        if (rows[index].folder.pinnedAt == null) children.add(rows[index])
        index += 1
    }
    return children
}

private fun isUnderInbox(mail: MailModel, folder: MailFolder): Boolean {
    var parentId = folder.parentId
    while (parentId.isNotEmpty()) {
        val parent = mail.folderById(parentId) ?: return false
        if (parent.kind == "inbox") return true
        parentId = parent.parentId
    }
    return false
}
