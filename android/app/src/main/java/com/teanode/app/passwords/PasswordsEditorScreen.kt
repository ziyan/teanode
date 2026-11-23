package com.teanode.app.passwords

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoFixHigh
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Slider
import androidx.compose.material3.Switch
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.passwords.PasswordsDraft
import com.teanode.kit.passwords.PasswordsDraftException
import com.teanode.kit.passwords.PasswordsError
import com.teanode.kit.passwords.PasswordsGenerator
import com.teanode.kit.passwords.PasswordsGeneratorOptions
import com.teanode.kit.passwords.PasswordsItemKind
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** An editor on screen: a new item of a kind (`itemId` null), or a change to an item the person owns. */
@Composable
fun PasswordsEditorScreen(itemKind: PasswordsItemKind, itemId: String?, isGeneratorOpen: Boolean = false) {
    val model = rememberPasswordsModel()
    val navigator = LocalAppNavigator.current
    GoBackWhenClosed(model)
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val editing = itemId?.let { id -> model.item(id) }
    var draft by remember { mutableStateOf(PasswordsDraft.of(editing)) }
    var loadedDraft by remember { mutableStateOf(PasswordsDraft.of(editing)) }
    var baseRevision by remember { mutableStateOf(editing?.revision) }
    var isSaving by remember { mutableStateOf(false) }
    var isGenerating by remember { mutableStateOf(isGeneratorOpen) }
    var isPasswordShown by remember { mutableStateOf(false) }

    val title = when (itemKind) {
        PasswordsItemKind.Login -> stringResource(if (editing == null) R.string.passwords_new_login else R.string.passwords_edit_login)
        PasswordsItemKind.Card -> stringResource(if (editing == null) R.string.passwords_new_card else R.string.passwords_edit_card)
        PasswordsItemKind.Note -> stringResource(if (editing == null) R.string.passwords_new_note else R.string.passwords_edit_note)
    }

    /** Another device changed the item while the person edited: their edits go onto the fresh copy and they check them first. */
    fun rebaseOntoFreshItem() {
        val fresh = itemId?.let { id -> model.item(id) }
        if (fresh == null || fresh.isDeleted) {
            model.snackbar?.showProblem(model.problemText(PasswordsError.ItemNotFound, ""))
            return
        }
        if (fresh.revision == baseRevision) {
            model.snackbar?.showProblem(context.getString(R.string.passwords_item_changed_unreadable))
            return
        }
        val freshDraft = PasswordsDraft.of(fresh)
        draft = draft.rebased(loadedDraft, freshDraft)
        loadedDraft = freshDraft
        baseRevision = fresh.revision
        model.snackbar?.showProblem(context.getString(R.string.passwords_item_changed_kept))
    }

    fun save() {
        isSaving = true
        scope.launch {
            try {
                val fields = draft.fields(itemKind)
                val saved = model.save(fields, editing?.id, baseRevision)
                model.say(context.getString(R.string.passwords_saved, saved.name))
                navigator.back()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (error: PasswordsDraftException) {
                model.snackbar?.showProblem(context.getString(R.string.passwords_one_time_code_invalid))
            } catch (error: PasswordsError.ItemChangedOnAnotherDevice) {
                rebaseOntoFreshItem()
            } catch (error: Throwable) {
                model.report(error, context.getString(R.string.passwords_save_failed))
            } finally {
                isSaving = false
            }
        }
    }

    TeaNodeScreen(
        title = title, onBack = { navigator.back() },
        actions = {
            if (isSaving) CircularProgressIndicator(Modifier.padding(end = 16.dp).size(20.dp), strokeWidth = 2.dp)
            else TextButton(onClick = { save() }, enabled = draft.name.trim().isNotEmpty()) { Text(stringResource(R.string.passwords_save)) }
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            val noAutoCorrect = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false)
            when (itemKind) {
                PasswordsItemKind.Login -> {
                    Field(draft.name, { draft = draft.copy(name = it) }, stringResource(R.string.passwords_item_name))
                    Field(draft.userName, { draft = draft.copy(userName = it) }, stringResource(R.string.passwords_user_name), keyboardOptions = noAutoCorrect)
                    OutlinedTextField(
                        value = draft.password, onValueChange = { draft = draft.copy(password = it) }, modifier = Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.passwords_password)) }, singleLine = true,
                        visualTransformation = if (isPasswordShown) androidx.compose.ui.text.input.VisualTransformation.None else androidx.compose.ui.text.input.PasswordVisualTransformation(),
                        textStyle = if (isPasswordShown) MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace) else MaterialTheme.typography.bodyLarge,
                        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false, keyboardType = KeyboardType.Password),
                        trailingIcon = {
                            IconButton(onClick = { isPasswordShown = !isPasswordShown }) {
                                Icon(if (isPasswordShown) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, stringResource(if (isPasswordShown) R.string.passwords_hide_password else R.string.passwords_show_password))
                            }
                        },
                    )
                    OutlinedButton(onClick = { isGenerating = true }) { Icon(Icons.Outlined.AutoFixHigh, null); Text(" " + stringResource(R.string.passwords_generate_a_password)) }
                    Field(draft.sitesText, { draft = draft.copy(sitesText = it) }, stringResource(R.string.passwords_sites), minLines = 1, maxLines = 5, placeholder = stringResource(R.string.passwords_one_address_each_line), keyboardOptions = noAutoCorrect)
                    Hint(stringResource(R.string.passwords_sites_footer))
                    Field(draft.oneTimeCodeText, { draft = draft.copy(oneTimeCodeText = it) }, stringResource(R.string.passwords_one_time_code), keyboardOptions = noAutoCorrect)
                    Hint(stringResource(R.string.passwords_one_time_code_footer))
                    Field(draft.notes, { draft = draft.copy(notes = it) }, stringResource(R.string.passwords_notes), minLines = 2, maxLines = 8)
                }
                PasswordsItemKind.Card -> {
                    Field(draft.name, { draft = draft.copy(name = it) }, stringResource(R.string.passwords_item_name))
                    Field(draft.cardholderName, { draft = draft.copy(cardholderName = it) }, stringResource(R.string.passwords_name_on_card))
                    Field(draft.number, { draft = draft.copy(number = it) }, stringResource(R.string.passwords_card_number), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
                    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        Field(draft.expiryMonth, { draft = draft.copy(expiryMonth = it) }, stringResource(R.string.passwords_expiry_month), Modifier.weight(1f), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
                        Field(draft.expiryYear, { draft = draft.copy(expiryYear = it) }, stringResource(R.string.passwords_expiry_year), Modifier.weight(1f), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number))
                    }
                    SecretField(draft.securityCode, { draft = draft.copy(securityCode = it) }, stringResource(R.string.passwords_security_code))
                    Field(draft.brand, { draft = draft.copy(brand = it) }, stringResource(R.string.passwords_card_brand))
                    Hint(stringResource(R.string.passwords_card_footer))
                    Field(draft.notes, { draft = draft.copy(notes = it) }, stringResource(R.string.passwords_notes), minLines = 2, maxLines = 8)
                }
                PasswordsItemKind.Note -> {
                    Field(draft.name, { draft = draft.copy(name = it) }, stringResource(R.string.passwords_item_name))
                    Field(draft.text, { draft = draft.copy(text = it) }, stringResource(R.string.passwords_note), minLines = 5, maxLines = 15)
                }
            }
            Field(draft.folder, { draft = draft.copy(folder = it) }, stringResource(R.string.passwords_folder))
            Hint(stringResource(R.string.passwords_folder_footer))
        }
    }
    if (isGenerating) {
        PasswordsGeneratorSheet(model, onUse = { generated -> draft = draft.copy(password = generated); isPasswordShown = true }, onDismiss = { isGenerating = false })
    }
}


@Composable
private fun Field(value: String, onValueChange: (String) -> Unit, label: String, modifier: Modifier = Modifier, minLines: Int = 1, maxLines: Int = 1, placeholder: String? = null, keyboardOptions: KeyboardOptions = KeyboardOptions.Default) {
    OutlinedTextField(
        value = value, onValueChange = onValueChange, modifier = modifier.fillMaxWidth(), label = { Text(label) }, placeholder = placeholder?.let { hint -> { Text(hint) } },
        minLines = minLines, maxLines = maxLines, singleLine = maxLines == 1, keyboardOptions = keyboardOptions,
    )
}

@Composable
private fun Hint(text: String) {
    Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

/** The password generator: what the person chooses for a new password, and the password it makes. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun PasswordsGeneratorSheet(model: PasswordsModel, onUse: (String) -> Unit, onDismiss: () -> Unit) {
    var options by remember { mutableStateOf(PasswordsGeneratorOptions.standard) }
    var generated by remember { mutableStateOf("") }
    val canGenerate = options.kindCount > 0
    val label = stringResource(R.string.passwords_generated_password)
    fun generate() {
        generated = runCatching { PasswordsGenerator.generate(options) }.getOrDefault("")
    }
    LaunchedEffect(options) { generate() }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 24.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.passwords_generator_title), style = MaterialTheme.typography.titleLarge)
            Text(stringResource(R.string.passwords_generated_password), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(generated, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace))
                IconButton(onClick = { model.copy(generated, label) }, enabled = generated.isNotEmpty()) { Icon(Icons.Outlined.ContentCopy, stringResource(R.string.passwords_copy)) }
            }
            OutlinedButton(onClick = { generate() }, enabled = canGenerate) { Icon(Icons.Outlined.Refresh, null); Text(" " + stringResource(R.string.passwords_another_password)) }
            if (!canGenerate) Hint(stringResource(R.string.passwords_choose_one_kind))
            Text(stringResource(R.string.passwords_length_label, options.length))
            Slider(value = options.length.toFloat(), onValueChange = { options = options.copy(length = it.toInt()) }, valueRange = 8f..PasswordsGenerator.MAXIMUM_LENGTH.toFloat())
            ToggleRow(stringResource(R.string.passwords_letters), options.shouldIncludeLetters) { options = options.copy(shouldIncludeLetters = it) }
            ToggleRow(stringResource(R.string.passwords_digits), options.shouldIncludeDigits) { options = options.copy(shouldIncludeDigits = it) }
            ToggleRow(stringResource(R.string.passwords_symbols), options.shouldIncludeSymbols) { options = options.copy(shouldIncludeSymbols = it) }
            ToggleRow(stringResource(R.string.passwords_look_alikes), options.shouldAvoidLookAlikes) { options = options.copy(shouldAvoidLookAlikes = it) }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.passwords_cancel)) }
                Button(onClick = { onUse(generated); onDismiss() }, enabled = generated.isNotEmpty() && canGenerate) { Text(stringResource(R.string.passwords_use)) }
            }
        }
    }
}

@Composable
private fun ToggleRow(title: String, isOn: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(title, modifier = Modifier.weight(1f))
        Switch(isOn, onChange)
    }
}
