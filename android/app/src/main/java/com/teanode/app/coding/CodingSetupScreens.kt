package com.teanode.app.coding

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
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
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeDivider
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.coding.HerdrHooksSetup
import com.teanode.kit.coding.codingContextPreview
import com.teanode.kit.coding.openHerdrSession
import com.teanode.kit.coding.setUpHerdrHooks
import kotlinx.coroutines.launch

/** Puts teanode's reporting hooks into Claude Code on each computer, or takes them out: the dashboard's "Reporting hooks" dialog. */
@Composable
fun CodingHooksScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val model = rememberCodingModel(connection)
    val scope = rememberCoroutineScope()
    var busyComputerName by remember { mutableStateOf<String?>(null) }
    var removingComputerName by remember { mutableStateOf<String?>(null) }
    val setups = remember { mutableStateMapOf<String, HerdrHooksSetup>() }

    fun setUp(computer: String, isRemoval: Boolean) {
        busyComputerName = computer
        scope.launch {
            try {
                val setup = connection.setUpHerdrHooks(computer, isRemoval)
                setups[computer] = setup
                snackbar.show(
                    if (setup.isInstalled) context.getString(R.string.coding_hooks_installed_on, setup.computer, setup.settingsPath)
                    else context.getString(R.string.coding_hooks_removed_on, setup.computer, setup.settingsPath),
                )
            } catch (error: Throwable) {
                snackbar.showError(error)
            } finally {
                busyComputerName = null
            }
        }
    }

    TeaNodeScreen(title = stringResource(R.string.coding_hooks_title), onBack = onBack) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState())) {
            Text(
                stringResource(R.string.coding_hooks_explainer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(16.dp),
            )
            model.computerNames.forEach { computer ->
                Row(Modifier.fillMaxWidth().padding(end = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                    Box(Modifier.weight(1f)) { TeaNodeSectionHeader(computer) }
                    if (busyComputerName == computer) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                }
                Row(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onClick = { setUp(computer, false) }, enabled = busyComputerName == null) { Text(stringResource(R.string.coding_hooks_install)) }
                    OutlinedButton(onClick = { removingComputerName = computer }, enabled = busyComputerName == null) {
                        Text(stringResource(R.string.coding_hooks_remove), color = MaterialTheme.colorScheme.error)
                    }
                }
                setups[computer]?.let { setup -> CodingHooksReport(setup) }
                TeaNodeDivider(Modifier.padding(top = 16.dp))
            }
        }
    }

    removingComputerName?.let { computer ->
        ConfirmationDialog(
            title = stringResource(R.string.coding_hooks_remove_title), text = stringResource(R.string.coding_hooks_remove_message, computer),
            confirmLabel = stringResource(R.string.coding_hooks_remove), isDestructive = true,
            onConfirm = { removingComputerName = null; setUp(computer, true) }, onDismiss = { removingComputerName = null },
        )
    }
}

/** What the last setup on a computer reported: whether the hooks are in, where, which events, and the copy of the file it kept. */
@Composable
private fun CodingHooksReport(setup: HerdrHooksSetup) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        LabeledValue(stringResource(R.string.coding_hooks_label), stringResource(if (setup.isInstalled) R.string.coding_hooks_installed else R.string.coding_hooks_removed))
        PathValue(stringResource(R.string.coding_hooks_settings_file), setup.settingsPath)
        if (setup.isInstalled) PathValue(stringResource(R.string.coding_hooks_script), setup.scriptPath)
        PathValue(stringResource(R.string.coding_hooks_backup), setup.backupPath)
        if (setup.hookEventNames.isNotEmpty()) LabeledValue(stringResource(R.string.coding_hooks_events), setup.hookEventNames.joinToString(", "))
    }
}

@Composable
private fun LabeledValue(label: String, value: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, style = MaterialTheme.typography.bodyMedium)
        Text(value, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
private fun PathValue(title: String, path: String) {
    if (path.isEmpty()) return
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(title, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        SelectionContainer { Text(path, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace)) }
    }
}

/**
 * Shows the text a coding session would be shown: in a directory on a computer, what a session starting there sees, and
 * with a question, what that question recalls from the directory's project. Nothing is said to a model and nothing is
 * marked as used.
 */
@Composable
fun CodingPreviewScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val model = rememberCodingModel(connection)
    val scope = rememberCoroutineScope()
    var computerName by remember { mutableStateOf("") }
    var directory by remember { mutableStateOf("") }
    var question by remember { mutableStateOf("") }
    var shownText by remember { mutableStateOf<String?>(null) }
    var isAsking by remember { mutableStateOf(false) }

    fun ask() {
        isAsking = true
        scope.launch {
            try {
                val preview = connection.codingContextPreview(directory.trim(), computerName.ifEmpty { null }, question.trim().ifEmpty { null })
                shownText = preview.text
            } catch (error: Throwable) {
                snackbar.showError(error)
            } finally {
                isAsking = false
            }
        }
    }

    TeaNodeScreen(title = stringResource(R.string.coding_preview_title), onBack = onBack) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedTextField(
                directory, { directory = it; shownText = null }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.coding_directory)) },
                placeholder = { Text("~/code/project") }, singleLine = true, textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace),
                keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false),
            )
            if (model.computerNames.size > 1) {
                Text(stringResource(R.string.coding_preview_computer_optional), style = MaterialTheme.typography.labelLarge)
                Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    FilterChip(selected = computerName.isEmpty(), onClick = { computerName = ""; shownText = null }, label = { Text(stringResource(R.string.coding_preview_any_computer)) })
                    model.computerNames.forEach { name ->
                        FilterChip(selected = computerName == name, onClick = { computerName = name; shownText = null }, label = { Text(name) })
                    }
                }
            }
            OutlinedTextField(
                question, { question = it; shownText = null }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.coding_preview_question_optional)) },
                minLines = 1, maxLines = 4,
            )
            Text(stringResource(R.string.coding_preview_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Button(onClick = { ask() }, enabled = directory.trim().isNotEmpty() && !isAsking, modifier = Modifier.fillMaxWidth()) {
                if (isAsking) CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp) else Text(stringResource(R.string.coding_preview_show))
            }
            shownText?.let { text ->
                Text(stringResource(R.string.coding_preview_shown_heading), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                if (text.isEmpty()) {
                    Text(stringResource(R.string.coding_preview_nothing), color = MaterialTheme.colorScheme.onSurfaceVariant)
                } else {
                    SelectionContainer { Text(text, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace)) }
                }
            }
        }
    }
}

/** Starts Claude Code or Codex in a new herdr pane, in a directory on one of the person's computers. */
@Composable
fun CodingOpenScreen(onBack: () -> Unit, onOpened: (computer: String, paneId: String) -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val model = rememberCodingModel(connection)
    val scope = rememberCoroutineScope()
    val computerNames = model.computerNames
    var computer by remember { mutableStateOf("") }
    var codingAgentKind by remember { mutableStateOf("claude") }
    var directory by remember { mutableStateOf("") }
    var agentName by remember { mutableStateOf("") }
    var shouldSkipPermissions by remember { mutableStateOf(false) }
    var isOpening by remember { mutableStateOf(false) }
    val chosenComputer = computer.ifEmpty { computerNames.firstOrNull() ?: "" }

    fun open() {
        isOpening = true
        scope.launch {
            try {
                val opened = connection.openHerdrSession(
                    chosenComputer, directory = directory.trim(), codingAgentKind = codingAgentKind,
                    agentName = agentName.trim().ifEmpty { null }, shouldSkipPermissions = shouldSkipPermissions,
                )
                model.upsert(opened)
                onOpened(opened.computer, opened.paneId)
            } catch (error: Throwable) {
                snackbar.showError(error)
            } finally {
                isOpening = false
            }
        }
    }

    TeaNodeScreen(
        title = stringResource(R.string.coding_open_title), onBack = onBack,
        actions = {
            if (isOpening) CircularProgressIndicator(Modifier.padding(end = 16.dp).size(20.dp), strokeWidth = 2.dp)
            else TextButton(onClick = { open() }, enabled = directory.trim().isNotEmpty() && chosenComputer.isNotEmpty()) { Text(stringResource(R.string.coding_open)) }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(16.dp)) {
            if (computerNames.size > 1) {
                Text(stringResource(R.string.coding_computer), style = MaterialTheme.typography.labelLarge)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    computerNames.forEach { name -> FilterChip(selected = chosenComputer == name, onClick = { computer = name }, label = { Text(name) }) }
                }
            }
            Text(stringResource(R.string.coding_coding_agent), style = MaterialTheme.typography.labelLarge)
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                listOf("claude" to "Claude Code", "codex" to "Codex").forEachIndexed { index, (kind, name) ->
                    SegmentedButton(selected = codingAgentKind == kind, onClick = { codingAgentKind = kind }, shape = SegmentedButtonDefaults.itemShape(index, 2)) { Text(name) }
                }
            }
            OutlinedTextField(
                directory, { directory = it }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.coding_directory)) }, placeholder = { Text("~/src/example") },
                singleLine = true, textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace),
                keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false),
            )
            OutlinedTextField(
                agentName, { agentName = it }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.coding_name_in_herdr)) }, singleLine = true,
                keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false),
            )
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Text(
                    stringResource(if (codingAgentKind == "codex") R.string.coding_skip_approvals_codex else R.string.coding_skip_prompts_claude),
                    modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium,
                )
                Switch(shouldSkipPermissions, { shouldSkipPermissions = it })
            }
        }
    }
}
