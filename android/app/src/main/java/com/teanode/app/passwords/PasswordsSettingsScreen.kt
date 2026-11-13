package com.teanode.app.passwords

import android.content.Intent
import android.provider.Settings
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
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.DeleteForever
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.LockReset
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.passwords.PasswordsFormat
import com.teanode.kit.passwords.PasswordsPassphraseStrength
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

private enum class SettingsSheet { Biometry, ChangePassphrase, ReplaceRecoveryKey }

/** The settings of Passwords: when it locks, the fingerprint, the passphrase and the recovery key, how to turn on autofill, and erasing it. */
@Composable
fun PasswordsSettingsScreen() {
    val model = rememberPasswordsModel()
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var sheet by remember { mutableStateOf<SettingsSheet?>(null) }
    var isConfirmingErase by remember { mutableStateOf(false) }
    var isErasing by remember { mutableStateOf(false) }
    var isLockMenuOpen by remember { mutableStateOf(false) }
    val isOpen = model.phase == PasswordsPhase.Unlocked

    TeaNodeScreen(title = stringResource(R.string.passwords_settings), onBack = { navigator.back() }) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState())) {
            if (isOpen) {
                Header(stringResource(R.string.passwords_lock_header))
                Box {
                    Row(Modifier.fillMaxWidth().clickable { isLockMenuOpen = true }.padding(16.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text(stringResource(R.string.passwords_lock_after))
                        Text(lockAfterText(model.lockAfterMinutes), color = MaterialTheme.colorScheme.primary)
                    }
                    DropdownMenu(isLockMenuOpen, { isLockMenuOpen = false }) {
                        (PasswordsFormat.lockAfterChoices + model.lockAfterMinutes).toSortedSet().forEach { minutes ->
                            DropdownMenuItem(text = { Text(lockAfterText(minutes)) }, onClick = {
                                isLockMenuOpen = false
                                scope.launch {
                                    try { model.updateLockAfterMinutes(minutes); model.say(context.getString(R.string.passwords_lock_saved)) } catch (cancelled: CancellationException) { throw cancelled } catch (error: Throwable) { model.report(error, context.getString(R.string.passwords_setting_not_saved)) }
                                }
                            })
                        }
                    }
                }
                Footer(stringResource(R.string.passwords_lock_footer, model.biometryName))
                HorizontalDivider()
                Row(Modifier.fillMaxWidth().padding(16.dp, 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(stringResource(R.string.passwords_open_with_biometry, model.biometryName), modifier = Modifier.weight(1f))
                    // Turning it off is always possible while a key is kept, even when the sensor has gone from this phone.
                    Switch(model.hasBiometricKey, enabled = model.canUseBiometry || model.hasBiometricKey, onCheckedChange = { isOn -> if (isOn) sheet = SettingsSheet.Biometry else model.disableBiometry() })
                }
                Footer(
                    when {
                        model.canUseBiometry -> stringResource(R.string.passwords_biometry_toggle_footer, model.biometryName, model.biometryName)
                        model.hasBiometricKey -> stringResource(R.string.passwords_biometry_gone, model.biometryName)
                        else -> stringResource(R.string.passwords_biometry_not_set_up, model.biometryName)
                    },
                )
                HorizontalDivider()
                Header(stringResource(R.string.passwords_keys_header))
                Action(Icons.Outlined.LockReset, stringResource(R.string.passwords_change_passphrase)) { sheet = SettingsSheet.ChangePassphrase }
                Action(Icons.Outlined.Sync, stringResource(R.string.passwords_replace_recovery_key)) { sheet = SettingsSheet.ReplaceRecoveryKey }
                Footer(stringResource(R.string.passwords_keys_footer))
                model.ownSafetyCode?.let { code ->
                    HorizontalDivider()
                    Header(stringResource(R.string.passwords_safety_code_header))
                    SelectionContainer { Text(code, modifier = Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace)) }
                    Footer(stringResource(R.string.passwords_safety_code_footer))
                }
            } else {
                Text(stringResource(R.string.passwords_settings_open_first), color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
            }
            HorizontalDivider()
            Header(stringResource(R.string.passwords_autofill_header))
            Text(stringResource(R.string.passwords_autofill_description), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            Action(Icons.Outlined.Settings, stringResource(R.string.passwords_autofill_open_settings)) {
                runCatching { context.startActivity(Intent(Settings.ACTION_REQUEST_SET_AUTOFILL_SERVICE).setData(android.net.Uri.parse("package:${context.packageName}")).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
            }
            HorizontalDivider()
            Row(Modifier.fillMaxWidth().clickable(enabled = !isErasing) { isConfirmingErase = true }.padding(16.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                Icon(Icons.Outlined.DeleteForever, null, tint = MaterialTheme.colorScheme.error)
                Text(stringResource(R.string.passwords_erase), color = MaterialTheme.colorScheme.error)
            }
            Footer(stringResource(R.string.passwords_erase_footer))
        }
    }
    if (isConfirmingErase) {
        ConfirmationDialog(
            title = stringResource(R.string.passwords_erase_title), text = stringResource(R.string.passwords_erase_message), confirmLabel = stringResource(R.string.passwords_erase_everything), isDestructive = true,
            onConfirm = {
                isConfirmingErase = false
                isErasing = true
                scope.launch {
                    try { model.erase() } catch (cancelled: CancellationException) { throw cancelled } catch (error: Throwable) { model.report(error, context.getString(R.string.passwords_erase_failed)) } finally { isErasing = false }
                }
            },
            onDismiss = { isConfirmingErase = false },
        )
    }
    when (sheet) {
        SettingsSheet.Biometry -> ConfirmPassphraseDialog(
            model, stringResource(R.string.passwords_open_with_biometry, model.biometryName), stringResource(R.string.passwords_biometry_sheet_explanation, model.biometryName), stringResource(R.string.passwords_turn_on),
            onDismiss = { sheet = null },
        ) { passphrase -> model.enableBiometry(passphrase) }
        SettingsSheet.ChangePassphrase -> ChangePassphraseDialog(model, onDismiss = { sheet = null })
        SettingsSheet.ReplaceRecoveryKey -> ReplaceRecoveryKeyDialog(model, onDismiss = { sheet = null })
        null -> Unit
    }
}

@Composable
private fun Header(text: String) {
    Text(text, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(16.dp, 16.dp, 16.dp, 4.dp))
}

@Composable
private fun Action(icon: androidx.compose.ui.graphics.vector.ImageVector, title: String, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(16.dp), horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        Icon(icon, null, tint = MaterialTheme.colorScheme.primary); Text(title, color = MaterialTheme.colorScheme.primary)
    }
}

/** "15 minutes", "1 hour", "1 day". */
@Composable
internal fun lockAfterText(minutes: Int): String = when (minutes) {
    1 -> stringResource(R.string.passwords_lock_1_minute)
    5 -> stringResource(R.string.passwords_lock_5_minutes)
    15 -> stringResource(R.string.passwords_lock_15_minutes)
    30 -> stringResource(R.string.passwords_lock_30_minutes)
    60 -> stringResource(R.string.passwords_lock_1_hour)
    240 -> stringResource(R.string.passwords_lock_4_hours)
    1440 -> stringResource(R.string.passwords_lock_1_day)
    10080 -> stringResource(R.string.passwords_lock_1_week)
    else -> pluralStringResource(R.plurals.passwords_lock_minutes, minutes, minutes)
}

/** Asks for the passphrase before something that needs it (the fingerprint, a new passphrase). */
@Composable
private fun ConfirmPassphraseDialog(model: PasswordsModel, title: String, explanation: String, actionTitle: String, onDismiss: () -> Unit, perform: suspend (String) -> Unit) {
    var passphrase by remember { mutableStateOf("") }
    var isWorking by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    AlertDialog(
        onDismissRequest = { if (!isWorking) onDismiss() }, title = { Text(title) },
        text = { Column(verticalArrangement = Arrangement.spacedBy(8.dp)) { SecretField(passphrase, { passphrase = it }, stringResource(R.string.passwords_passphrase)); Text(explanation, style = MaterialTheme.typography.bodySmall) } },
        confirmButton = {
            if (isWorking) CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
            else TextButton(enabled = passphrase.isNotEmpty(), onClick = {
                isWorking = true
                scope.launch {
                    try { perform(passphrase); onDismiss() } catch (cancelled: CancellationException) { throw cancelled } catch (error: Throwable) { model.report(error, context.getString(R.string.passwords_setting_not_saved)) } finally { isWorking = false }
                }
            }) { Text(actionTitle) }
        },
        dismissButton = { TextButton(onClick = onDismiss, enabled = !isWorking) { Text(stringResource(R.string.passwords_cancel)) } },
    )
}

@Composable
private fun ChangePassphraseDialog(model: PasswordsModel, onDismiss: () -> Unit) {
    var currentPassphrase by remember { mutableStateOf("") }
    var recoveryKeyText by remember { mutableStateOf("") }
    var isUsingRecoveryKey by remember { mutableStateOf(false) }
    var newPassphrase by remember { mutableStateOf("") }
    var repeated by remember { mutableStateOf("") }
    var isWorking by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val isLongEnough = newPassphrase.codePointCount(0, newPassphrase.length) >= PasswordsPassphraseStrength.MINIMUM_LENGTH
    val hasProof = if (isUsingRecoveryKey) recoveryKeyText.isNotEmpty() else currentPassphrase.isNotEmpty()
    AlertDialog(
        onDismissRequest = { if (!isWorking) onDismiss() }, title = { Text(stringResource(R.string.passwords_change_passphrase)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (isUsingRecoveryKey) {
                    OutlinedTextField(recoveryKeyText, { recoveryKeyText = it }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.passwords_recovery_key_field)) }, textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace), keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Characters, autoCorrectEnabled = false))
                    Text(stringResource(R.string.passwords_recovery_key_proof_footer), style = MaterialTheme.typography.bodySmall)
                } else {
                    SecretField(currentPassphrase, { currentPassphrase = it }, stringResource(R.string.passwords_current_passphrase))
                }
                TextButton(onClick = { isUsingRecoveryKey = !isUsingRecoveryKey }, enabled = !isWorking) {
                    Icon(Icons.Outlined.Key, null); Text(" " + stringResource(if (isUsingRecoveryKey) R.string.passwords_use_current_passphrase else R.string.passwords_use_recovery_key_instead))
                }
                SecretField(newPassphrase, { newPassphrase = it }, stringResource(R.string.passwords_new_passphrase))
                SecretField(repeated, { repeated = it }, stringResource(R.string.passwords_type_it_again))
                if (repeated.isNotEmpty() && newPassphrase != repeated) Text(stringResource(R.string.passwords_passphrases_differ), style = MaterialTheme.typography.bodySmall)
                Text(stringResource(R.string.passwords_min_length_footer), style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            if (isWorking) CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
            else TextButton(enabled = hasProof && isLongEnough && newPassphrase == repeated, onClick = {
                isWorking = true
                scope.launch {
                    try {
                        if (isUsingRecoveryKey) model.changePassphraseWithRecoveryKey(recoveryKeyText, newPassphrase) else model.changePassphrase(currentPassphrase, newPassphrase)
                        model.say(context.getString(R.string.passwords_passphrase_changed))
                        onDismiss()
                    } catch (cancelled: CancellationException) { throw cancelled } catch (error: Throwable) { model.report(error, context.getString(R.string.passwords_passphrase_change_failed)) } finally { isWorking = false }
                }
            }) { Text(stringResource(R.string.passwords_change_button)) }
        },
        dismissButton = { TextButton(onClick = onDismiss, enabled = !isWorking) { Text(stringResource(R.string.passwords_cancel)) } },
    )
}

@Composable
private fun ReplaceRecoveryKeyDialog(model: PasswordsModel, onDismiss: () -> Unit) {
    var passphrase by remember { mutableStateOf("") }
    var newRecoveryKeyText by remember { mutableStateOf<String?>(null) }
    var isSaved by remember { mutableStateOf(false) }
    var isWorking by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val shownKey = newRecoveryKeyText
    AlertDialog(
        // The new key is shown once: it cannot be dismissed before it is kept.
        onDismissRequest = { if (!isWorking && shownKey == null) onDismiss() }, title = { Text(stringResource(R.string.passwords_replace_recovery_key)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (shownKey != null) {
                    PasswordsRecoveryKeySections(model, shownKey, isSaved) { isSaved = it }
                } else {
                    SecretField(passphrase, { passphrase = it }, stringResource(R.string.passwords_passphrase))
                    Text(stringResource(R.string.passwords_replace_key_footer), style = MaterialTheme.typography.bodySmall)
                }
            }
        },
        confirmButton = {
            if (isWorking) CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
            else if (shownKey == null) TextButton(enabled = passphrase.isNotEmpty(), onClick = {
                isWorking = true
                scope.launch {
                    try { newRecoveryKeyText = model.replaceRecoveryKey(passphrase); passphrase = "" } catch (cancelled: CancellationException) { throw cancelled } catch (error: Throwable) { model.report(error, context.getString(R.string.passwords_recovery_key_replace_failed)) } finally { isWorking = false }
                }
            }) { Text(stringResource(R.string.passwords_continue_button)) }
            else TextButton(enabled = isSaved, onClick = { model.say(context.getString(R.string.passwords_recovery_key_replaced)); onDismiss() }) { Text(stringResource(R.string.passwords_done)) }
        },
        dismissButton = { if (shownKey == null) TextButton(onClick = onDismiss, enabled = !isWorking) { Text(stringResource(R.string.passwords_cancel)) } },
    )
}
