package com.teanode.app.settings

import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.Mic
import androidx.compose.material.icons.outlined.NightsStay
import androidx.compose.material.icons.outlined.PanTool
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.PersonOff
import androidx.compose.material.icons.outlined.RecordVoiceOver
import androidx.compose.material.icons.outlined.Sell
import androidx.compose.material.icons.outlined.TextFields
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.SnackbarController
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.Connection
import com.teanode.kit.agent.AgentCategory
import com.teanode.kit.agent.AgentChange
import com.teanode.kit.agent.AgentConfirmDraft
import com.teanode.kit.agent.AgentRecord
import com.teanode.kit.agent.AgentSettings
import com.teanode.kit.agent.AgentTool
import com.teanode.kit.agent.AgentUnattendedDraft
import com.teanode.kit.agent.AgentVoiceDraft
import com.teanode.kit.agent.CollectionSource
import com.teanode.kit.agent.MailboxAutoReply
import com.teanode.kit.agent.MailboxHours
import com.teanode.kit.agent.MailboxPolicy
import com.teanode.kit.agent.MailboxSource
import com.teanode.kit.agent.MailboxSummaries
import com.teanode.kit.agent.MailboxTriage
import com.teanode.kit.agent.VoiceAvailability
import com.teanode.kit.agent.agentSettings
import com.teanode.kit.agent.agentTools
import com.teanode.kit.agent.forgetAgent
import com.teanode.kit.agent.grantAgentCollection
import com.teanode.kit.agent.grantAgentMailbox
import com.teanode.kit.agent.revokeAgentMailbox
import com.teanode.kit.agent.speechVoices
import com.teanode.kit.agent.speechVoiceSample
import com.teanode.kit.agent.updateAgent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/** The pages of Agent settings by name, as a link or the Search screen opens them. */
enum class AgentSettingsPart(val routeName: String) {
    About("about"), Instructions("instructions"), Language("language"), WritingVoice("voice"), SpeakingVoice("speaking-voice"), Reach("reach"),
    Categories("categories"), Model("model"), SpeakingFirst("speaking-first"), AskFirst("ask-first"), WhenAway("when-away");

    companion object {
        fun fromRouteName(routeName: String): AgentSettingsPart? = entries.firstOrNull { part -> part.routeName == routeName }
    }
}

/** What the pages of Agent settings share: the settings as the server has them, and every change, said and read back. */
@Stable
class AgentSettingsModel(private val connection: Connection, private val scope: CoroutineScope, private val snackbar: SnackbarController) {
    var settings: AgentSettings? by mutableStateOf(null)
        private set
    var voices: VoiceAvailability? by mutableStateOf(null)
        private set
    var isLoaded: Boolean by mutableStateOf(false)
        private set
    var loadError: Throwable? by mutableStateOf(null)
        private set
    var isBusy: Boolean by mutableStateOf(false)
        private set

    val agent: AgentRecord? get() = settings?.agent

    fun load() {
        scope.launch {
            try {
                settings = connection.agentSettings()
                loadError = null
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                if (settings == null) loadError = failure else snackbar.showError(failure)
            }
            isLoaded = true
        }
    }

    fun loadVoices() {
        scope.launch { voices = runCatching { connection.speechVoices() }.getOrNull() }
    }

    /** Runs a change that answers the settings as they now are; says [saying], or why it was refused. */
    fun mutate(saying: String?, onDone: (Boolean) -> Unit = {}, work: suspend (Connection) -> AgentSettings) {
        if (isBusy) return
        isBusy = true
        scope.launch {
            var isDone = false
            try {
                settings = work(connection)
                if (saying != null) snackbar.show(saying)
                isDone = true
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
            isBusy = false
            onDone(isDone)
        }
    }

    fun update(change: AgentChange, saying: String?, onDone: (Boolean) -> Unit = {}) = mutate(saying, onDone) { connection -> connection.updateAgent(change) }
}

@Composable
fun rememberAgentSettingsModel(): AgentSettingsModel {
    val connection = LocalConnection.current
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    val model = remember(connection) { AgentSettingsModel(connection, scope, snackbar) }
    LaunchedEffect(model) { model.load() }
    return model
}

/** A page of Agent settings: waits for the settings, shows why they could not be read, and hands the agent to the page. */
@Composable
private fun AgentPage(
    title: String,
    onBack: () -> Unit,
    model: AgentSettingsModel,
    actions: @Composable androidx.compose.foundation.layout.RowScope.() -> Unit = {},
    floatingActionButton: @Composable () -> Unit = {},
    content: LazyListScope.(AgentSettings, AgentRecord) -> Unit,
) {
    TeaNodeScreen(title = title, onBack = onBack, actions = actions, floatingActionButton = floatingActionButton) { contentPadding ->
        val settings = model.settings
        val agent = settings?.agent
        val error = model.loadError
        when {
            settings != null && agent != null -> androidx.compose.foundation.lazy.LazyColumn(Modifier.padding(contentPadding), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 88.dp)) { content(settings, agent) }
            error != null -> ErrorState(LocalAppSession.current.describeLoadProblem(error), onRetry = { model.load() }, modifier = Modifier.padding(contentPadding))
            !model.isLoaded -> LoadingState(Modifier.padding(contentPadding))
        }
    }
}

/** The first page: turn the agent on, and every part of how it works. */
@Composable
fun AgentSettingsScreen(onBack: () -> Unit, onOpenPart: (AgentSettingsPart) -> Unit, onOpenMailbox: (String) -> Unit = {}) {
    val model = rememberAgentSettingsModel()
    var isConfirmingForget by remember { mutableStateOf(false) }
    val onText = stringResource(R.string.settings_agent_is_on)
    val forgottenText = stringResource(R.string.settings_agent_forgotten)
    TeaNodeScreen(title = stringResource(R.string.settings_agent_title), onBack = onBack) { contentPadding ->
        val settings = model.settings
        val error = model.loadError
        when {
            settings != null && !settings.allowed.enabled -> EmptyState(Icons.Outlined.PersonOff, stringResource(R.string.settings_agent_your_agent), description = stringResource(R.string.settings_agent_not_enabled), modifier = Modifier.padding(contentPadding))
            settings != null -> androidx.compose.foundation.lazy.LazyColumn(Modifier.padding(contentPadding)) {
                val agent = settings.agent
                if (agent == null) {
                    item {
                        FooterText(stringResource(R.string.settings_agent_intro))
                        TeaNodeRow(headline = stringResource(R.string.settings_agent_turn_on), leadingIcon = Icons.Outlined.Person, onClick = { model.update(AgentChange(enabled = true), onText) })
                    }
                } else {
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_about), { onOpenPart(AgentSettingsPart.About) }, leadingIcon = Icons.Outlined.Person) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_instructions), { onOpenPart(AgentSettingsPart.Instructions) }, leadingIcon = Icons.Outlined.TextFields) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_language), { onOpenPart(AgentSettingsPart.Language) }, leadingIcon = Icons.Outlined.Language) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_voice), { onOpenPart(AgentSettingsPart.WritingVoice) }, leadingIcon = Icons.Outlined.ChatBubbleOutline) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_speaking_voice), { onOpenPart(AgentSettingsPart.SpeakingVoice) }, leadingIcon = Icons.Outlined.RecordVoiceOver) }
                    if (agent.isOperatorDisabled) item { TeaNodeRow(headline = stringResource(R.string.settings_agent_operator_disabled), leadingIcon = Icons.Outlined.WarningAmber) }
                    item { TeaNodeSectionHeader(stringResource(R.string.settings_agent_mail_and_calendars)) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_reach), { onOpenPart(AgentSettingsPart.Reach) }, leadingIcon = Icons.Outlined.Inbox) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_categories), { onOpenPart(AgentSettingsPart.Categories) }, leadingIcon = Icons.Outlined.Sell) }
                    item { TeaNodeSectionHeader(stringResource(R.string.settings_agent_advanced)) }
                    if (settings.choices.isNotEmpty()) item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_model), { onOpenPart(AgentSettingsPart.Model) }, leadingIcon = Icons.Outlined.Memory) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_speaking_first), { onOpenPart(AgentSettingsPart.SpeakingFirst) }, leadingIcon = Icons.Outlined.Mic) }
                    if (settings.allowed.ask) item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_ask_first), { onOpenPart(AgentSettingsPart.AskFirst) }, leadingIcon = Icons.Outlined.PanTool) }
                    item { TeaNodeNavigationRow(stringResource(R.string.settings_agent_when_away), { onOpenPart(AgentSettingsPart.WhenAway) }, leadingIcon = Icons.Outlined.NightsStay) }
                    item {
                        TeaNodeRow(headline = stringResource(R.string.settings_agent_forget), leadingIcon = Icons.Outlined.Delete, onClick = { isConfirmingForget = true })
                        FooterText(stringResource(R.string.settings_agent_forget_footer))
                    }
                }
            }
            error != null -> ErrorState(LocalAppSession.current.describeLoadProblem(error), onRetry = { model.load() }, modifier = Modifier.padding(contentPadding))
            else -> LoadingState(Modifier.padding(contentPadding))
        }
    }
    if (isConfirmingForget) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_agent_forget), confirmLabel = stringResource(R.string.settings_agent_forget), isDestructive = true, text = stringResource(R.string.settings_agent_forget_detail),
            onConfirm = { isConfirmingForget = false; model.mutate(forgottenText) { connection -> connection.forgetAgent() } }, onDismiss = { isConfirmingForget = false },
        )
    }
}

/** The page a part of Agent settings names. */
@Composable
fun AgentSettingsPartScreen(part: AgentSettingsPart, onBack: () -> Unit) {
    val model = rememberAgentSettingsModel()
    when (part) {
        AgentSettingsPart.About -> AgentAboutPage(model, onBack)
        AgentSettingsPart.Instructions -> AgentInstructionsPage(model, onBack)
        AgentSettingsPart.Language -> AgentLanguagePage(model, onBack)
        AgentSettingsPart.WritingVoice -> AgentWritingVoicePage(model, onBack)
        AgentSettingsPart.SpeakingVoice -> AgentSpeakingVoicePage(model, onBack)
        AgentSettingsPart.Reach -> AgentReachPage(model, onBack)
        AgentSettingsPart.Categories -> AgentCategoriesPage(model, onBack)
        AgentSettingsPart.Model -> AgentModelPage(model, onBack)
        AgentSettingsPart.SpeakingFirst -> AgentSpeakingFirstPage(model, onBack)
        AgentSettingsPart.AskFirst -> AgentAskFirstPage(model, onBack)
        AgentSettingsPart.WhenAway -> AgentWhenAwayPage(model, onBack)
    }
}

@Composable
private fun AgentAboutPage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    val onText = stringResource(R.string.settings_agent_is_on)
    val offText = stringResource(R.string.settings_agent_is_off)
    var name by remember(model.agent?.id) { mutableStateOf(model.agent?.name.orEmpty()) }
    AgentPage(
        stringResource(R.string.settings_agent_about), onBack, model,
        actions = { BarTextAction(stringResource(R.string.settings_save), isEnabled = name.isNotBlank() && name.trim() != model.agent?.name, isWorking = model.isBusy, onClick = { model.update(AgentChange(name = name), savedText) }) },
    ) { settings, agent ->
        item {
            SwitchRow(stringResource(R.string.settings_agent_on), agent.enabled, { isOn -> model.update(AgentChange(enabled = isOn), if (isOn) onText else offText) }, isEnabled = !agent.isOperatorDisabled)
            if (agent.isOperatorDisabled) FooterText(stringResource(R.string.settings_agent_operator_disabled))
            TeaNodeSectionHeader(stringResource(R.string.settings_agent_what_to_call))
            FormField(name, { typed -> name = typed.take(AgentSettings.AGENT_NAME_LENGTH) }, stringResource(R.string.settings_agent_what_to_call))
            TeaNodeSectionHeader(stringResource(R.string.settings_agent_today))
            val budget = settings.budget
            if (budget == null) {
                TeaNodeRow(headline = stringResource(R.string.settings_agent_no_budget))
            } else {
                val usedText = if (budget.limit > 0) stringResource(R.string.settings_agent_tokens_of, budget.used.toLong(), budget.limit.toLong()) else stringResource(R.string.settings_agent_tokens_unlimited, budget.used.toLong())
                TeaNodeRow(headline = stringResource(R.string.settings_agent_tokens), supporting = usedText)
                if (budget.costLimit > 0 || budget.cost > 0) {
                    val currency = budget.currency.orEmpty()
                    TeaNodeRow(headline = stringResource(R.string.settings_agent_cost), supporting = if (budget.costLimit > 0) "%.2f / %.2f %s".format(budget.cost, budget.costLimit, currency).trim() else "%.2f %s".format(budget.cost, currency).trim())
                }
            }
            TeaNodeRow(headline = stringResource(R.string.settings_profile_timezone), supporting = settings.timezone)
            TeaNodeRow(headline = stringResource(R.string.settings_agent_language), supporting = languageName(settings.language))
        }
    }
}

private fun languageName(code: String): String = AgentSettings.languages.firstOrNull { language -> language.code == code }?.name ?: code

@Composable
private fun AgentInstructionsPage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    var text by remember(model.agent?.id) { mutableStateOf(model.agent?.instructions.orEmpty()) }
    AgentPage(
        stringResource(R.string.settings_agent_instructions), onBack, model,
        actions = { BarTextAction(stringResource(R.string.settings_save), isEnabled = text != (model.agent?.instructions ?: ""), isWorking = model.isBusy, onClick = { model.update(AgentChange(instructions = text), savedText) }) },
    ) { _, _ ->
        item { FormField(text, { text = it }, stringResource(R.string.settings_agent_instructions), placeholder = stringResource(R.string.settings_agent_instructions_prompt), isSingleLine = false, minLines = 6) }
    }
}

@Composable
private fun LanguageChoiceField(label: String, code: String, onChange: (String) -> Unit) {
    val other = "__other__"
    val known = AgentSettings.languages.any { language -> language.code == code }
    var isOtherChosen by remember { mutableStateOf(code.isNotEmpty() && !known) }
    val followsAccount = stringResource(R.string.settings_agent_language_follows)
    val anotherLanguage = stringResource(R.string.settings_agent_language_another)
    ChoiceRow(
        label, listOf("") + AgentSettings.languages.map { language -> language.code } + other, if (isOtherChosen) other else code,
        label = { choice -> if (choice.isEmpty()) followsAccount else if (choice == other) anotherLanguage else languageName(choice) },
        onSelect = { choice ->
            isOtherChosen = choice == other
            onChange(if (choice == other) "" else choice)
        },
    )
    if (isOtherChosen) FormField(code, onChange, stringResource(R.string.settings_agent_language_code), placeholder = "sv")
}

@Composable
private fun AgentLanguagePage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    var talks by remember(model.agent?.id) { mutableStateOf(model.agent?.language.orEmpty()) }
    var works by remember(model.agent?.id) { mutableStateOf(model.agent?.knowledgeLanguage.orEmpty()) }
    val changed = talks != (model.agent?.language ?: "") || works != (model.agent?.knowledgeLanguage ?: "")
    AgentPage(
        stringResource(R.string.settings_agent_language), onBack, model,
        actions = { BarTextAction(stringResource(R.string.settings_save), isEnabled = changed, isWorking = model.isBusy, onClick = { model.update(AgentChange(language = talks, knowledgeLanguage = works), savedText) }) },
    ) { _, _ ->
        item {
            TeaNodeSectionHeader(stringResource(R.string.settings_agent_talks_in))
            LanguageChoiceField(stringResource(R.string.settings_agent_talks_in), talks, { talks = it })
            TeaNodeSectionHeader(stringResource(R.string.settings_agent_works_in))
            LanguageChoiceField(stringResource(R.string.settings_agent_works_in), works, { works = it })
            FooterText(stringResource(R.string.settings_agent_works_in_footer))
        }
    }
}

@Composable
private fun AgentWritingVoicePage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    var draft by remember(model.agent?.id) { mutableStateOf(AgentVoiceDraft.of(model.agent?.voice)) }
    val original = AgentVoiceDraft.of(model.agent?.voice)
    AgentPage(
        stringResource(R.string.settings_agent_voice), onBack, model,
        actions = { BarTextAction(stringResource(R.string.settings_save), isEnabled = draft != original, isWorking = model.isBusy, onClick = { model.update(AgentChange(voice = draft.voice), savedText) }) },
    ) { _, _ ->
        item {
            ChoiceRow(
                stringResource(R.string.settings_agent_tone), AgentVoiceDraft.tones, draft.tone,
                label = { tone -> stringResource(when (tone) { "formal" -> R.string.settings_agent_tone_formal; "casual" -> R.string.settings_agent_tone_casual; else -> R.string.settings_agent_tone_neutral }) },
                onSelect = { draft = draft.copy(tone = it) },
            )
            ChoiceRow(
                stringResource(R.string.settings_agent_length), AgentVoiceDraft.lengths, draft.length,
                label = { length -> stringResource(when (length) { "short" -> R.string.settings_agent_length_short; "long" -> R.string.settings_agent_length_long; else -> R.string.settings_agent_length_medium }) },
                onSelect = { draft = draft.copy(length = it) },
            )
            FooterText(stringResource(R.string.settings_agent_voice_footer))
            FormField(draft.greeting, { draft = draft.copy(greeting = it) }, stringResource(R.string.settings_agent_greeting))
            FormField(draft.signoff, { draft = draft.copy(signoff = it) }, stringResource(R.string.settings_agent_signoff))
        }
    }
}

@Composable
private fun AgentSpeakingVoicePage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    LaunchedEffect(model) { model.loadVoices() }
    AgentPage(stringResource(R.string.settings_agent_speaking_voice), onBack, model) { _, agent ->
        val voices = model.voices
        if (voices != null) {
            item {
                val chosen = agent.speechVoice.orEmpty()
                val serverText = stringResource(R.string.settings_agent_server_voice, voices.serverSpeechVoice.orEmpty())
                ChoiceRow(
                    stringResource(R.string.settings_agent_speaking_voice), listOf("") + (voices.speechVoices ?: emptyList()), chosen,
                    label = { voice -> if (voice.isEmpty()) serverText else voice }, onSelect = { voice -> model.update(AgentChange(speechVoice = voice), savedText) },
                )
                FooterText(stringResource(R.string.settings_agent_speaking_voice_footer))
                val context = androidx.compose.ui.platform.LocalContext.current
                val actions = rememberActionRunner()
                val listening = remember { mutableStateOf(false) }
                val heard = chosen.ifEmpty { voices.serverSpeechVoice.orEmpty() }
                val connection = LocalConnection.current
                TeaNodeRow(
                    headline = stringResource(if (listening.value) R.string.settings_agent_playing else R.string.settings_agent_listen), leadingIcon = Icons.Outlined.RecordVoiceOver,
                    onClick = if (listening.value || heard.isEmpty()) null else ({
                        listening.value = true
                        actions.run(onSuccess = { listening.value = false }) { playSample(context, connection.speechVoiceSample(heard)) }
                    }),
                )
            }
        }
    }
}

@Composable
private fun AgentModelPage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    AgentPage(stringResource(R.string.settings_agent_model), onBack, model) { settings, agent ->
        item {
            val serverChoice = stringResource(R.string.settings_agent_server_choice)
            ChoiceRow(
                stringResource(R.string.settings_agent_model), listOf("") + settings.choices, agent.askModel.orEmpty(),
                label = { choice -> if (choice.isEmpty()) serverChoice else choice }, onSelect = { choice -> model.update(AgentChange(askModel = choice), savedText) },
            )
        }
    }
}

@Composable
private fun AgentSpeakingFirstPage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    AgentPage(stringResource(R.string.settings_agent_speaking_first), onBack, model) { _, agent ->
        item {
            SwitchRow(stringResource(R.string.settings_agent_memory_checks), agent.isMemoryCheckEnabled, { isOn -> model.update(AgentChange(isMemoryCheckEnabled = isOn), savedText) })
            FooterText(stringResource(R.string.settings_agent_memory_checks_footer))
            SwitchRow(stringResource(R.string.settings_agent_offer_ideas), agent.isIdeasEnabled, { isOn -> model.update(AgentChange(isIdeasEnabled = isOn), savedText) })
            FooterText(stringResource(R.string.settings_agent_offer_ideas_footer))
            FooterText(stringResource(R.string.settings_agent_speaking_first_footer))
        }
    }
}

@Composable
private fun riskTitle(risk: String): String = when (risk) {
    "outward" -> stringResource(R.string.settings_agent_risk_outward)
    "money" -> stringResource(R.string.settings_agent_risk_money)
    "destructive" -> stringResource(R.string.settings_agent_risk_destructive)
    "granting" -> stringResource(R.string.settings_agent_risk_granting)
    "listed" -> stringResource(R.string.settings_agent_risk_listed)
    else -> risk
}

@Composable
private fun AgentWhenAwayPage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    AgentPage(stringResource(R.string.settings_agent_when_away), onBack, model) { _, agent ->
        item {
            val draft = AgentUnattendedDraft.of(agent)
            FooterText(stringResource(R.string.settings_agent_when_away_footer))
            AgentSettings.unattendedRisks.forEach { risk ->
                SwitchRow(riskTitle(risk), draft.isAllowed(risk), { isOn -> model.update(AgentChange(unattendedAllowedRisks = draft.setting(risk, isOn).orderedAllowedRisks), savedText) })
            }
        }
    }
}

@Composable
private fun AgentAskFirstPage(model: AgentSettingsModel, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val savedText = stringResource(R.string.settings_saved)
    val tools = com.teanode.app.ui.components.rememberLoad { connection.agentTools() }
    AgentPage(stringResource(R.string.settings_agent_ask_first), onBack, model) { _, agent ->
        item { FooterText(stringResource(R.string.settings_agent_ask_first_footer)) }
        val list = tools.value
        if (list == null) {
            item { TeaNodeRow(headline = stringResource(R.string.common_loading)) }
        } else {
            val draft = AgentConfirmDraft.of(agent.confirm)
            AgentTool.families(list).forEach { family ->
                item {
                    SwitchRow(family, draft.isAskingFirst(family), { isOn -> model.update(AgentChange(confirm = draft.settingAskingFirst(family, isOn).askFirstNames), savedText) }, supporting = stringResource(R.string.settings_agent_whole_family))
                }
                items(list.filter { tool -> tool.family == family }.size) { index ->
                    val tool = list.filter { candidate -> candidate.family == family }[index]
                    val asks = draft.doesToolAskFirst(tool)
                    SwitchRow(
                        tool.name, asks, { isOn -> model.update(AgentChange(confirm = draft.settingAskingFirst(tool.name, isOn).askFirstNames), savedText) },
                        supporting = if (tool.confirms) stringResource(R.string.settings_agent_always_asks) else tool.description.ifEmpty { null }, isEnabled = !tool.confirms,
                    )
                }
            }
        }
    }
}

@Composable
private fun AgentReachPage(model: AgentSettingsModel, onBack: () -> Unit) {
    val connection = LocalConnection.current
    var openMailbox by remember { mutableStateOf<MailboxSource?>(null) }
    val savedText = stringResource(R.string.settings_saved)
    AgentPage(stringResource(R.string.settings_agent_reach), onBack, model) { settings, _ ->
        item { TeaNodeSectionHeader(stringResource(R.string.settings_agent_mailboxes)) }
        items(settings.sources.size) { index ->
            val source = settings.sources[index]
            TeaNodeRow(
                headline = source.name, supporting = (listOf(source.addresses.joinToString(", ")) + stringResource(if (source.isGranted) R.string.settings_agent_can_reach else R.string.settings_agent_cannot_reach)).filter { text -> text.isNotEmpty() }.joinToString("\n"),
                supportingMaxLines = 3, onClick = { openMailbox = source },
            )
        }
        item { FooterText(stringResource(R.string.settings_agent_mailboxes_footer)) }
        if (settings.collections.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_agent_collections)) }
            items(settings.collections.size) { index ->
                val collection = settings.collections[index]
                SwitchRow(
                    collection.name, collection.granted, { isOn -> model.mutate(savedText) { current -> current.grantAgentCollection(collection, isOn) } },
                    supporting = stringResource(if (collection.isCalendar) R.string.settings_agent_events_next_month else R.string.settings_agent_people_count, collection.items),
                )
            }
        }
    }
    openMailbox?.let { source -> AgentMailboxDialog(model, source, onDismiss = { openMailbox = null }) }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun AgentMailboxDialog(model: AgentSettingsModel, source: MailboxSource, onDismiss: () -> Unit) {
    val settings = model.settings ?: return
    val current = settings.source(source.mailboxId) ?: source
    val allowed = settings.allowed
    val savedText = stringResource(R.string.settings_saved)
    val revokedText = stringResource(R.string.settings_agent_revoked)
    var policy by remember(source.mailboxId, current.policy) { mutableStateOf(current.shownPolicy) }
    var isConfirmingRevoke by remember { mutableStateOf(false) }
    FormDialog(
        title = source.name, confirmLabel = stringResource(if (current.isGranted) R.string.settings_save else R.string.settings_agent_let_it_reach), isConfirmEnabled = policy.autoReply?.hours?.let { hours -> AgentSettings.clockParts(hours.from) != null && AgentSettings.clockParts(hours.until) != null } ?: true, isWorking = model.isBusy,
        onConfirm = { model.mutate(savedText, onDone = { isDone -> if (isDone) onDismiss() }) { connection -> connection.grantAgentMailbox(source.mailboxId, policy) } },
        onDismiss = onDismiss,
    ) {
        androidx.compose.foundation.layout.Column(Modifier.verticalScroll(androidx.compose.foundation.rememberScrollState())) {
            if (allowed.triage) {
                val triage = policy.triage ?: MailboxTriage()
                SwitchRow(stringResource(R.string.settings_agent_sorting), triage.enabled, { isOn -> policy = policy.copy(triage = triage.copy(enabled = isOn)) })
            }
            if (allowed.summaries) {
                val summaries = policy.summaries ?: MailboxSummaries()
                SwitchRow(stringResource(R.string.settings_agent_summaries), summaries.enabled, { isOn -> policy = policy.copy(summaries = summaries.copy(enabled = isOn)) })
            }
            SwitchRow(stringResource(R.string.settings_agent_draft_replies), policy.draftReplies, { isOn -> policy = policy.copy(draftReplies = isOn) }, isEnabled = allowed.draftReplies)
            SwitchRow(stringResource(R.string.settings_agent_search_by_meaning), policy.search, { isOn -> policy = policy.copy(search = isOn) }, isEnabled = allowed.search)
            SwitchRow(stringResource(R.string.settings_agent_research), policy.research, { isOn -> policy = policy.copy(research = isOn) }, isEnabled = allowed.research)
            if (allowed.autoReply) {
                val reply = policy.autoReply ?: MailboxAutoReply()
                SwitchRow(stringResource(R.string.settings_agent_answer_for_me), reply.enabled, { isOn -> policy = policy.copy(autoReply = reply.copy(enabled = isOn)) })
                if (reply.enabled) {
                    FormField(reply.guidance.orEmpty(), { typed -> policy = policy.copy(autoReply = reply.copy(guidance = typed)) }, stringResource(R.string.settings_agent_guidance), isSingleLine = false, minLines = 3)
                    val whoms = listOf("known", "everyone", "list")
                    ChoiceRow(
                        stringResource(R.string.settings_agent_whom), whoms, reply.scope?.ifEmpty { null } ?: "known",
                        label = { scope -> stringResource(when (scope) { "everyone" -> R.string.settings_agent_whom_everyone; "list" -> R.string.settings_agent_whom_list; else -> R.string.settings_agent_whom_known }) },
                        onSelect = { scope -> policy = policy.copy(autoReply = reply.copy(scope = scope)) },
                    )
                    val whens = listOf("always", "outsideHours", "whenAway")
                    ChoiceRow(
                        stringResource(R.string.settings_agent_when), whens, reply.`when`?.ifEmpty { null } ?: "always",
                        label = { whenChoice -> stringResource(when (whenChoice) { "outsideHours" -> R.string.settings_agent_when_outside_hours; "whenAway" -> R.string.settings_agent_when_away_only; else -> R.string.settings_agent_when_always }) },
                        onSelect = { whenChoice -> policy = policy.copy(autoReply = reply.copy(`when` = whenChoice, hours = if (whenChoice == "outsideHours") reply.hours ?: MailboxHours.workingHours else reply.hours)) },
                    )
                    if (reply.`when` == "outsideHours") {
                        val hours = reply.hours ?: MailboxHours.workingHours
                        FormField(hours.from, { typed -> policy = policy.copy(autoReply = reply.copy(hours = hours.copy(from = typed))) }, stringResource(R.string.settings_dreams_from), placeholder = "HH:MM", isError = AgentSettings.clockParts(hours.from) == null)
                        FormField(hours.until, { typed -> policy = policy.copy(autoReply = reply.copy(hours = hours.copy(until = typed))) }, stringResource(R.string.settings_dreams_until), placeholder = "HH:MM", isError = AgentSettings.clockParts(hours.until) == null)
                    }
                    if (reply.`when` == "outsideHours") {
                        val hours = reply.hours ?: MailboxHours.workingHours
                        androidx.compose.foundation.layout.FlowRow(Modifier.padding(horizontal = 16.dp)) {
                            listOf(1, 2, 3, 4, 5, 6, 0).forEach { day ->
                                androidx.compose.material3.FilterChip(
                                    selected = hours.days.contains(day), modifier = Modifier.padding(end = 6.dp),
                                    onClick = { policy = policy.copy(autoReply = reply.copy(hours = hours.copy(days = if (hours.days.contains(day)) hours.days - day else (hours.days + day).sorted()))) },
                                    label = { Text(weekdayShortName(day)) },
                                )
                            }
                        }
                    }
                    FormField(AgentSettings.joinList(reply.allow), { typed -> policy = policy.copy(autoReply = reply.copy(allow = AgentSettings.splitList(typed))) }, stringResource(R.string.settings_agent_allow_list), placeholder = "example.com, someone@example.org")
                    FormField(AgentSettings.joinList(reply.never), { typed -> policy = policy.copy(autoReply = reply.copy(never = AgentSettings.splitList(typed))) }, stringResource(R.string.settings_agent_never_list), placeholder = "example.net")
                    FormField(AgentSettings.joinList(reply.categories), { typed -> policy = policy.copy(autoReply = reply.copy(categories = AgentSettings.splitList(typed))) }, stringResource(R.string.settings_agent_only_categories), placeholder = stringResource(R.string.settings_agent_any))
                    FormField((reply.holdMinutes ?: 0).takeIf { minutes -> minutes > 0 }?.toString().orEmpty(), { typed -> policy = policy.copy(autoReply = reply.copy(holdMinutes = AgentSettings.wholeNumber(typed))) }, stringResource(R.string.settings_agent_hold_minutes), placeholder = "10", keyboardType = KeyboardType.Number)
                    FormField((reply.dailyLimit ?: 0).takeIf { limit -> limit > 0 }?.toString().orEmpty(), { typed -> policy = policy.copy(autoReply = reply.copy(dailyLimit = AgentSettings.wholeNumber(typed))) }, stringResource(R.string.settings_agent_daily_limit), placeholder = "20", keyboardType = KeyboardType.Number)
                }
            }
            if (current.isGranted) {
                TeaNodeRow(headline = stringResource(R.string.settings_agent_revoke), leadingIcon = Icons.Outlined.Delete, onClick = { isConfirmingRevoke = true })
            }
        }
    }
    if (isConfirmingRevoke) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_agent_revoke), confirmLabel = stringResource(R.string.settings_agent_revoke), isDestructive = true, text = stringResource(R.string.settings_agent_revoke_detail, source.name),
            onConfirm = { isConfirmingRevoke = false; model.mutate(revokedText, onDone = { isDone -> if (isDone) onDismiss() }) { connection -> connection.revokeAgentMailbox(source.mailboxId) } },
            onDismiss = { isConfirmingRevoke = false },
        )
    }
}

@Composable
private fun AgentCategoriesPage(model: AgentSettingsModel, onBack: () -> Unit) {
    val savedText = stringResource(R.string.settings_saved)
    var editing by remember { mutableStateOf<Pair<Int, AgentCategory>?>(null) }
    var removing by remember { mutableStateOf<Int?>(null) }
    AgentPage(
        stringResource(R.string.settings_agent_categories), onBack, model,
        floatingActionButton = { FloatingActionButton(onClick = { editing = -1 to AgentCategory() }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_agent_add_category)) } },
    ) { settings, agent ->
        if (agent.categories.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_agent_no_categories)) }
        items(agent.categories.size) { index ->
            val category = agent.categories[index]
            TeaNodeRow(
                headline = category.name, supporting = category.description?.ifEmpty { null }, onClick = { editing = index to category },
                trailing = { IconButton(onClick = { removing = index }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_remove)) } },
            )
        }
        item { FooterText(stringResource(R.string.settings_agent_categories_footer, settings.fixedCategoryNames.joinToString(", "))) }
    }
    editing?.let { (index, original) ->
        var category by remember(index) { mutableStateOf(original) }
        val agent = model.agent
        FormDialog(
            title = stringResource(if (index < 0) R.string.settings_agent_add_category else R.string.settings_agent_edit_category), confirmLabel = stringResource(R.string.settings_save),
            isConfirmEnabled = category.name.isNotBlank(), isWorking = model.isBusy,
            onConfirm = {
                val list = (agent?.categories ?: emptyList()).toMutableList()
                val saved = category.copy(name = category.name.trim().take(AgentSettings.CATEGORY_NAME_LENGTH))
                if (index < 0) list.add(saved) else list[index] = saved
                model.update(AgentChange(categories = list), savedText, onDone = { isDone -> if (isDone) editing = null })
            },
            onDismiss = { editing = null },
        ) {
            FormField(category.name, { category = category.copy(name = it) }, stringResource(R.string.settings_agent_category_name))
            FormField(category.description.orEmpty(), { category = category.copy(description = it) }, stringResource(R.string.settings_agent_category_what), isSingleLine = false, minLines = 2)
        }
    }
    removing?.let { index ->
        val removed = model.agent?.categories?.getOrNull(index)
        if (removed != null) {
            ConfirmationDialog(
                title = stringResource(R.string.settings_agent_remove_category, removed.name), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true,
                text = stringResource(R.string.settings_agent_remove_category_detail),
                onConfirm = { removing = null; model.update(AgentChange(categories = model.agent?.categories?.filterIndexed { position, _ -> position != index }), savedText) },
                onDismiss = { removing = null },
            )
        }
    }
}

/** Plays a WAV sample once and returns when it has finished. */
private suspend fun playSample(context: android.content.Context, wave: ByteArray) {
    val file = java.io.File.createTempFile("voice-sample", ".wav", context.cacheDir)
    file.writeBytes(wave)
    try {
        kotlinx.coroutines.suspendCancellableCoroutine<Unit> { continuation ->
            val player = android.media.MediaPlayer()
            continuation.invokeOnCancellation { player.release() }
            player.setDataSource(file.path)
            player.setOnCompletionListener { player.release(); if (continuation.isActive) continuation.resumeWith(Result.success(Unit)) }
            player.setOnErrorListener { _, _, _ -> player.release(); if (continuation.isActive) continuation.resumeWith(Result.failure(IllegalStateException("The sample could not be played."))); true }
            player.prepare()
            player.start()
        }
    } finally {
        file.delete()
    }
}

/** The short name of a day, 0 being Sunday, in the person's language. */
private fun weekdayShortName(day: Int): String =
    java.time.DayOfWeek.of(if (day == 0) 7 else day).getDisplayName(java.time.format.TextStyle.SHORT, java.util.Locale.getDefault())
