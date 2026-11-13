package com.teanode.app.passwords

import android.content.Intent
import android.view.WindowManager
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Fingerprint
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.SmartToy
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.ArrowDropDown
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.CreditCard
import androidx.compose.material.icons.outlined.Notes
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
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
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
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
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.passwords.PasswordItem
import com.teanode.kit.passwords.PasswordsFilter
import com.teanode.kit.passwords.PasswordsFormat
import com.teanode.kit.passwords.PasswordsItemKind
import com.teanode.kit.passwords.PasswordsPassphraseStrength
import com.teanode.kit.passwords.folder
import com.teanode.kit.passwords.matches
import com.teanode.kit.passwords.subtitle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * The first screen of Passwords: whatever Passwords is in. Setting it up, closed (locked), or open with the list of items.
 * An item, the recently deleted items and the settings are screens of their own over it.
 */
@Composable
fun PasswordsSection(demoStart: PasswordsDemoStart = PasswordsDemoStart.Unlocked) {
    val model = rememberPasswordsModel(demoStart)
    val scope = rememberCoroutineScope()
    val lifecycleOwner = LocalLifecycleOwner.current
    val context = LocalContext.current

    LaunchedEffect(model) { model.start() }
    PasswordsScreenGuard(model)
    // Locks itself once the lock time has passed without use, checked every few seconds while Passwords is open.
    LaunchedEffect(model.phase) {
        if (model.phase != PasswordsPhase.Unlocked) return@LaunchedEffect
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                if (model.isDemo) break
                delay(15_000)
                model.lockIfAskRequired()
            }
        }
    }
    // Coming back to the app asks again if Passwords locked meanwhile; the phone locking locks Passwords at once.
    DisposableEffect(model) {
        val receiver = object : android.content.BroadcastReceiver() {
            override fun onReceive(receiverContext: android.content.Context?, intent: Intent?) {
                model.lock()
            }
        }
        context.registerReceiver(receiver, android.content.IntentFilter(Intent.ACTION_SCREEN_OFF))
        onDispose { runCatching { context.unregisterReceiver(receiver) } }
    }
    LaunchedEffect(Unit) {
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) {
            if (model.phase == PasswordsPhase.Unlocked) {
                model.lockIfAskRequired()
                model.sync()
            } else if (model.phase == PasswordsPhase.Locked) {
                model.start()
            }
        }
    }

    when (model.phase) {
        PasswordsPhase.Loading -> TeaNodeScreen(stringResource(R.string.passwords_title)) { padding -> Box(Modifier.padding(padding)) { LoadingState() } }
        PasswordsPhase.Failed -> TeaNodeScreen(stringResource(R.string.passwords_title)) { padding ->
            Box(Modifier.padding(padding)) { ErrorState(model.failedText.orEmpty(), onRetry = { scope.launch { model.start() } }) }
        }
        PasswordsPhase.NotSetUp -> PasswordsIntroScreen(model)
        PasswordsPhase.RecoveryKey -> PasswordsRecoveryKeyScreen(model)
        PasswordsPhase.Locked -> PasswordsUnlockScreen(model)
        PasswordsPhase.Unlocked -> PasswordsListScreen(model)
    }
}

/**
 * Keeps what the screen shows out of the picture the system keeps of the app (the recents list) and out of screenshots
 * and screen recordings, while Passwords is open.
 */
@Composable
internal fun PasswordsScreenGuard(model: PasswordsModel) {
    val context = LocalContext.current
    val isOpen = model.phase == PasswordsPhase.Unlocked || model.phase == PasswordsPhase.RecoveryKey
    DisposableEffect(isOpen) {
        val window = (context as? android.app.Activity)?.window
        if (isOpen) window?.setFlags(WindowManager.LayoutParams.FLAG_SECURE, WindowManager.LayoutParams.FLAG_SECURE)
        onDispose { window?.clearFlags(WindowManager.LayoutParams.FLAG_SECURE) }
    }
}

/** A screen that only makes sense while Passwords is open goes back to the first screen when it locks. */
@Composable
internal fun GoBackWhenClosed(model: PasswordsModel) {
    val navigator = LocalAppNavigator.current
    LaunchedEffect(model.phase) { if (model.phase != PasswordsPhase.Unlocked) navigator.back() }
}

/** The kind of an item as a symbol. */
internal fun PasswordsItemKind.icon() = when (this) {
    PasswordsItemKind.Login -> Icons.Outlined.Key
    PasswordsItemKind.Card -> Icons.Outlined.CreditCard
    PasswordsItemKind.Note -> Icons.Outlined.Notes
}

@Composable
internal fun PasswordsItemKind.name(): String = stringResource(
    when (this) {
        PasswordsItemKind.Login -> R.string.passwords_kind_login
        PasswordsItemKind.Card -> R.string.passwords_kind_card
        PasswordsItemKind.Note -> R.string.passwords_kind_note
    },
)

@Composable
internal fun PasswordsItemKind.pluralName(): String = stringResource(
    when (this) {
        PasswordsItemKind.Login -> R.string.passwords_kind_logins
        PasswordsItemKind.Card -> R.string.passwords_kind_cards
        PasswordsItemKind.Note -> R.string.passwords_kind_notes
    },
)

// The intro

/** The screen before Passwords is set up: what it is, and the way to set it up. */
@Composable
private fun PasswordsIntroScreen(model: PasswordsModel) {
    val navigator = LocalAppNavigator.current
    TeaNodeScreen(
        title = stringResource(R.string.passwords_title),
        actions = { SettingsAction() },
    ) { padding ->
        Box(Modifier.padding(padding)) {
            EmptyState(
                Icons.Outlined.Key, stringResource(R.string.passwords_intro_title), description = stringResource(R.string.passwords_intro_description),
                actionLabel = stringResource(R.string.passwords_intro_set_up), onAction = { navigator.navigate("passwords/setup") },
            )
        }
    }
}

@Composable
private fun SettingsAction() {
    val navigator = LocalAppNavigator.current
    IconButton(onClick = { navigator.navigate("passwords/settings") }) {
        Icon(Icons.Outlined.Settings, contentDescription = stringResource(R.string.passwords_settings_description))
    }
}

// The list

/** The open Passwords: the items, searchable, and a menu that chooses which to show. */
@Composable
private fun PasswordsListScreen(model: PasswordsModel) {
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    var filter by remember { mutableStateOf<PasswordsFilter>(PasswordsFilter.Everything) }
    var searchText by remember { mutableStateOf("") }
    var isFilterOpen by remember { mutableStateOf(false) }
    var isNewOpen by remember { mutableStateOf(false) }
    var isOptionsOpen by remember { mutableStateOf(false) }
    val shownItems = model.items.filter { item -> filter.includes(item) && item.matches(searchText) }
    val filterTitle = when (val chosen = filter) {
        PasswordsFilter.Everything -> stringResource(R.string.passwords_title)
        is PasswordsFilter.Kind -> chosen.itemKind.pluralName()
        PasswordsFilter.Shared -> stringResource(R.string.passwords_filter_shared)
        PasswordsFilter.UsableByAgent -> stringResource(R.string.passwords_filter_agent)
        is PasswordsFilter.Folder -> chosen.name
    }

    TeaNodeScreen(
        title = filterTitle,
        actions = {
            Box {
                IconButton(onClick = { isFilterOpen = true }) { Icon(Icons.Outlined.ArrowDropDown, contentDescription = stringResource(R.string.passwords_filter_show)) }
                DropdownMenu(isFilterOpen, { isFilterOpen = false }) {
                    FilterItem(stringResource(R.string.passwords_filter_all), filter == PasswordsFilter.Everything) { filter = PasswordsFilter.Everything; isFilterOpen = false; model.noteUse() }
                    PasswordsItemKind.entries.forEach { kind ->
                        FilterItem(kind.pluralName(), filter == PasswordsFilter.Kind(kind), kind.icon()) { filter = PasswordsFilter.Kind(kind); isFilterOpen = false; model.noteUse() }
                    }
                    FilterItem(stringResource(R.string.passwords_filter_shared), filter == PasswordsFilter.Shared) { filter = PasswordsFilter.Shared; isFilterOpen = false; model.noteUse() }
                    FilterItem(stringResource(R.string.passwords_filter_agent), filter == PasswordsFilter.UsableByAgent) { filter = PasswordsFilter.UsableByAgent; isFilterOpen = false; model.noteUse() }
                    if (model.folders.isNotEmpty()) {
                        HorizontalDivider()
                        model.folders.forEach { folder ->
                            FilterItem(folder, filter == PasswordsFilter.Folder(folder), Icons.Outlined.Folder) { filter = PasswordsFilter.Folder(folder); isFilterOpen = false; model.noteUse() }
                        }
                    }
                }
            }
            Box {
                IconButton(onClick = { isNewOpen = true }) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.passwords_new_item)) }
                DropdownMenu(isNewOpen, { isNewOpen = false }) {
                    PasswordsItemKind.entries.forEach { kind ->
                        DropdownMenuItem(
                            text = { Text(stringResource(when (kind) { PasswordsItemKind.Login -> R.string.passwords_new_login; PasswordsItemKind.Card -> R.string.passwords_new_card; PasswordsItemKind.Note -> R.string.passwords_new_note })) },
                            leadingIcon = { Icon(kind.icon(), null) }, onClick = { isNewOpen = false; navigator.navigate("passwords/edit/${kind.rawValue}/new") },
                        )
                    }
                }
            }
            Box {
                IconButton(onClick = { isOptionsOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.passwords_options)) }
                DropdownMenu(isOptionsOpen, { isOptionsOpen = false }) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.passwords_lock_now)) }, leadingIcon = { Icon(Icons.Outlined.Lock, null) }, onClick = { isOptionsOpen = false; model.lock() })
                    DropdownMenuItem(text = { Text(stringResource(R.string.passwords_recently_deleted)) }, leadingIcon = { Icon(Icons.Outlined.Delete, null) }, onClick = { isOptionsOpen = false; navigator.navigate("passwords/deleted") })
                    DropdownMenuItem(text = { Text(stringResource(R.string.passwords_settings)) }, leadingIcon = { Icon(Icons.Outlined.Settings, null) }, onClick = { isOptionsOpen = false; navigator.navigate("passwords/settings") })
                }
            }
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            if (model.items.isNotEmpty()) {
                OutlinedTextField(
                    value = searchText, onValueChange = { searchText = it; model.noteUse() }, modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                    placeholder = { Text(stringResource(R.string.passwords_search_items)) }, singleLine = true,
                    leadingIcon = { Icon(Icons.Outlined.Search, null) },
                    trailingIcon = { if (searchText.isNotEmpty()) IconButton(onClick = { searchText = "" }) { Icon(Icons.Outlined.Close, null) } },
                    shape = androidx.compose.foundation.shape.RoundedCornerShape(28.dp),
                )
            }
            when {
                model.items.isEmpty() -> EmptyState(
                    Icons.Outlined.Key, stringResource(R.string.passwords_empty_title), description = stringResource(R.string.passwords_empty_description),
                    actionLabel = stringResource(R.string.passwords_add_a_login), onAction = { navigator.navigate("passwords/edit/login/new") },
                )
                shownItems.isEmpty() -> EmptyState(Icons.Outlined.Search, filterTitle, description = stringResource(R.string.passwords_nothing_here))
                else -> LazyColumn(Modifier.fillMaxSize()) {
                    items(shownItems, key = { item -> item.id }) { item -> PasswordsItemRow(model, item, onClick = { navigator.navigate("passwords/item/${android.net.Uri.encode(item.id)}") }) }
                    if (model.unreadableItemCount > 0) {
                        item {
                            Text(
                                pluralStringResource(R.plurals.passwords_unreadable_items, model.unreadableItemCount, model.unreadableItemCount),
                                modifier = Modifier.padding(16.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            }
        }
    }
    @Suppress("UNUSED_VARIABLE") val unusedScope = scope
}

@Composable
private fun FilterItem(text: String, isSelected: Boolean, icon: androidx.compose.ui.graphics.vector.ImageVector? = null, onClick: () -> Unit) {
    DropdownMenuItem(
        text = { Text(text, style = if (isSelected) MaterialTheme.typography.titleSmall else MaterialTheme.typography.bodyLarge) },
        leadingIcon = icon?.let { vector -> { Icon(vector, null) } }, onClick = onClick,
    )
}

/** One item in the list: what it is, its name, and whether it is shared. A long press opens what the item holds to copy without opening it. */
@Composable
internal fun PasswordsItemRow(model: PasswordsModel, item: PasswordItem, onClick: () -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    Box {
        Row(
            Modifier.fillMaxWidth().combinedClickableCompat(onClick) { isMenuOpen = true }.padding(horizontal = 16.dp, vertical = 12.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Icon(item.itemKind.icon(), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Column(Modifier.weight(1f)) {
                Text(item.name.ifEmpty { stringResource(R.string.passwords_untitled) }, style = MaterialTheme.typography.bodyLarge, maxLines = 2)
                if (item.subtitle.isNotEmpty()) Text(item.subtitle, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                if (item.isSharedWithMe) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.Group, null, modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text(" " + stringResource(R.string.passwords_shared_by, item.ownerName), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                } else if (item.recordedShares.isNotEmpty()) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.Group, null, modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text(" " + stringResource(R.string.passwords_shared), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
        DropdownMenu(isMenuOpen, { isMenuOpen = false }) { PasswordsCopyMenu(model, item) { isMenuOpen = false } }
    }
}

@OptIn(androidx.compose.foundation.ExperimentalFoundationApi::class)
private fun Modifier.combinedClickableCompat(onClick: () -> Unit, onLongClick: () -> Unit): Modifier =
    this.combinedClickable(onClick = onClick, onLongClick = onLongClick)

/** The long-press menu of an item in the list: copy what the item holds without opening it. */
@Composable
private fun PasswordsCopyMenu(model: PasswordsModel, item: PasswordItem, onDone: () -> Unit) {
    val fields = item.fields
    val userNameLabel = stringResource(R.string.passwords_user_name)
    val passwordLabel = stringResource(R.string.passwords_password)
    val cardNumberLabel = stringResource(R.string.passwords_card_number)
    when (fields) {
        is com.teanode.kit.passwords.PasswordsItemFields.Login -> {
            if (fields.fields.userName.isNotEmpty()) DropdownMenuItem(text = { Text(stringResource(R.string.passwords_copy_user_name)) }, leadingIcon = { Icon(Icons.Outlined.ContentCopy, null) }, onClick = { onDone(); model.copy(fields.fields.userName, userNameLabel) })
            if (fields.fields.password.isNotEmpty()) DropdownMenuItem(text = { Text(stringResource(R.string.passwords_copy_password)) }, leadingIcon = { Icon(Icons.Outlined.Key, null) }, onClick = { onDone(); model.copy(fields.fields.password, passwordLabel) })
        }
        is com.teanode.kit.passwords.PasswordsItemFields.Card -> {
            if (fields.fields.number.isNotEmpty()) DropdownMenuItem(text = { Text(stringResource(R.string.passwords_copy_card_number)) }, leadingIcon = { Icon(Icons.Outlined.CreditCard, null) }, onClick = { onDone(); model.copy(fields.fields.number.filter { character -> character.isDigit() }, cardNumberLabel) })
        }
        is com.teanode.kit.passwords.PasswordsItemFields.Note -> DropdownMenuItem(text = { Text(item.name) }, onClick = onDone, enabled = false)
    }
}

// Setting up

/** Asks for the passphrase twice. Nothing about the passphrase is sent to the server: the key made from it stays here. */
@Composable
fun PasswordsSetUpScreen(demoPassphrase: String? = null) {
    val model = rememberPasswordsModel(PasswordsDemoStart.NotSetUp)
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    var passphrase by remember { mutableStateOf(demoPassphrase.orEmpty()) }
    var repeated by remember { mutableStateOf(demoPassphrase.orEmpty()) }
    var isWorking by remember { mutableStateOf(false) }
    val strength = PasswordsPassphraseStrength.of(passphrase)
    val isLongEnough = passphrase.codePointCount(0, passphrase.length) >= PasswordsPassphraseStrength.MINIMUM_LENGTH
    val isMatching = passphrase == repeated
    val canContinue = isLongEnough && isMatching && !isWorking
    // The recovery key screen is the first screen's: leaving this one shows it.
    LaunchedEffect(model.phase) { if (model.phase == PasswordsPhase.RecoveryKey) navigator.back() }
    val failedText = stringResource(R.string.passwords_set_up_failed)

    TeaNodeScreen(
        title = stringResource(R.string.passwords_set_up_title), onBack = { navigator.back() },
        actions = {
            if (isWorking) CircularProgressIndicator(Modifier.padding(end = 16.dp).size(20.dp), strokeWidth = 2.dp)
            else TextButton(
                enabled = canContinue,
                onClick = {
                    isWorking = true
                    scope.launch {
                        try {
                            model.setUp(passphrase)
                        } catch (cancelled: CancellationException) {
                            throw cancelled
                        } catch (error: Throwable) {
                            model.report(error, failedText)
                        } finally {
                            isWorking = false
                        }
                    }
                },
            ) { Text(stringResource(R.string.passwords_continue_button)) }
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.passwords_choose_passphrase), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            SecretField(passphrase, { passphrase = it }, stringResource(R.string.passwords_passphrase))
            Text(
                when (strength) {
                    PasswordsPassphraseStrength.Empty -> stringResource(R.string.passwords_strength_empty)
                    PasswordsPassphraseStrength.Short -> stringResource(R.string.passwords_strength_short, PasswordsPassphraseStrength.MINIMUM_LENGTH)
                    PasswordsPassphraseStrength.Weak -> stringResource(R.string.passwords_strength_weak)
                    PasswordsPassphraseStrength.Fair -> stringResource(R.string.passwords_strength_fair)
                    PasswordsPassphraseStrength.Strong -> stringResource(R.string.passwords_strength_strong)
                },
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(stringResource(R.string.passwords_passphrase_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            SecretField(repeated, { repeated = it }, stringResource(R.string.passwords_type_it_again))
            if (repeated.isNotEmpty() && !isMatching) Text(stringResource(R.string.passwords_passphrases_differ), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(stringResource(R.string.passwords_loss_sentence), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** A field for something secret: its letters are dots, with a switch to see them while the person types. */
@Composable
internal fun SecretField(value: String, onValueChange: (String) -> Unit, label: String, modifier: Modifier = Modifier, isMonospaced: Boolean = false, canReveal: Boolean = true) {
    var isShown by remember { mutableStateOf(false) }
    OutlinedTextField(
        value = value, onValueChange = onValueChange, modifier = modifier.fillMaxWidth(), label = { Text(label) }, singleLine = true,
        visualTransformation = if (isShown) VisualTransformation.None else PasswordVisualTransformation(),
        textStyle = if (isMonospaced) MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace) else MaterialTheme.typography.bodyLarge,
        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false, keyboardType = androidx.compose.ui.text.input.KeyboardType.Password),
        trailingIcon = if (canReveal) {
            {
                IconButton(onClick = { isShown = !isShown }) {
                    Icon(if (isShown) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, contentDescription = stringResource(if (isShown) R.string.passwords_hide_password else R.string.passwords_show_password))
                }
            }
        } else null,
    )
}

// The recovery key

/** A recovery key, with the ways to keep it (copy, share) and the switch that says it was kept. Shown at setup and when the person makes a new one. */
@Composable
internal fun PasswordsRecoveryKeySections(model: PasswordsModel, recoveryKeyText: String, isSaved: Boolean, onSavedChange: (Boolean) -> Unit) {
    val context = LocalContext.current
    val label = stringResource(R.string.passwords_recovery_key)
    val subject = stringResource(R.string.passwords_recovery_key_subject)
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(stringResource(R.string.passwords_your_recovery_key), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        Text(PasswordsFormat.recoveryKeyLines(recoveryKeyText).joinToString("\n"), style = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(onClick = { model.copy(recoveryKeyText, label) }) {
                Icon(Icons.Outlined.ContentCopy, null, Modifier.size(18.dp)); Text(" " + stringResource(R.string.passwords_copy_recovery_key))
            }
            OutlinedButton(onClick = {
                val send = Intent(Intent.ACTION_SEND).apply { type = "text/plain"; putExtra(Intent.EXTRA_TEXT, recoveryKeyText); putExtra(Intent.EXTRA_SUBJECT, subject) }
                context.startActivity(Intent.createChooser(send, subject).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            }) {
                Icon(Icons.Outlined.Share, null, Modifier.size(18.dp)); Text(" " + stringResource(R.string.passwords_share_recovery_key))
            }
        }
        Text(stringResource(R.string.passwords_recovery_key_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth().clickable { onSavedChange(!isSaved) }) {
            Checkbox(isSaved, onCheckedChange = null)
            Text(stringResource(R.string.passwords_recovery_key_saved), modifier = Modifier.padding(start = 12.dp))
        }
        Text(stringResource(R.string.passwords_loss_sentence), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The end of the setup: the recovery key, shown once, and the way into Passwords once the person says they saved it. */
@Composable
private fun PasswordsRecoveryKeyScreen(model: PasswordsModel) {
    val scope = rememberCoroutineScope()
    var isSaved by remember { mutableStateOf(false) }
    var shouldRemember by remember { mutableStateOf(true) }
    var isFinishing by remember { mutableStateOf(false) }
    TeaNodeScreen(title = stringResource(R.string.passwords_recovery_key)) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
                PasswordsRecoveryKeySections(model, model.recoveryKeyText.orEmpty(), isSaved) { isSaved = it }
                if (model.canUseBiometry) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(stringResource(R.string.passwords_use_biometry_next_time, model.biometryName), modifier = Modifier.weight(1f))
                        Switch(shouldRemember, { shouldRemember = it })
                    }
                    Text(stringResource(R.string.passwords_biometry_footer, model.biometryName), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            PrimaryButton(stringResource(R.string.passwords_done), isFinishing, isSaved) {
                isFinishing = true
                scope.launch {
                    try { model.finishSetUp(shouldRemember && model.canUseBiometry) } finally { isFinishing = false }
                }
            }
        }
    }
}

/** A screen's one prominent action, full width at the bottom. */
@Composable
internal fun PrimaryButton(title: String, isWorking: Boolean = false, isEnabled: Boolean = true, onClick: () -> Unit) {
    Button(onClick = onClick, enabled = isEnabled && !isWorking, modifier = Modifier.fillMaxWidth().padding(16.dp)) {
        if (isWorking) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) else Text(title)
    }
}

// Unlocking

/** Closed Passwords: the passphrase (or the recovery key) opens it, and so does the fingerprint when this phone keeps the key. */
@Composable
private fun PasswordsUnlockScreen(model: PasswordsModel) {
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    var typedText by remember { mutableStateOf("") }
    var isUsingRecoveryKey by remember { mutableStateOf(false) }
    var shouldRemember by remember { mutableStateOf(true) }
    var isWorking by remember { mutableStateOf(false) }
    val recoveryFailed = stringResource(R.string.passwords_unlock_failed_recovery_key)
    val passphraseFailed = stringResource(R.string.passwords_unlock_failed_passphrase)

    fun unlock() {
        if (typedText.isEmpty() || isWorking) return
        isWorking = true
        scope.launch {
            try {
                if (isUsingRecoveryKey) model.unlockWithRecoveryKey(typedText)
                else model.unlock(typedText, shouldRemember && model.canUseBiometry && !model.hasBiometricKey)
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                model.report(error, if (isUsingRecoveryKey) recoveryFailed else passphraseFailed)
            } finally {
                isWorking = false
            }
        }
    }

    TeaNodeScreen(title = stringResource(R.string.passwords_title), actions = { SettingsAction() }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            Column(Modifier.weight(1f).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Column(Modifier.fillMaxWidth().padding(vertical = 8.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Icon(Icons.Outlined.Lock, null, Modifier.size(40.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(stringResource(R.string.passwords_locked_title), style = MaterialTheme.typography.titleMedium)
                }
                if (isUsingRecoveryKey) {
                    OutlinedTextField(
                        typedText, { typedText = it }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.passwords_recovery_key_field)) },
                        textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace),
                        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Characters, autoCorrectEnabled = false),
                    )
                    Text(stringResource(R.string.passwords_type_recovery_key_to_open), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                } else {
                    SecretField(typedText, { typedText = it }, stringResource(R.string.passwords_passphrase))
                    Text(stringResource(R.string.passwords_type_passphrase_to_open), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    if (model.canUseBiometry && !model.hasBiometricKey) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(stringResource(R.string.passwords_use_biometry_next_time, model.biometryName), modifier = Modifier.weight(1f))
                            Switch(shouldRemember, { shouldRemember = it })
                        }
                    }
                }
                if (model.hasBiometricKey) {
                    TextButton(onClick = { scope.launch { model.unlockWithBiometry() } }) {
                        Icon(Icons.Outlined.Fingerprint, null); Text(" " + stringResource(R.string.passwords_use_biometry, model.biometryName))
                    }
                }
                TextButton(onClick = { typedText = ""; isUsingRecoveryKey = !isUsingRecoveryKey }) {
                    Icon(Icons.Outlined.Key, null)
                    Text(" " + stringResource(if (isUsingRecoveryKey) R.string.passwords_use_the_passphrase else R.string.passwords_use_the_recovery_key))
                }
                Text(stringResource(R.string.passwords_locked_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Start)
            }
            PrimaryButton(stringResource(R.string.passwords_unlock), isWorking, typedText.isNotEmpty()) { unlock() }
        }
    }
    @Suppress("UNUSED_VARIABLE") val unusedNavigator = navigator
}
