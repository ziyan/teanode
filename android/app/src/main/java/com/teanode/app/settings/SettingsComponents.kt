package com.teanode.app.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.LoadState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.SnackbarController
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * ActionRunner runs what a button does: one at a time, with a flag a screen can show, and with the answer told in a
 * snackbar, never as text in the page.
 */
@Stable
class ActionRunner(private val scope: CoroutineScope, private val snackbar: SnackbarController) {
    var isWorking: Boolean by mutableStateOf(false)
        private set

    /** Runs [work]; says [successText] when it finishes, or why it failed. [onSuccess] runs after a success. */
    fun run(successText: String? = null, onSuccess: () -> Unit = {}, work: suspend () -> Unit) {
        if (isWorking) return
        isWorking = true
        scope.launch {
            try {
                work()
                if (successText != null) snackbar.show(successText)
                onSuccess()
            } catch (cancelled: CancellationException) {
                throw cancelled
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            } finally {
                isWorking = false
            }
        }
    }
}

@Composable
fun rememberActionRunner(): ActionRunner {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    return remember(scope, snackbar) { ActionRunner(scope, snackbar) }
}

/** A settings page: the frame, and a list of its rows. */
@Composable
fun SettingsPage(
    title: String,
    onBack: (() -> Unit)?,
    modifier: Modifier = Modifier,
    actions: @Composable RowScope.() -> Unit = {},
    floatingActionButton: @Composable () -> Unit = {},
    content: LazyListScope.() -> Unit,
) {
    TeaNodeScreen(title = title, modifier = modifier, onBack = onBack, actions = actions, floatingActionButton = floatingActionButton) { contentPadding ->
        LazyColumn(Modifier.padding(contentPadding), contentPadding = PaddingValues(bottom = 24.dp), content = content)
    }
}

/** A settings page whose rows come from one read of the server. */
@Composable
fun <Value> LoadedSettingsPage(
    title: String,
    onBack: (() -> Unit)?,
    state: LoadState<Value>,
    modifier: Modifier = Modifier,
    actions: @Composable RowScope.() -> Unit = {},
    floatingActionButton: @Composable () -> Unit = {},
    isEmpty: (Value) -> Boolean = { false },
    empty: @Composable () -> Unit = {},
    content: LazyListScope.(Value) -> Unit,
) {
    TeaNodeScreen(title = title, modifier = modifier, onBack = onBack, actions = actions, floatingActionButton = floatingActionButton) { contentPadding ->
        LoadedContent(state, Modifier.padding(contentPadding), isEmpty = isEmpty, empty = empty) { value ->
            LazyColumn(contentPadding = PaddingValues(bottom = 88.dp)) { content(value) }
        }
    }
}

/** The explanation under a group of rows. */
@Composable
fun FooterText(text: String, modifier: Modifier = Modifier) {
    Text(
        text, modifier = modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

/** A row with a switch at its end. */
@Composable
fun SwitchRow(headline: String, isChecked: Boolean, onChange: (Boolean) -> Unit, modifier: Modifier = Modifier, supporting: String? = null, isEnabled: Boolean = true) {
    TeaNodeRow(
        headline = headline, supporting = supporting, modifier = modifier,
        trailing = { Switch(checked = isChecked, onCheckedChange = onChange, enabled = isEnabled) },
        onClick = if (isEnabled) ({ onChange(!isChecked) }) else null,
    )
}

/** A text field of a form. */
@Composable
fun FormField(
    value: String,
    onChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    placeholder: String? = null,
    isPassword: Boolean = false,
    isSingleLine: Boolean = true,
    minLines: Int = 1,
    keyboardType: KeyboardType = KeyboardType.Text,
    supporting: String? = null,
    isError: Boolean = false,
) {
    OutlinedTextField(
        value = value, onValueChange = onChange, label = { Text(label) }, modifier = modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp),
        placeholder = placeholder?.let { text -> { Text(text) } }, singleLine = isSingleLine, minLines = minLines, isError = isError,
        supportingText = supporting?.let { text -> { Text(text) } },
        visualTransformation = if (isPassword) PasswordVisualTransformation() else VisualTransformation.None,
        keyboardOptions = KeyboardOptions(keyboardType = if (isPassword) KeyboardType.Password else keyboardType),
    )
}

/** A row that shows the choice made and opens a list of the choices. */
@Composable
fun <Choice> ChoiceRow(
    headline: String,
    choices: List<Choice>,
    selected: Choice,
    label: @Composable (Choice) -> String,
    onSelect: (Choice) -> Unit,
    modifier: Modifier = Modifier,
    isEnabled: Boolean = true,
) {
    var isOpen by remember { mutableStateOf(false) }
    TeaNodeRow(
        headline = headline, supporting = label(selected), modifier = modifier, onClick = if (isEnabled) ({ isOpen = true }) else null,
    )
    if (isOpen) {
        AlertDialog(
            onDismissRequest = { isOpen = false },
            title = { Text(headline) },
            text = {
                Column {
                    choices.forEach { choice ->
                        TeaNodeRow(
                            headline = label(choice), modifier = Modifier.clickable { isOpen = false; onSelect(choice) },
                            leading = { RadioButton(selected = choice == selected, onClick = null) },
                        )
                    }
                }
            },
            confirmButton = { TextButton(onClick = { isOpen = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/** A text button for the top bar that shows a spinner while its work is on. */
@Composable
fun BarTextAction(label: String, onClick: () -> Unit, isEnabled: Boolean = true, isWorking: Boolean = false) {
    if (isWorking) {
        CircularProgressIndicator(Modifier.padding(horizontal = 16.dp), strokeWidth = 2.dp)
    } else {
        TextButton(onClick = onClick, enabled = isEnabled) { Text(label) }
    }
}

/** Whether a form dialog is drawn in place of the screen instead of in a window of its own. Screenshot tests set it, since a picture of the screen leaves a window out. */
val LocalDialogsInline = androidx.compose.runtime.staticCompositionLocalOf { false }

/** A dialog with a form: a title, the fields, and a confirming button that stays off until the form can be sent. */
@Composable
fun FormDialog(
    title: String,
    confirmLabel: String,
    isConfirmEnabled: Boolean,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    isWorking: Boolean = false,
    content: @Composable () -> Unit,
) {
    if (LocalDialogsInline.current) {
        androidx.compose.foundation.layout.Box(
            Modifier.fillMaxSize().background(androidx.compose.ui.graphics.Color.Black.copy(alpha = 0.32f)).padding(24.dp),
            contentAlignment = androidx.compose.ui.Alignment.Center,
        ) {
            androidx.compose.material3.Surface(shape = MaterialTheme.shapes.extraLarge, tonalElevation = 6.dp, modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.padding(24.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(title, style = MaterialTheme.typography.headlineSmall)
                    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) { content() }
                    androidx.compose.foundation.layout.Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                        TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                        TextButton(onClick = onConfirm, enabled = isConfirmEnabled && !isWorking) { Text(confirmLabel) }
                    }
                }
            }
        }
        return
    }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { Column(verticalArrangement = Arrangement.spacedBy(4.dp)) { content() } },
        confirmButton = { TextButton(onClick = onConfirm, enabled = isConfirmEnabled && !isWorking) { Text(confirmLabel) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
