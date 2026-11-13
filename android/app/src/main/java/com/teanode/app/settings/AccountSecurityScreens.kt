package com.teanode.app.settings

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Apps
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.Devices
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.account.APIToken
import com.teanode.kit.account.APITokenChange
import com.teanode.kit.account.APITokenExpiry
import com.teanode.kit.account.APITokenLifetime
import com.teanode.kit.account.AccountSession
import com.teanode.kit.account.ConnectedApp
import com.teanode.kit.account.accountSessions
import com.teanode.kit.account.apiTokens
import com.teanode.kit.account.connectedApps
import com.teanode.kit.account.createApiToken
import com.teanode.kit.account.deleteApiToken
import com.teanode.kit.account.disconnectApp
import com.teanode.kit.account.renameConnectedApp
import com.teanode.kit.account.revokeAllSessions
import com.teanode.kit.account.revokeSession
import com.teanode.kit.account.updateApiToken
import com.teanode.kit.photos.ServerTime
import java.text.DateFormat
import java.time.Instant
import java.util.Date

/** A date and time as the person's locale writes them, short. */
fun shortDateTime(instant: Instant): String = DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(Date.from(instant))

private fun copyToClipboard(context: Context, text: String) {
    (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(ClipData.newPlainText("TeaNode", text))
}

/** The browsers signed in to the account, newest first, with the one reading the list at the top. */
@Composable
fun SessionsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { AccountSession.listed(connection.accountSessions(includeRevoked = true)) }
    var revoking by remember { mutableStateOf<AccountSession?>(null) }
    var isConfirmingAll by remember { mutableStateOf(false) }
    val signedOutText = stringResource(R.string.settings_sessions_signed_out)
    LoadedSettingsPage(stringResource(R.string.settings_sessions_title), onBack, loaded) { sessions ->
        item {
            if (sessions.size <= 1) FooterText(stringResource(R.string.settings_sessions_nothing_else))
            FooterText(stringResource(R.string.settings_sessions_footer))
        }
        items(sessions.size) { index ->
            val session = sessions[index]
            val tags = buildList {
                if (session.revoked != null) add(stringResource(R.string.settings_sessions_signed_out))
                session.impersonatorUsername?.let { operator -> add(stringResource(R.string.settings_sessions_impersonated, operator)) }
                if (session.current) add(stringResource(R.string.settings_sessions_this_browser))
            }
            val lastUsed = ServerTime.instant(session.lastUsed)?.let { instant -> stringResource(R.string.settings_last_used, relativeTimeText(instant.toEpochMilli())) }
                ?: stringResource(R.string.settings_sessions_not_used)
            TeaNodeRow(
                headline = session.browserName ?: stringResource(R.string.settings_sessions_unknown_browser),
                supporting = (tags + listOfNotNull(lastUsed, session.ip?.takeIf { address -> address.isNotEmpty() })).joinToString(" · "),
                supportingMaxLines = 3,
                trailing = if (session.canRevoke) ({ IconButton(onClick = { revoking = session }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_sessions_sign_out)) } }) else null,
            )
        }
        if (sessions.any { session -> session.canRevoke }) {
            item {
                TeaNodeRow(headline = stringResource(R.string.settings_sessions_sign_out_all), leadingIcon = Icons.Outlined.Delete, onClick = { isConfirmingAll = true })
                FooterText(stringResource(R.string.settings_sessions_sign_out_all_footer))
            }
        }
    }
    revoking?.let { session ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_sessions_sign_out_title), confirmLabel = stringResource(R.string.settings_sessions_sign_out), isDestructive = true,
            onConfirm = { revoking = null; actions.run(signedOutText, onSuccess = { loaded.refresh() }) { connection.revokeSession(session.id) } },
            onDismiss = { revoking = null },
        )
    }
    if (isConfirmingAll) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_sessions_sign_out_all), confirmLabel = stringResource(R.string.settings_sessions_sign_out_all), isDestructive = true,
            text = stringResource(R.string.settings_sessions_sign_out_all_detail),
            onConfirm = { isConfirmingAll = false; actions.run(signedOutText, onSuccess = { loaded.refresh() }) { connection.revokeAllSessions() } },
            onDismiss = { isConfirmingAll = false },
        )
    }
}

@Composable
private fun tokenName(token: APIToken): String = token.name.ifEmpty { stringResource(R.string.settings_tokens_unnamed) }

@Composable
private fun expiryText(expiry: APITokenExpiry): String = when (expiry) {
    APITokenExpiry.Never -> stringResource(R.string.settings_tokens_never_expires)
    APITokenExpiry.Expired -> stringResource(R.string.settings_tokens_expired)
    is APITokenExpiry.ExpiresAt -> stringResource(R.string.settings_tokens_expires, shortDateTime(expiry.instant))
}

@Composable
private fun lifetimeText(lifetime: APITokenLifetime): String = when (lifetime) {
    APITokenLifetime.ThirtyDays -> stringResource(R.string.settings_tokens_30_days)
    APITokenLifetime.NinetyDays -> stringResource(R.string.settings_tokens_90_days)
    APITokenLifetime.Year -> stringResource(R.string.settings_tokens_year)
    APITokenLifetime.Never -> stringResource(R.string.settings_tokens_never)
}

/** The person's API tokens: make, change and revoke. */
@Composable
fun TokensScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val context = LocalContext.current
    val loaded = rememberLoad { connection.apiTokens(includeRevoked = true) }
    var editing by remember { mutableStateOf<APIToken?>(null) }
    var revoking by remember { mutableStateOf<APIToken?>(null) }
    var isMaking by remember { mutableStateOf(false) }
    var issuedSecret by remember { mutableStateOf<String?>(null) }
    val changedText = stringResource(R.string.settings_tokens_changed)
    val revokedText = stringResource(R.string.settings_tokens_revoked)
    LoadedSettingsPage(
        stringResource(R.string.settings_tokens_title), onBack, loaded,
        floatingActionButton = { FloatingActionButton(onClick = { isMaking = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_tokens_new)) } },
        isEmpty = { tokens -> tokens.isEmpty() },
        empty = { EmptyState(Icons.Outlined.Terminal, stringResource(R.string.settings_tokens_none), description = stringResource(R.string.settings_tokens_none_detail)) },
    ) { tokens ->
        items(tokens.size) { index ->
            val token = tokens[index]
            val parts = buildList {
                if (token.revoked != null) add(stringResource(R.string.settings_tokens_revoked))
                add(expiryText(token.expiry()))
                ServerTime.instant(token.created)?.let { instant -> add(stringResource(R.string.settings_tokens_created, shortDateTime(instant))) }
                add(ServerTime.instant(token.lastUsed)?.let { instant -> stringResource(R.string.settings_last_used, relativeTimeText(instant.toEpochMilli())) } ?: stringResource(R.string.settings_never_used))
                add(stringResource(R.string.settings_tokens_acts_as, token.username))
            }
            TeaNodeRow(
                headline = tokenName(token), supporting = parts.joinToString(" · "), supportingMaxLines = 4, leadingIcon = Icons.Outlined.Terminal,
                trailing = if (token.revoked == null) ({
                    Column {
                        IconButton(onClick = { editing = token }) { Icon(Icons.Outlined.Edit, stringResource(R.string.settings_edit)) }
                    }
                }) else null,
                onClick = if (token.revoked == null) ({ revoking = token }) else null,
            )
        }
    }
    if (isMaking) {
        var name by remember { mutableStateOf("") }
        var lifetime by remember { mutableStateOf(APITokenLifetime.standard) }
        FormDialog(
            title = stringResource(R.string.settings_tokens_new), confirmLabel = stringResource(R.string.settings_create), isConfirmEnabled = name.isNotBlank(), isWorking = actions.isWorking,
            onConfirm = {
                actions.run(onSuccess = { isMaking = false; loaded.refresh() }) {
                    issuedSecret = connection.createApiToken(name.trim(), lifetime).secret
                }
            },
            onDismiss = { isMaking = false },
        ) {
            FormField(name, { name = it }, stringResource(R.string.settings_tokens_name), placeholder = "laptop")
            ChoiceRow(stringResource(R.string.settings_tokens_expires_after), APITokenLifetime.entries, lifetime, label = { choice -> lifetimeText(choice) }, onSelect = { lifetime = it })
        }
    }
    issuedSecret?.let { secret ->
        val lines = "export TEANODE_URL=${connection.account.server.origin.toString().trimEnd('/')}\nexport TEANODE_TOKEN=$secret"
        FormDialog(
            title = stringResource(R.string.settings_tokens_new), confirmLabel = stringResource(R.string.common_done), isConfirmEnabled = true, onConfirm = { issuedSecret = null }, onDismiss = { issuedSecret = null },
        ) {
            Text(secret, style = MaterialTheme.typography.bodyLarge, fontFamily = FontFamily.Monospace)
            TeaNodeRow(headline = stringResource(R.string.settings_tokens_copy), leadingIcon = Icons.Outlined.ContentCopy, onClick = { copyToClipboard(context, secret) })
            Text(stringResource(R.string.settings_tokens_secret_footer), style = MaterialTheme.typography.bodySmall)
            Text(stringResource(R.string.settings_tokens_use_from), style = MaterialTheme.typography.titleSmall)
            Text(lines, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
            TeaNodeRow(headline = stringResource(R.string.settings_copy), leadingIcon = Icons.Outlined.ContentCopy, onClick = { copyToClipboard(context, lines) })
        }
    }
    editing?.let { token ->
        var name by remember(token.id) { mutableStateOf(token.name) }
        var lifetime by remember(token.id) { mutableStateOf<APITokenLifetime?>(null) }
        val change = APITokenChange.of(token, name, lifetime)
        FormDialog(
            title = stringResource(R.string.settings_tokens_change_title), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = change.hasChanges && name.isNotBlank(), isWorking = actions.isWorking,
            onConfirm = { actions.run(changedText, onSuccess = { editing = null; loaded.refresh() }) { connection.updateApiToken(change) } },
            onDismiss = { editing = null },
        ) {
            FormField(name, { name = it }, stringResource(R.string.settings_tokens_name))
            val keep = stringResource(R.string.settings_tokens_keep, expiryText(token.expiry()))
            ChoiceRow(
                stringResource(R.string.settings_tokens_expires_after), listOf<APITokenLifetime?>(null) + APITokenLifetime.entries, lifetime,
                label = { choice -> if (choice == null) keep else lifetimeFromNowText(choice) }, onSelect = { lifetime = it },
            )
            Text(stringResource(R.string.settings_tokens_change_footer), style = MaterialTheme.typography.bodySmall)
            TeaNodeRow(headline = stringResource(R.string.settings_tokens_revoke), leadingIcon = Icons.Outlined.Delete, onClick = { editing = null; revoking = token })
        }
    }
    revoking?.let { token ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_tokens_revoke), confirmLabel = stringResource(R.string.settings_tokens_revoke), isDestructive = true,
            text = stringResource(R.string.settings_tokens_revoke_detail, tokenName(token)),
            onConfirm = { revoking = null; actions.run(revokedText, onSuccess = { loaded.refresh() }) { connection.deleteApiToken(token.id) } },
            onDismiss = { revoking = null },
        )
    }
}

@Composable
private fun lifetimeFromNowText(lifetime: APITokenLifetime): String = when (lifetime) {
    APITokenLifetime.ThirtyDays -> stringResource(R.string.settings_tokens_30_days_from_now)
    APITokenLifetime.NinetyDays -> stringResource(R.string.settings_tokens_90_days_from_now)
    APITokenLifetime.Year -> stringResource(R.string.settings_tokens_year_from_now)
    APITokenLifetime.Never -> stringResource(R.string.settings_tokens_never)
}

/** The apps the person authorized to act as them; rename and disconnect. */
@Composable
fun ConnectedAppsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.connectedApps() }
    var renaming by remember { mutableStateOf<ConnectedApp?>(null) }
    var disconnecting by remember { mutableStateOf<ConnectedApp?>(null) }
    val renamedText = stringResource(R.string.settings_apps_renamed)
    val disconnectedText = stringResource(R.string.settings_apps_disconnected)
    val thisClientId = connection.account.clientId
    LoadedSettingsPage(
        stringResource(R.string.settings_apps_title), onBack, loaded,
        isEmpty = { apps -> apps.isEmpty() }, empty = { EmptyState(Icons.Outlined.Apps, stringResource(R.string.settings_apps_none)) },
    ) { apps ->
        items(apps.size) { index ->
            val app = apps[index]
            val lastUsed = ServerTime.instant(app.lastUsed)?.let { instant -> stringResource(R.string.settings_last_used, relativeTimeText(instant.toEpochMilli())) } ?: stringResource(R.string.settings_apps_not_used)
            val parts = buildList {
                if (app.clientId == thisClientId) add(stringResource(R.string.settings_apps_this_phone))
                ServerTime.instant(app.renewableUntil)?.let { instant -> add(stringResource(R.string.settings_apps_renews_until, shortDateTime(instant))) }
                app.differentRegisteredName?.let { registered -> add(stringResource(R.string.settings_apps_registered_as, registered)) }
                if (app.redirectHosts.isNotEmpty()) add(stringResource(R.string.settings_apps_returns_to, app.redirectHosts.joinToString(", ")))
                add("$lastUsed · ${stringResource(R.string.settings_apps_authorized, app.tokenCount)}")
            }
            TeaNodeRow(
                headline = app.displayName ?: stringResource(R.string.settings_apps_no_name), supporting = parts.joinToString("\n"), supportingMaxLines = 6, leadingIcon = Icons.Outlined.Apps,
                trailing = {
                    Column {
                        IconButton(onClick = { renaming = app }) { Icon(Icons.Outlined.Edit, stringResource(R.string.settings_apps_rename)) }
                        IconButton(onClick = { disconnecting = app }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_apps_disconnect)) }
                    }
                },
            )
        }
        item { FooterText(stringResource(R.string.settings_apps_footer)) }
    }
    renaming?.let { app ->
        var name by remember(app.clientId) { mutableStateOf(app.name) }
        FormDialog(
            title = stringResource(R.string.settings_apps_rename), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = name.isNotBlank(), isWorking = actions.isWorking,
            onConfirm = { actions.run(renamedText, onSuccess = { renaming = null; loaded.refresh() }) { connection.renameConnectedApp(app.clientId, name.trim()) } },
            onDismiss = { renaming = null },
        ) {
            FormField(name, { name = it }, stringResource(R.string.settings_apps_name))
            Text(stringResource(R.string.settings_apps_rename_footer), style = MaterialTheme.typography.bodySmall)
        }
    }
    disconnecting?.let { app ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_apps_disconnect_title), confirmLabel = stringResource(R.string.settings_apps_disconnect), isDestructive = true,
            text = if (app.clientId == thisClientId) stringResource(R.string.settings_apps_disconnect_this_phone) else stringResource(R.string.settings_apps_disconnect_detail, app.displayName ?: stringResource(R.string.settings_apps_no_name)),
            onConfirm = { disconnecting = null; actions.run(disconnectedText, onSuccess = { loaded.refresh() }) { connection.disconnectApp(app.clientId) } },
            onDismiss = { disconnecting = null },
        )
    }
}
