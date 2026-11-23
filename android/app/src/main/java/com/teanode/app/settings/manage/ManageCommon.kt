package com.teanode.app.settings.manage

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.settings.BarTextAction
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.FormField
import com.teanode.app.settings.rememberActionRunner
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.AgentServerSettings
import com.teanode.kit.manage.agentServerSettings
import com.teanode.kit.manage.updateAgentServerSettings
import kotlinx.serialization.json.JsonObject
import java.text.NumberFormat

/**
 * One card of the operator's agent settings as a screen: its fields filled from what is stored, and a Save in the top bar
 * that sends the section and goes back. [content] draws the fields from the draft and says each change.
 */
@Composable
fun <Draft : Any> AgentSettingsFormScreen(
    title: String,
    onBack: () -> Unit,
    makeDraft: (AgentServerSettings) -> Draft,
    parameters: (Draft) -> JsonObject,
    content: @Composable (draft: Draft, onChange: (Draft) -> Unit, settings: AgentServerSettings) -> Unit,
) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.agentServerSettings() }
    val settings = loaded.value
    var draft by remember(settings) { mutableStateOf(settings?.let(makeDraft)) }
    val hasChanges = settings != null && draft != null && draft != makeDraft(settings)
    val savedText = stringResource(R.string.settings_saved)
    TeaNodeScreen(
        title = title, onBack = onBack,
        actions = {
            val current = draft
            if (settings != null && current != null) {
                BarTextAction(stringResource(R.string.settings_save), onClick = { actions.run(savedText, onSuccess = onBack) { connection.updateAgentServerSettings(parameters(current)) } }, isEnabled = hasChanges, isWorking = actions.isWorking)
            }
        },
    ) { contentPadding ->
        LoadedContent(loaded, Modifier.padding(contentPadding)) { found ->
            val current = draft
            if (current != null) Column(Modifier.verticalScroll(rememberScrollState())) {
                content(current, { changed -> draft = changed }, found)
                androidx.compose.foundation.layout.Spacer(Modifier.padding(bottom = 24.dp))
            }
        }
    }
}

/** One typed setting: its name over the box, and what it is for under it, as the dashboard's forms put a hint under each field. */
@Composable
fun SettingField(
    label: String,
    text: String,
    onChange: (String) -> Unit,
    hint: String? = null,
    prompt: String? = null,
    keyboardType: KeyboardType = KeyboardType.Text,
    isSingleLine: Boolean = true,
) {
    FormField(text, onChange, label, placeholder = prompt, keyboardType = keyboardType, isSingleLine = isSingleLine, minLines = if (isSingleLine) 1 else 3)
    if (hint != null) FooterText(hint)
}

/** A value the server will not read back. It says whether one is stored rather than pretending to show it; an empty box keeps it. */
@Composable
fun SecretField(label: String, isStored: Boolean, typed: String, onChange: (String) -> Unit, hint: String? = null) {
    FormField(
        typed, onChange, label, isPassword = true,
        placeholder = stringResource(if (isStored) R.string.manage_secret_keep else R.string.manage_secret_not_set),
    )
    if (hint != null) FooterText(hint)
    if (isStored) FooterText(stringResource(R.string.manage_secret_stored))
}

/** A column of rows with a heading. */
@Composable
fun GroupHeader(text: String, modifier: Modifier = Modifier) {
    TeaNodeSectionHeader(text, modifier.fillMaxWidth())
}

/** A count said short, as the dashboard does: 1.2K, 3.4M. */
fun compactCount(count: Long): String = android.icu.text.CompactDecimalFormat.getInstance(java.util.Locale.getDefault(), android.icu.text.CompactDecimalFormat.CompactStyle.SHORT).format(count)

/** An amount of money in its currency, or with the code after it when the currency is not known to the locale. */
fun moneyText(amount: Double, currencyCode: String?): String {
    val code = currencyCode.orEmpty()
    return runCatching {
        val format = NumberFormat.getCurrencyInstance()
        if (code.isNotEmpty()) format.currency = java.util.Currency.getInstance(code)
        format.format(amount)
    }.getOrElse { "%.2f %s".format(amount, code).trim() }
}
