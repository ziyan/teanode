package com.teanode.app.settings

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.services.CreatedSyncCredential
import com.teanode.kit.services.ServiceSelection
import com.teanode.kit.services.SyncCredential
import com.teanode.kit.services.createSyncCredential
import com.teanode.kit.services.revokeSyncCredential
import com.teanode.kit.services.serviceSelection
import com.teanode.kit.services.syncCredentials
import com.teanode.kit.services.updateServices

/** Which services of the server are on. Turning one off keeps its data and pauses its pending work. */
@Composable
fun ServerServicesScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.serviceSelection() }
    var selection by remember(loaded.value) { mutableStateOf(loaded.value) }
    var isReviewing by remember { mutableStateOf(false) }
    val appliedText = stringResource(R.string.settings_services_applied)
    LoadedSettingsPage(stringResource(R.string.settings_services_title), onBack, loaded) { _ ->
        val current = selection ?: return@LoadedSettingsPage
        item {
            TeaNodeSectionHeader(stringResource(R.string.settings_services_title))
            SwitchRow(stringResource(R.string.settings_services_mail_routing), current.isMailRoutingEnabled, { isOn -> selection = current.copy(isMailRoutingEnabled = isOn, isMailboxEnabled = current.isMailboxEnabled && isOn) })
            SwitchRow(stringResource(R.string.settings_services_mailboxes), current.isMailboxEnabled, { isOn -> selection = current.copy(isMailboxEnabled = isOn) }, isEnabled = current.isMailRoutingEnabled)
            SwitchRow(stringResource(R.string.section_calendar), current.isCalendarEnabled, { isOn -> selection = current.copy(isCalendarEnabled = isOn) })
            SwitchRow(stringResource(R.string.settings_services_reminders), current.isRemindersEnabled, { isOn -> selection = current.copy(isRemindersEnabled = isOn) })
            SwitchRow(stringResource(R.string.section_contacts), current.isContactsEnabled, { isOn -> selection = current.copy(isContactsEnabled = isOn) })
            SwitchRow(stringResource(R.string.section_photos), current.isPhotosEnabled, { isOn -> selection = current.copy(isPhotosEnabled = isOn) })
            TeaNodeRow(headline = stringResource(R.string.settings_services_review), onClick = if (actions.isWorking) null else ({ isReviewing = true }))
            FooterText(stringResource(R.string.settings_services_footer))
        }
    }
    val reviewed = selection
    if (isReviewing && reviewed != null) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_services_apply_title), confirmLabel = stringResource(R.string.settings_services_apply),
            onConfirm = { isReviewing = false; actions.run(appliedText, onSuccess = { session.refreshFacts() }) { connection.updateServices(reviewed) } }, onDismiss = { isReviewing = false },
        )
    }
}

private fun copyText(context: Context, text: String) {
    (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("TeaNode", text))
}

/** Passwords a calendar, reminders or contacts app signs in with, one per device. */
@Composable
fun SyncCredentialsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val facts by LocalAppSession.current.facts.collectAsState()
    val context = LocalContext.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.syncCredentials().sortedByDescending { credential -> credential.createdAt.orEmpty() } }
    var name by remember { mutableStateOf("") }
    var hasCalendar by remember { mutableStateOf(false) }
    var hasReminders by remember { mutableStateOf(false) }
    var hasContacts by remember { mutableStateOf(false) }
    var created by remember { mutableStateOf<CreatedSyncCredential?>(null) }
    var revoking by remember { mutableStateOf<SyncCredential?>(null) }
    val revokedText = stringResource(R.string.settings_sync_revoked)
    val services = facts.services
    LoadedSettingsPage(
        stringResource(R.string.settings_sync_title), onBack, loaded,
    ) { credentials ->
        item {
            TeaNodeSectionHeader(stringResource(R.string.settings_sync_manual))
            FormField(name, { name = it }, stringResource(R.string.settings_sync_device_name))
            if (services?.isCalendarEnabled == true) SwitchRow(stringResource(R.string.section_calendar), hasCalendar, { hasCalendar = it })
            if (services?.isRemindersEnabled == true) SwitchRow(stringResource(R.string.settings_services_reminders), hasReminders, { hasReminders = it })
            if (services?.isContactsEnabled == true) SwitchRow(stringResource(R.string.section_contacts), hasContacts, { hasContacts = it })
            TeaNodeRow(
                headline = stringResource(R.string.settings_sync_create), leadingIcon = Icons.Outlined.Sync,
                onClick = if (actions.isWorking || name.isBlank() || !(hasCalendar || hasReminders || hasContacts)) null else ({
                    actions.run(onSuccess = { loaded.refresh() }) { created = connection.createSyncCredential(name.trim(), hasCalendar, hasReminders, hasContacts) }
                }),
            )
        }
        created?.let { made ->
            item {
                TeaNodeSectionHeader(stringResource(R.string.settings_sync_save_now))
                Text(made.username, modifier = Modifier.padding(horizontal = 16.dp), fontFamily = FontFamily.Monospace)
                Text(made.password, modifier = Modifier.padding(horizontal = 16.dp), fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyLarge)
                Text(made.principalPath, modifier = Modifier.padding(horizontal = 16.dp), fontFamily = FontFamily.Monospace)
                TeaNodeRow(headline = stringResource(R.string.settings_copy), leadingIcon = Icons.Outlined.ContentCopy, onClick = { copyText(context, made.password) })
            }
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_sync_devices)) }
        if (credentials.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_sync_none)) }
        items(credentials.size) { index ->
            val credential = credentials[index]
            val scopes = listOfNotNull(
                if (credential.hasCalendarAccess) stringResource(R.string.section_calendar) else null,
                if (credential.hasRemindersAccess) stringResource(R.string.settings_services_reminders) else null,
                if (credential.hasContactsAccess) stringResource(R.string.section_contacts) else null,
            ).joinToString(", ")
            val lastUsed = ServerTime.instant(credential.lastUsedAt)?.let { instant -> stringResource(R.string.settings_last_used, com.teanode.app.ui.components.relativeTimeText(instant.toEpochMilli())) } ?: stringResource(R.string.settings_never_used)
            TeaNodeRow(
                headline = credential.credentialName, supporting = "$scopes · $lastUsed",
                trailing = { IconButton(onClick = { revoking = credential }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_tokens_revoke)) } },
            )
        }
    }
    revoking?.let { credential ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_sync_revoke_title), confirmLabel = stringResource(R.string.settings_tokens_revoke), isDestructive = true,
            onConfirm = { revoking = null; actions.run(revokedText, onSuccess = { loaded.refresh() }) { connection.revokeSyncCredential(credential.id) } }, onDismiss = { revoking = null },
        )
    }
}
