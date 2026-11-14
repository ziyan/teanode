package com.teanode.app.passwords

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Undo
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DeleteForever
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
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
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.passwords.PasswordItem
import com.teanode.kit.passwords.PasswordItemVersion
import com.teanode.kit.passwords.PasswordsCardFields
import com.teanode.kit.passwords.PasswordsFormat
import com.teanode.kit.passwords.PasswordsItemFields
import com.teanode.kit.passwords.PasswordsItemKind
import com.teanode.kit.passwords.PasswordsLoginFields
import com.teanode.kit.passwords.PasswordsNoteFields
import com.teanode.kit.passwords.PasswordsOneTimeCode
import com.teanode.kit.passwords.PasswordsPasskeyFields
import com.teanode.kit.passwords.PasswordsSharingCheck
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.text.DateFormat
import java.util.Date

/** One item: its fields (secrets hidden until shown, a tap copies), the one-time code counting down, passkeys, sites that open in the browser, and for the owner: the agent, versions and deleting. */
@Composable
fun PasswordsItemScreen(itemId: String) {
    val model = rememberPasswordsModel()
    val navigator = LocalAppNavigator.current
    GoBackWhenClosed(model)
    val item = model.item(itemId)
    var isConfirmingDelete by remember { mutableStateOf(false) }
    var isConfirmingDeleteForever by remember { mutableStateOf(false) }
    var isWorking by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    LaunchedEffect(itemId) { model.noteUse() }

    TeaNodeScreen(
        title = item?.name.orEmpty(), onBack = { navigator.back() },
        actions = {
            if (item != null && !item.isSharedWithMe && !item.isDeleted) {
                TextButton(onClick = { navigator.navigate("passwords/edit/${item.itemKind.rawValue}/${Uri.encode(item.id)}") }) { Text(stringResource(R.string.passwords_edit)) }
            }
        },
    ) { padding ->
        if (item == null) {
            Box(Modifier.padding(padding)) {
                EmptyState(Icons.Outlined.Key, stringResource(R.string.passwords_item_gone_title), description = stringResource(R.string.passwords_item_gone_description))
            }
        } else {
            fun run(failure: Int, work: suspend () -> Unit) {
                isWorking = true
                scope.launch {
                    try {
                        work()
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (error: Throwable) {
                        model.report(error, context.getString(failure))
                    } finally {
                        isWorking = false
                    }
                }
            }
            Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
                PasswordsFieldSections(model, item.fields)
                when {
                    item.isDeleted -> {
                        ActionRow(Icons.AutoMirrored.Outlined.Undo, stringResource(R.string.passwords_restore), enabled = !isWorking) {
                            run(R.string.passwords_restored_failed) { model.restore(item); model.say(context.getString(R.string.passwords_restored_done, item.name)); navigator.back() }
                        }
                        Footer(stringResource(R.string.passwords_restore_footer))
                        ActionRow(Icons.Outlined.DeleteForever, stringResource(R.string.passwords_delete_forever), isDestructive = true, enabled = !isWorking) { isConfirmingDeleteForever = true }
                    }
                    item.isSharedWithMe -> {
                        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Outlined.Group, null); Text(" " + stringResource(R.string.passwords_shared_by, item.ownerName))
                        }
                        Footer(stringResource(R.string.passwords_shared_by_footer, item.ownerName, item.ownerName))
                    }
                    else -> OwnerSections(model, item, isWorking, onRun = { failure, work -> run(failure, work) }, onDelete = { isConfirmingDelete = true })
                }
            }
        }
    }
    if (isConfirmingDelete && item != null) {
        ConfirmationDialog(
            title = stringResource(R.string.passwords_delete_title, item.name), text = stringResource(R.string.passwords_delete_message),
            confirmLabel = stringResource(R.string.passwords_delete), isDestructive = true,
            onConfirm = {
                isConfirmingDelete = false
                isWorking = true
                scope.launch {
                    try {
                        model.delete(item)
                        model.say(context.getString(R.string.passwords_deleted_done, item.name))
                        navigator.back()
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (error: Throwable) {
                        model.report(error, context.getString(R.string.passwords_delete_failed))
                    } finally {
                        isWorking = false
                    }
                }
            },
            onDismiss = { isConfirmingDelete = false },
        )
    }
    if (isConfirmingDeleteForever && item != null) {
        ConfirmationDialog(
            title = stringResource(R.string.passwords_delete_forever_item_title, item.name), text = stringResource(R.string.passwords_delete_forever_item_message),
            confirmLabel = stringResource(R.string.passwords_delete_forever), isDestructive = true,
            onConfirm = {
                isConfirmingDeleteForever = false
                scope.launch {
                    try {
                        model.purge(item)
                        model.say(context.getString(R.string.passwords_deleted_forever_done, item.name))
                        navigator.back()
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (error: Throwable) {
                        model.report(error, context.getString(R.string.passwords_deleted_forever_failed))
                    }
                }
            },
            onDismiss = { isConfirmingDeleteForever = false },
        )
    }
}

@Composable
internal fun Footer(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp))
}

@Composable
internal fun ActionRow(icon: androidx.compose.ui.graphics.vector.ImageVector, title: String, isDestructive: Boolean = false, enabled: Boolean = true, onClick: () -> Unit) {
    val color = if (isDestructive) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary
    Row(
        Modifier.fillMaxWidth().clickable(enabled = enabled, onClick = onClick).padding(horizontal = 16.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Icon(icon, null, tint = if (enabled) color else color.copy(alpha = 0.4f))
        Text(title, color = if (enabled) color else color.copy(alpha = 0.4f), style = MaterialTheme.typography.bodyLarge)
    }
}

@Composable
private fun OwnerSections(model: PasswordsModel, item: PasswordItem, isWorking: Boolean, onRun: (Int, suspend () -> Unit) -> Unit, onDelete: () -> Unit) {
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    HorizontalDivider()
    if (item.itemKind != PasswordsItemKind.Card) {
        Text(stringResource(R.string.passwords_agent_header), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(16.dp, 16.dp, 16.dp, 0.dp))
        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.passwords_agent_toggle), modifier = Modifier.weight(1f))
            // The agent's consent is the person's own record, which has to be checked first.
            Switch(
                checked = item.isUsableByAgent, enabled = !isWorking && item.sharingCheck == PasswordsSharingCheck.Verified,
                onCheckedChange = { isOn ->
                    onRun(R.string.passwords_agent_failed) {
                        model.setUsableByAgent(item, isOn)
                        model.say(context.getString(if (isOn) R.string.passwords_agent_now else R.string.passwords_agent_no_longer))
                    }
                },
            )
        }
        val usedAt = item.lastUsedByAgentAtEpochMS
        if (item.isUsableByAgent && usedAt != null) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(stringResource(R.string.passwords_agent_last_used)); Text(relativeTimeText(usedAt, System.currentTimeMillis()), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        Footer(stringResource(if (item.itemKind == PasswordsItemKind.Note) R.string.passwords_agent_footer_note else R.string.passwords_agent_footer_login))
    }
    if (model.itemIdsNeedingNewKey().contains(item.id)) {
        Text(stringResource(R.string.passwords_new_key_failed_title), modifier = Modifier.padding(16.dp))
        ActionRow(Icons.Outlined.Refresh, stringResource(R.string.passwords_try_again), enabled = !isWorking) { onRun(R.string.passwords_new_key_failed) { model.giveNewKey(item) } }
    }
    if (item.sharingCheck == PasswordsSharingCheck.Unverified || item.sharingCheck == PasswordsSharingCheck.Replayed) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (item.sharingCheck == PasswordsSharingCheck.Unverified) {
                Text(stringResource(R.string.passwords_sharing_unchecked), style = MaterialTheme.typography.titleSmall)
                Text(stringResource(R.string.passwords_sharing_unchecked_text), color = MaterialTheme.colorScheme.onSurfaceVariant)
            } else {
                Text(stringResource(R.string.passwords_sharing_replayed), style = MaterialTheme.typography.titleSmall)
                Text(stringResource(R.string.passwords_sharing_replayed_text), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    ActionRow(Icons.Outlined.History, stringResource(R.string.passwords_versions_row)) { navigator.navigate("passwords/versions/${Uri.encode(item.id)}") }
    Footer(stringResource(R.string.passwords_versions_footer))
    ActionRow(Icons.Outlined.Delete, stringResource(R.string.passwords_delete), isDestructive = true, enabled = !isWorking, onClick = onDelete)
}

// The fields

/**
 * The sections that show an item's fields (a login, a card or a note): secrets hidden until shown, a tap copies, sites
 * open in the browser. The item's screen and an earlier version's screen both show them.
 */
@Composable
internal fun PasswordsFieldSections(model: PasswordsModel, fields: PasswordsItemFields) {
    var revealed by remember { mutableStateOf(setOf<String>()) }
    // A secret shown stays shown only while the app is in front.
    val lifecycleOwner = androidx.lifecycle.compose.LocalLifecycleOwner.current
    androidx.compose.runtime.DisposableEffect(lifecycleOwner) {
        val observer = androidx.lifecycle.LifecycleEventObserver { _, event -> if (event == androidx.lifecycle.Lifecycle.Event.ON_STOP) revealed = emptySet() }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }
    fun toggle(name: String) {
        if (revealed.contains(name)) revealed -= name else { revealed += name; model.noteUse() }
    }
    Column {
        when (fields) {
            is PasswordsItemFields.Login -> LoginSections(model, fields.fields, revealed, ::toggle)
            is PasswordsItemFields.Card -> CardSections(model, fields.fields, revealed, ::toggle)
            is PasswordsItemFields.Note -> {
                FieldRow(model, stringResource(R.string.passwords_note), fields.fields.text)
                FolderRow(fields.fields.folder)
            }
        }
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(text, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(16.dp, 16.dp, 16.dp, 4.dp))
}

@Composable
private fun LoginSections(model: PasswordsModel, login: PasswordsLoginFields, revealed: Set<String>, toggle: (String) -> Unit) {
    val context = LocalContext.current
    if (login.userName.isNotEmpty()) FieldRow(model, stringResource(R.string.passwords_user_name), login.userName)
    if (login.password.isNotEmpty()) FieldRow(model, stringResource(R.string.passwords_password), login.password, isSecret = true, isRevealed = revealed.contains("password"), onToggleReveal = { toggle("password") })
    val settings = remember(login.oneTimeCodeUri) { runCatching { PasswordsOneTimeCode.parse(login.oneTimeCodeUri) }.getOrNull() }
    if (settings != null) OneTimeCodeRow(model, settings)
    else if (login.oneTimeCodeUri.isNotEmpty()) Text(stringResource(R.string.passwords_one_time_code_unreadable), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
    if (login.sites.isNotEmpty()) {
        SectionTitle(stringResource(R.string.passwords_sites))
        login.sites.forEach { site ->
            val address = PasswordsFormat.urlOfSite(site)
            Row(
                Modifier.fillMaxWidth().then(if (address != null) Modifier.clickable { runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(address)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) } } else Modifier).padding(horizontal = 16.dp, vertical = 10.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                if (address != null) Icon(Icons.Outlined.Language, null, tint = MaterialTheme.colorScheme.primary)
                Text(if (address != null) PasswordsFormat.shownSite(site) else site)
            }
        }
    }
    if (login.passkeys.isNotEmpty()) {
        SectionTitle(stringResource(R.string.passwords_passkeys))
        login.passkeys.forEach { passkey -> PasskeyRow(passkey, login.userName) }
    }
    if (login.notes.isNotEmpty()) {
        SectionTitle(stringResource(R.string.passwords_notes))
        FieldRow(model, stringResource(R.string.passwords_notes), login.notes)
    }
    FolderRow(login.folder)
}

@Composable
private fun CardSections(model: PasswordsModel, card: PasswordsCardFields, revealed: Set<String>, toggle: (String) -> Unit) {
    if (card.cardholderName.isNotEmpty()) FieldRow(model, stringResource(R.string.passwords_name_on_card), card.cardholderName)
    if (card.number.isNotEmpty()) {
        FieldRow(
            model, stringResource(R.string.passwords_card_number), PasswordsFormat.groupedCardNumber(card.number), copiedValue = card.number.filter { character -> character.isDigit() },
            isSecret = true, isRevealed = revealed.contains("number"), maskedValue = PasswordsFormat.maskedCardNumber(card.number), onToggleReveal = { toggle("number") },
        )
    }
    val expiry = PasswordsFormat.expiryText(card.expiryMonth, card.expiryYear)
    if (expiry.isNotEmpty()) FieldRow(model, stringResource(R.string.passwords_expires), expiry)
    if (card.securityCode.isNotEmpty()) FieldRow(model, stringResource(R.string.passwords_security_code), card.securityCode, isSecret = true, isRevealed = revealed.contains("securityCode"), onToggleReveal = { toggle("securityCode") })
    if (card.brand.isNotEmpty()) FieldRow(model, stringResource(R.string.passwords_card_brand), card.brand)
    if (card.notes.isNotEmpty()) {
        SectionTitle(stringResource(R.string.passwords_notes))
        FieldRow(model, stringResource(R.string.passwords_notes), card.notes)
    }
    FolderRow(card.folder)
}

@Composable
private fun FolderRow(folder: String) {
    if (folder.isEmpty()) return
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(stringResource(R.string.passwords_folder)); Text(folder, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** One field of an item: its name above its value. A tap copies the value; a secret shows as dots until the eye is pressed. */
@Composable
internal fun FieldRow(
    model: PasswordsModel,
    title: String,
    value: String,
    copiedValue: String? = null,
    isSecret: Boolean = false,
    isRevealed: Boolean = false,
    maskedValue: String = "••••••••••",
    onToggleReveal: (() -> Unit)? = null,
) {
    Row(Modifier.fillMaxWidth().clickable { model.copy(copiedValue ?: value, title) }.padding(start = 16.dp, top = 8.dp, bottom = 8.dp, end = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(
                if (isSecret && !isRevealed) maskedValue else value,
                style = if (isSecret && isRevealed) MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace) else MaterialTheme.typography.bodyLarge,
            )
        }
        if (isSecret && onToggleReveal != null) {
            IconButton(onClick = onToggleReveal) {
                Icon(
                    if (isRevealed) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility,
                    contentDescription = stringResource(if (isRevealed) R.string.passwords_hide_field else R.string.passwords_show_field, title),
                )
            }
        }
    }
}

/** A login's one-time code, changing as it does, with the seconds left before the next. A tap copies the code. */
@Composable
private fun OneTimeCodeRow(model: PasswordsModel, settings: com.teanode.kit.passwords.PasswordsOneTimeCodeSettings) {
    var nowSeconds by remember { mutableLongStateOf(System.currentTimeMillis() / 1000) }
    LaunchedEffect(settings) {
        while (true) {
            nowSeconds = System.currentTimeMillis() / 1000
            if (model.isDemo) break
            delay(500)
        }
    }
    val code = PasswordsOneTimeCode.code(settings, nowSeconds)
    val secondsLeft = PasswordsOneTimeCode.secondsLeft(settings, nowSeconds)
    val label = stringResource(R.string.passwords_one_time_code)
    Column(Modifier.fillMaxWidth().clickable { model.copy(code, label) }.padding(16.dp, 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(label, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(PasswordsFormat.groupedOneTimeCode(code), style = MaterialTheme.typography.headlineSmall.copy(fontFamily = FontFamily.Monospace))
        LinearProgressIndicator(progress = { secondsLeft.toFloat() / settings.periodSeconds }, modifier = Modifier.fillMaxWidth())
        Text(pluralStringResource(R.plurals.passwords_changes_in_seconds, secondsLeft, secondsLeft), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** A passkey kept in a login: the site it is for, the account, and when it was made. */
@Composable
private fun PasskeyRow(passkey: PasswordsPasskeyFields, loginUserName: String) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(passkey.relyingPartyId)
        val account = passkey.userName.ifEmpty { loginUserName }
        if (account.isNotEmpty()) Text(account, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        val created = runCatching { java.time.Instant.parse(passkey.createdAt).toEpochMilli() }.getOrNull()
        if (created != null) {
            Text(stringResource(R.string.passwords_created_on, DateFormat.getDateInstance(DateFormat.MEDIUM).format(Date(created))), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

// Earlier versions

/** The earlier versions of an item the person owns: the list, each with its date and who saved it. The server keeps them for 30 days. */
@Composable
fun PasswordsVersionsScreen(itemId: String) {
    val model = rememberPasswordsModel()
    val navigator = LocalAppNavigator.current
    GoBackWhenClosed(model)
    var isLoading by remember { mutableStateOf(true) }
    val item = model.item(itemId)
    LaunchedEffect(itemId) {
        // Read once; coming back to this screen from a version shows what was read.
        val held = model.item(itemId)
        if (held != null && model.versionsByItemId[itemId] == null) model.loadVersions(held)
        isLoading = false
    }
    val opened = model.versionsByItemId[itemId]
    val scope = rememberCoroutineScope()
    TeaNodeScreen(title = stringResource(R.string.passwords_versions_title), onBack = { navigator.back() }) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            when {
                item == null || item.isSharedWithMe || item.isDeleted -> EmptyState(Icons.Outlined.Key, stringResource(R.string.passwords_item_gone_title), description = stringResource(R.string.passwords_item_gone_description))
                opened != null && opened.versions.isEmpty() && opened.unreadableVersionCount == 0 -> EmptyState(Icons.Outlined.History, stringResource(R.string.passwords_versions_none_title), description = stringResource(R.string.passwords_versions_none_description))
                opened != null -> Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                    opened.versions.forEach { version ->
                        Column(Modifier.fillMaxWidth().clickable { navigator.navigate("passwords/version/${Uri.encode(itemId)}/${Uri.encode(version.id)}") }.padding(16.dp, 12.dp)) {
                            Text(versionDateText(version))
                            Text(versionSavedByText(version), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    if (opened.unreadableVersionCount > 0) Footer(pluralStringResource(R.plurals.passwords_versions_unreadable, opened.unreadableVersionCount, opened.unreadableVersionCount))
                }
                isLoading -> LoadingState()
                else -> com.teanode.app.ui.components.ErrorState(stringResource(R.string.passwords_versions_could_not_read), onRetry = { scope.launch { model.loadVersions(item) } })
            }
        }
    }
}

@Composable
internal fun versionDateText(version: PasswordItemVersion): String {
    val savedAt = version.savedAtEpochMS ?: return stringResource(R.string.passwords_version_earlier)
    return DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(Date(savedAt))
}

@Composable
internal fun versionSavedByText(version: PasswordItemVersion): String =
    stringResource(if (version.isSavedByPerson) R.string.passwords_version_by_you else R.string.passwords_version_by_other)

/** One earlier version of an item, read-only, with the button that puts it back. */
@Composable
fun PasswordsVersionScreen(itemId: String, versionId: String) {
    val model = rememberPasswordsModel()
    val navigator = LocalAppNavigator.current
    GoBackWhenClosed(model)
    val item = model.item(itemId)
    val version = model.versionsByItemId[itemId]?.versions?.firstOrNull { entry -> entry.id == versionId }
    var isConfirmingRestore by remember { mutableStateOf(false) }
    var isWorking by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    LaunchedEffect(Unit) { model.noteUse() }
    val dateText = if (version != null) versionDateText(version) else ""
    TeaNodeScreen(title = dateText, onBack = { navigator.back() }) { padding ->
        if (item == null || version == null) {
            Box(Modifier.padding(padding)) { EmptyState(Icons.Outlined.History, stringResource(R.string.passwords_version_gone_title), description = stringResource(R.string.passwords_version_gone_description)) }
        } else {
            Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
                Row(Modifier.padding(16.dp), verticalAlignment = Alignment.CenterVertically) { Icon(Icons.Outlined.History, null); Text(" " + versionSavedByText(version)) }
                PasswordsFieldSections(model, version.fields)
                ActionRow(Icons.AutoMirrored.Outlined.Undo, stringResource(R.string.passwords_version_restore), enabled = !isWorking) { isConfirmingRestore = true }
            }
        }
    }
    if (isConfirmingRestore && item != null && version != null) {
        ConfirmationDialog(
            title = stringResource(R.string.passwords_version_restore), text = stringResource(R.string.passwords_version_restore_message, dateText),
            confirmLabel = stringResource(R.string.passwords_version_restore),
            onConfirm = {
                isConfirmingRestore = false
                isWorking = true
                scope.launch {
                    try {
                        model.restoreVersion(version, item)
                        model.say(context.getString(R.string.passwords_version_restored, dateText))
                        model.forgetVersions(item.id)
                        navigator.back()
                        navigator.back()
                    } catch (cancelled: CancellationException) {
                        throw cancelled
                    } catch (error: Throwable) {
                        model.report(error, context.getString(R.string.passwords_version_restore_failed))
                    } finally {
                        isWorking = false
                    }
                }
            },
            onDismiss = { isConfirmingRestore = false },
        )
    }
}

// Recently deleted

/** The items deleted in the last 30 days, which can be restored. */
@Composable
fun PasswordsDeletedScreen() {
    val model = rememberPasswordsModel()
    val navigator = LocalAppNavigator.current
    GoBackWhenClosed(model)
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var toDeleteForever by remember { mutableStateOf<PasswordItem?>(null) }
    TeaNodeScreen(title = stringResource(R.string.passwords_recently_deleted), onBack = { navigator.back() }) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            if (model.deletedItems.isEmpty()) {
                EmptyState(Icons.Outlined.Delete, stringResource(R.string.passwords_deleted_empty_title), description = stringResource(R.string.passwords_deleted_empty_description))
            } else {
                Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                    model.deletedItems.forEach { item ->
                        Row(Modifier.fillMaxWidth().clickable { navigator.navigate("passwords/item/${Uri.encode(item.id)}") }.padding(start = 16.dp, top = 8.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                            Icon(item.itemKind.icon(), null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                            Column(Modifier.weight(1f)) {
                                Text(item.name)
                                item.deletedAtEpochMS?.let { deletedAt -> Text(stringResource(R.string.passwords_deleted_ago, relativeTimeText(deletedAt, System.currentTimeMillis())), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            }
                            IconButton(onClick = {
                                scope.launch {
                                    try { model.restore(item); model.say(context.getString(R.string.passwords_restored_done, item.name)) } catch (cancelled: CancellationException) { throw cancelled } catch (error: Throwable) { model.report(error, context.getString(R.string.passwords_restored_failed)) }
                                }
                            }) { Icon(Icons.AutoMirrored.Outlined.Undo, contentDescription = stringResource(R.string.passwords_restore)) }
                            IconButton(onClick = { toDeleteForever = item }) { Icon(Icons.Outlined.DeleteForever, contentDescription = stringResource(R.string.passwords_delete_forever), tint = MaterialTheme.colorScheme.error) }
                        }
                    }
                    Footer(stringResource(R.string.passwords_deleted_empty_description))
                }
            }
        }
    }
    toDeleteForever?.let { item ->
        ConfirmationDialog(
            title = stringResource(R.string.passwords_delete_forever_title), text = stringResource(R.string.passwords_delete_forever_message, item.name),
            confirmLabel = stringResource(R.string.passwords_delete_forever), isDestructive = true,
            onConfirm = {
                toDeleteForever = null
                scope.launch {
                    try { model.purge(item); model.say(context.getString(R.string.passwords_deleted_forever_done, item.name)) } catch (cancelled: CancellationException) { throw cancelled } catch (error: Throwable) { model.report(error, context.getString(R.string.passwords_deleted_forever_failed)) }
                }
            },
            onDismiss = { toDeleteForever = null },
        )
    }
}
