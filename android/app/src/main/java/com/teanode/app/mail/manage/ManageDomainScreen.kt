package com.teanode.app.mail.manage

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.mail.ChipRow
import com.teanode.app.mail.NameDialog
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LoadState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.Connection
import com.teanode.kit.mail.CreatedDomainCredential
import com.teanode.kit.mail.DNSRecord
import com.teanode.kit.mail.DomainAlias
import com.teanode.kit.mail.DomainCredential
import com.teanode.kit.mail.DomainOverview
import com.teanode.kit.mail.DomainTemplates
import com.teanode.kit.mail.ManagedDomain
import com.teanode.kit.mail.ManagedTone
import com.teanode.kit.mail.ManagedWords
import com.teanode.kit.mail.ServerMailbox
import com.teanode.kit.mail.checkDomain
import com.teanode.kit.mail.createDomainAlias
import com.teanode.kit.mail.createDomainCredential
import com.teanode.kit.mail.createManagedDomain
import com.teanode.kit.mail.deleteDomainAlias
import com.teanode.kit.mail.deleteDomainCredential
import com.teanode.kit.mail.domainOverview
import com.teanode.kit.mail.domainTemplates
import com.teanode.kit.mail.managedDomain
import com.teanode.kit.mail.regenerateDomainKey
import com.teanode.kit.mail.serverMailboxes
import com.teanode.kit.mail.updateDomainAlias
import com.teanode.kit.mail.updateDomainDkimSelector
import com.teanode.kit.mail.updateDomainLinkHost
import com.teanode.kit.mail.updateDomainMailServers
import kotlinx.coroutines.launch

/** What is loaded for the page of one domain. */
class DomainPage(val domain: ManagedDomain, val overview: DomainOverview?, val templates: DomainTemplates?, val mailboxes: List<ServerMailbox>)

/** One domain: how much mail it has had, its DNS records, where its mail goes, who may send as it, its templates, and its settings. */
@Composable
fun DomainScreen(domainId: String, onBack: () -> Unit, connection: Connection = LocalConnection.current) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val page = rememberLoad(domainId) {
        val domain = connection.managedDomain(domainId) ?: throw com.teanode.kit.TeaNodeError.Refused("api: not found")
        DomainPage(domain, runCatching { connection.domainOverview(domainId) }.getOrNull(), runCatching { connection.domainTemplates(domainId) }.getOrNull(), runCatching { connection.serverMailboxes() }.getOrDefault(emptyList()))
    }
    var editedAlias by remember { mutableStateOf<DomainAlias?>(null) }
    var isAddingAlias by remember { mutableStateOf(false) }
    var removingAlias by remember { mutableStateOf<DomainAlias?>(null) }
    var isAddingCredential by remember { mutableStateOf(false) }
    var newCredentialComment by remember { mutableStateOf("") }
    var createdCredential by remember { mutableStateOf<CreatedDomainCredential?>(null) }
    var removingCredential by remember { mutableStateOf<DomainCredential?>(null) }
    var editing by remember { mutableStateOf<String?>(null) }
    var editedText by remember { mutableStateOf("") }
    var isConfirmingKey by remember { mutableStateOf(false) }

    fun run(said: String?, work: suspend () -> Unit) {
        scope.launch {
            try {
                work()
                if (said != null) snackbar.show(said)
                page.refresh()
            } catch (failed: kotlinx.coroutines.CancellationException) {
                throw failed
            } catch (failed: Throwable) {
                snackbar.showError(failed)
            }
        }
    }

    TeaNodeScreen(
        title = page.value?.domain?.domainName ?: stringResource(R.string.mail_manage_domain),
        onBack = onBack,
        actions = { IconButton(onClick = { run(context.getString(R.string.mail_manage_dns_checked)) { connection.checkDomain(domainId) } }) { Icon(Icons.Outlined.Refresh, stringResource(R.string.mail_manage_check_dns)) } },
    ) { contentPadding ->
        LoadedContent(page, modifier = Modifier.padding(contentPadding)) { loaded: DomainPage ->
            val domain = loaded.domain
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) { DnsSummaryTag(domain.dnsSummary) }
                val overview = loaded.overview
                if (overview != null) {
                    TeaNodeSectionHeader(stringResource(R.string.mail_manage_overview))
                    DetailRow(stringResource(R.string.mail_manage_messages), stringResource(R.string.mail_manage_accepted_rejected, overview.totalMailCount, overview.acceptedMailCount, overview.rejectedMailCount))
                    DetailRow(stringResource(R.string.mail_manage_newest_message), ManageLabels.relativeTime(overview.newestReceivedAt))
                    if (overview.pendingDeliveries.isNotEmpty()) DetailRow(stringResource(R.string.mail_manage_waiting_deliveries), stringResource(R.string.mail_manage_waiting_failing, overview.pendingDeliveries.size, overview.failingDeliveryCount))
                }
                TeaNodeSectionHeader(stringResource(R.string.mail_manage_dns_records))
                val records = domain.dnsCheck?.records ?: emptyList()
                if (records.isEmpty()) Text(stringResource(R.string.mail_manage_dns_not_checked), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                for (record in records) DnsRecordRow(record)
                domain.dnsCheck?.errorMessage?.takeIf { text -> text.isNotEmpty() }?.let { text -> Text(text, Modifier.padding(16.dp), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
                TeaNodeSectionHeader(stringResource(R.string.mail_manage_settings))
                TeaNodeRow(headline = stringResource(R.string.mail_manage_mail_servers), supporting = (domain.mailServers ?: emptyList()).joinToString(", ").ifEmpty { stringResource(R.string.mail_manage_none_set) }, onClick = { editing = "servers"; editedText = (domain.mailServers ?: emptyList()).joinToString(", ") })
                TeaNodeRow(headline = stringResource(R.string.mail_manage_link_host), supporting = (domain.linkHost ?: domain.linkHostname).orEmpty().ifEmpty { stringResource(R.string.mail_manage_none_set) }, onClick = { editing = "link"; editedText = domain.linkHost.orEmpty() })
                TeaNodeRow(headline = stringResource(R.string.mail_manage_dkim_selector), supporting = domain.dkimSelector.orEmpty().ifEmpty { stringResource(R.string.mail_manage_none_set) }, onClick = { editing = "selector"; editedText = domain.dkimSelector.orEmpty() })
                TeaNodeRow(headline = stringResource(R.string.mail_manage_regenerate_key), supporting = stringResource(if (domain.hasDkimKey == true) R.string.mail_manage_has_key else R.string.mail_manage_no_key), onClick = { isConfirmingKey = true })
                TeaNodeSectionHeader(stringResource(R.string.mail_manage_aliases))
                val aliases = domain.aliases ?: emptyList()
                if (aliases.isEmpty()) Text(stringResource(R.string.mail_manage_no_aliases), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                for (alias in aliases) {
                    TeaNodeRow(
                        headline = alias.pattern.orEmpty(), supporting = aliasDestination(alias, loaded.mailboxes) + if (alias.isDisabled == true) " · " + stringResource(R.string.mail_off) else "",
                        onClick = { editedAlias = alias }, trailing = { IconButton(onClick = { removingAlias = alias }) { Icon(Icons.Outlined.Delete, stringResource(R.string.mail_remove), tint = MaterialTheme.colorScheme.error) } },
                    )
                }
                OutlinedButton(onClick = { isAddingAlias = true }, Modifier.padding(16.dp)) { Icon(Icons.Outlined.Add, null); Text(stringResource(R.string.mail_manage_add_alias), Modifier.padding(start = 8.dp)) }
                TeaNodeSectionHeader(stringResource(R.string.mail_manage_credentials))
                val credentials = domain.credentials ?: emptyList()
                if (credentials.isEmpty()) Text(stringResource(R.string.mail_manage_no_credentials), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                for (credential in credentials) {
                    TeaNodeRow(
                        headline = credential.comment?.ifEmpty { null } ?: stringResource(R.string.mail_manage_unnamed_credential), supporting = credential.alias?.takeIf { alias -> alias.isNotEmpty() }?.let { alias -> stringResource(R.string.mail_manage_only_as, alias) },
                        trailing = { IconButton(onClick = { removingCredential = credential }) { Icon(Icons.Outlined.Delete, stringResource(R.string.mail_remove), tint = MaterialTheme.colorScheme.error) } },
                    )
                }
                OutlinedButton(onClick = { newCredentialComment = ""; isAddingCredential = true }, Modifier.padding(16.dp)) { Icon(Icons.Outlined.Add, null); Text(stringResource(R.string.mail_manage_add_credential), Modifier.padding(start = 8.dp)) }
                val templates = loaded.templates
                if (templates != null) {
                    TeaNodeSectionHeader(stringResource(R.string.mail_manage_templates))
                    if (templates.templates.isEmpty() && templates.layouts.isEmpty()) Text(stringResource(R.string.mail_manage_no_templates), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    for (template in templates.templates) TeaNodeRow(headline = template.name, supporting = template.locales.joinToString(", ").ifEmpty { null })
                    for (layout in templates.layouts) TeaNodeRow(headline = layout.comment?.ifEmpty { null } ?: stringResource(R.string.mail_manage_layout), supporting = layout.locales.joinToString(", ").ifEmpty { null })
                }
            }
            if (isAddingAlias || editedAlias != null) {
                AliasDialog(
                    editedAlias, loaded.mailboxes, onDismiss = { isAddingAlias = false; editedAlias = null },
                    onSave = { pattern, comment, kind, destination, mailboxId, isDisabled ->
                        val existing = editedAlias
                        isAddingAlias = false
                        editedAlias = null
                        run(context.getString(R.string.mail_saved)) {
                            if (existing == null) connection.createDomainAlias(domainId, pattern, comment, kind, destination, mailboxId)
                            else connection.updateDomainAlias(existing.id, pattern, comment, kind, destination, mailboxId, isDisabled)
                        }
                    },
                )
            }
        }
    }

    removingAlias?.let { alias ->
        ConfirmationDialog(
            title = stringResource(R.string.mail_manage_remove_alias_question, alias.pattern.orEmpty()), text = stringResource(R.string.mail_manage_remove_alias_message), confirmLabel = stringResource(R.string.mail_remove), isDestructive = true,
            onConfirm = { removingAlias = null; run(context.getString(R.string.mail_manage_alias_removed)) { connection.deleteDomainAlias(alias.id) } }, onDismiss = { removingAlias = null },
        )
    }
    removingCredential?.let { credential ->
        ConfirmationDialog(
            title = stringResource(R.string.mail_manage_remove_credential_question), text = stringResource(R.string.mail_manage_remove_credential_message), confirmLabel = stringResource(R.string.mail_remove), isDestructive = true,
            onConfirm = { removingCredential = null; run(context.getString(R.string.mail_manage_credential_removed)) { connection.deleteDomainCredential(credential.id) } }, onDismiss = { removingCredential = null },
        )
    }
    if (isAddingCredential) {
        NameDialog(stringResource(R.string.mail_manage_add_credential), stringResource(R.string.mail_manage_comment), newCredentialComment, { typed -> newCredentialComment = typed }, stringResource(R.string.mail_create), onDismiss = { isAddingCredential = false }) {
            isAddingCredential = false
            val comment = newCredentialComment.trim()
            scope.launch {
                try {
                    createdCredential = connection.createDomainCredential(domainId, comment)
                    page.refresh()
                } catch (failed: kotlinx.coroutines.CancellationException) {
                    throw failed
                } catch (failed: Throwable) {
                    snackbar.showError(failed)
                }
            }
        }
    }
    createdCredential?.let { created ->
        AlertDialog(
            onDismissRequest = {}, title = { Text(stringResource(R.string.mail_manage_new_credential)) },
            text = {
                SelectionContainer {
                    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(stringResource(R.string.mail_user_name) + ": " + created.username, fontFamily = FontFamily.Monospace)
                        Text(stringResource(R.string.mail_password) + ": " + created.password, fontFamily = FontFamily.Monospace)
                        Text(stringResource(R.string.mail_manage_server) + ": " + created.host + ":" + created.port, fontFamily = FontFamily.Monospace)
                        Text(stringResource(R.string.mail_shown_this_once), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            },
            confirmButton = { TextButton(onClick = { createdCredential = null }) { Text(stringResource(R.string.common_done)) } },
        )
    }
    editing?.let { which ->
        NameDialog(
            stringResource(when (which) { "servers" -> R.string.mail_manage_mail_servers; "link" -> R.string.mail_manage_link_host; else -> R.string.mail_manage_dkim_selector }),
            stringResource(if (which == "servers") R.string.mail_manage_comma_separated else R.string.mail_name), editedText, { typed -> editedText = typed }, stringResource(R.string.mail_save), onDismiss = { editing = null },
        ) {
            editing = null
            val typed = editedText.trim()
            run(context.getString(R.string.mail_saved)) {
                when (which) {
                    "servers" -> connection.updateDomainMailServers(domainId, ManagedWords.mailServerNames(typed))
                    "link" -> connection.updateDomainLinkHost(domainId, typed)
                    else -> connection.updateDomainDkimSelector(domainId, typed)
                }
            }
        }
    }
    if (isConfirmingKey) {
        ConfirmationDialog(
            title = stringResource(R.string.mail_manage_regenerate_key_question), text = stringResource(R.string.mail_manage_regenerate_key_message), confirmLabel = stringResource(R.string.mail_manage_regenerate), isDestructive = true,
            onConfirm = { isConfirmingKey = false; run(context.getString(R.string.mail_manage_key_regenerated)) { connection.regenerateDomainKey(domainId) } }, onDismiss = { isConfirmingKey = false },
        )
    }
}

@Composable
private fun aliasDestination(alias: DomainAlias, mailboxes: List<ServerMailbox>): String = when (alias.aliasKind) {
    "mailbox" -> mailboxes.firstOrNull { mailbox -> mailbox.id == alias.mailboxId }?.rowText ?: stringResource(R.string.mail_manage_alias_mailbox)
    "email" -> alias.email.orEmpty()
    "webhook" -> alias.webhook.orEmpty()
    "null" -> stringResource(R.string.mail_manage_alias_nowhere)
    "mailServer" -> alias.mailServer?.let { server -> "${server.host.orEmpty()}:${server.port ?: ""}" }.orEmpty()
    else -> alias.aliasKind.orEmpty()
}

/** One DNS record a domain should publish: what, where, what it should say, and whether it is there. */
@Composable
fun DnsRecordRow(record: DNSRecord) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("${record.recordType.orEmpty()}  ${record.name.orEmpty()}", Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            ManageTag(
                stringResource(if (record.isVerified == true) R.string.mail_manage_dns_published else if (record.isOptional == true) R.string.mail_manage_dns_optional else R.string.mail_manage_dns_missing_record),
                if (record.isVerified == true) ManagedTone.Good else if (record.isOptional == true) ManagedTone.Plain else ManagedTone.Warn,
            )
        }
        SelectionContainer { Text(record.publishedValue, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        record.purpose?.takeIf { purpose -> purpose.isNotEmpty() }?.let { purpose -> Text(purpose, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        record.blockedReason?.takeIf { reason -> reason.isNotEmpty() }?.let { reason -> Text(reason, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
    }
    HorizontalDivider()
}

/** The form of one alias: its pattern, where its mail goes, and whether it is on. */
@Composable
private fun AliasDialog(
    existing: DomainAlias?,
    mailboxes: List<ServerMailbox>,
    onDismiss: () -> Unit,
    onSave: (pattern: String, comment: String, kind: String, destination: String, mailboxId: String, isDisabled: Boolean) -> Unit,
) {
    var pattern by remember(existing) { mutableStateOf(existing?.pattern.orEmpty()) }
    var comment by remember(existing) { mutableStateOf(existing?.comment.orEmpty()) }
    var kind by remember(existing) { mutableStateOf(existing?.editorKind?.ifEmpty { null } ?: "mailbox") }
    var destination by remember(existing) { mutableStateOf(existing?.editableDestination(kind).orEmpty()) }
    var mailboxId by remember(existing) { mutableStateOf(existing?.mailboxId.orEmpty()) }
    var isDisabled by remember(existing) { mutableStateOf(existing?.isDisabled == true) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(if (existing == null) R.string.mail_manage_add_alias else R.string.mail_manage_edit_alias)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(pattern, { typed -> pattern = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_manage_alias_pattern)) }, singleLine = true)
                OutlinedTextField(comment, { typed -> comment = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_manage_comment)) }, singleLine = true)
                ChipRow {
                    for (choice in (existing?.selectableKinds ?: DomainAlias.creatableKinds)) {
                        FilterChip(kind == choice, { kind = choice }, { Text(stringResource(when (choice) { "mailbox" -> R.string.mail_manage_alias_mailbox; "email" -> R.string.mail_manage_alias_email; "webhook" -> R.string.mail_manage_alias_webhook; "null" -> R.string.mail_manage_alias_nowhere; else -> R.string.mail_manage_alias_mail_server })) })
                    }
                }
                when (kind) {
                    "email", "webhook" -> OutlinedTextField(destination, { typed -> destination = typed }, Modifier.fillMaxWidth(), label = { Text(stringResource(if (kind == "email") R.string.mail_manage_alias_email else R.string.mail_manage_alias_webhook)) }, singleLine = true)
                    "mailbox" -> ChipRow {
                        for (mailbox in mailboxes) FilterChip(mailboxId == mailbox.id, { mailboxId = mailbox.id }, { Text(mailbox.rowText) })
                    }
                }
                if (existing != null) Row(verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.mail_manage_disabled), Modifier.weight(1f)); Switch(isDisabled, { checked -> isDisabled = checked }) }
            }
        },
        confirmButton = {
            TextButton(enabled = pattern.isNotBlank() && DomainAlias.isDestinationFilled(kind, destination, mailboxId), onClick = { onSave(pattern.trim(), comment.trim(), kind, destination.trim(), mailboxId, isDisabled) }) { Text(stringResource(R.string.mail_save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The button of the domains list that adds one: a dialog for its name. */
@Composable
fun NewDomainDialog(onDismiss: () -> Unit, onCreated: (String) -> Unit, connection: Connection = LocalConnection.current) {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var name by remember { mutableStateOf("") }
    NameDialog(stringResource(R.string.mail_manage_new_domain), stringResource(R.string.mail_manage_domain), name, { typed -> name = typed }, stringResource(R.string.mail_create), onDismiss = onDismiss) {
        scope.launch {
            try {
                onCreated(connection.createManagedDomain(name.trim()))
            } catch (failed: kotlinx.coroutines.CancellationException) {
                throw failed
            } catch (failed: Throwable) {
                snackbar.showError(failed)
            }
        }
    }
}
