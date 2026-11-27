package com.teanode.app.settings.manage

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material3.Text
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
import com.teanode.app.settings.ChoiceRow
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.FormField
import com.teanode.app.settings.SwitchRow
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.AgentFeatureSwitches
import com.teanode.kit.manage.AgentGeneralDraft
import com.teanode.kit.manage.AgentLimitsDraft
import com.teanode.kit.manage.AgentModelAssignments
import com.teanode.kit.manage.AgentModelChoices
import com.teanode.kit.manage.AgentServerRecordingDraft
import com.teanode.kit.manage.AgentServerSettings
import com.teanode.kit.manage.AgentServerVoiceDraft
import com.teanode.kit.manage.RecordingModelCapability
import com.teanode.kit.manage.SettingsList
import com.teanode.kit.manage.recordingModelCapabilities

/** Whether people may have an agent at all, the house instructions every agent here is given, and the equipment on this network the agent may reach. */
@Composable
fun AgentPersonalSettingsScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_personal_agents), onBack, { settings -> AgentGeneralDraft.of(settings) }, { draft -> draft.agentParameters }) { draft, onChange, _ ->
        SwitchRow(stringResource(R.string.manage_personal_enabled), draft.isEnabled, { isOn -> onChange(draft.copy(isEnabled = isOn)) })
        FooterText(stringResource(R.string.manage_personal_footer))
        TeaNodeSectionHeader(stringResource(R.string.manage_house_instructions))
        FormField(
            draft.instructions, { typed -> onChange(draft.copy(instructions = typed)) }, stringResource(R.string.manage_house_instructions),
            placeholder = stringResource(R.string.manage_house_instructions_prompt), isSingleLine = false, minLines = 3,
        )
        SettingField(
            stringResource(R.string.manage_private_addresses), draft.allowPrivateAddressesText, { typed -> onChange(draft.copy(allowPrivateAddressesText = typed)) },
            hint = stringResource(R.string.manage_private_addresses_hint), prompt = "192.0.2.10, 10.0.0.0/24, printer.lan",
        )
        SettingField(
            stringResource(R.string.manage_skip_certificates), draft.skipCertificateCheckText, { typed -> onChange(draft.copy(skipCertificateCheckText = typed)) },
            hint = stringResource(R.string.manage_skip_certificates_hint), prompt = "camera.lan, 192.0.2.10",
        )
    }
}

/** What an empty assignment means, as its picker says it. */
@Composable
fun modelPlaceholder(fieldName: String): String = stringResource(
    when (fieldName) {
        "default", "fast", "embedding" -> R.string.manage_model_none
        "quick" -> R.string.manage_model_quick_none
        "synthesize" -> R.string.manage_model_synthesize_none
        "transcription" -> R.string.manage_model_transcription_none
        "decide" -> R.string.manage_model_decide_none
        "categorize" -> R.string.manage_model_categorize_none
        else -> R.string.manage_model_same_as_default
    },
)

@Composable
fun modelLabel(fieldName: String): String = stringResource(
    when (fieldName) {
        "default" -> R.string.manage_model_default
        "fast" -> R.string.manage_model_fast
        "quick" -> R.string.manage_model_quick
        "embedding" -> R.string.manage_model_embedding
        "triage" -> R.string.manage_model_triage
        "research" -> R.string.manage_model_research
        "summarize" -> R.string.manage_model_summarize
        "reply" -> R.string.manage_model_reply
        "ask" -> R.string.manage_model_ask
        "schedule" -> R.string.manage_model_schedule
        "compact" -> R.string.manage_model_compact
        "scan" -> R.string.manage_model_scan
        "synthesize" -> R.string.manage_model_synthesize
        "decide" -> R.string.manage_model_decide
        "categorize" -> R.string.manage_model_categorize
        else -> R.string.manage_model_transcription
    },
)

/** The models the enabled providers offer, for the pickers: a provider that cannot be reached contributes nothing. */
@Composable
private fun rememberModelCapabilities(): List<RecordingModelCapability> {
    val connection = LocalConnection.current
    val loaded = rememberLoad { runCatching { connection.recordingModelCapabilities() }.getOrDefault(emptyList()) }
    return loaded.value ?: emptyList()
}

/**
 * Assigns work to models: the default, fast, quick and embedding ones, then one per kind of work, and the models a person may pick
 * for their own conversations. A deciding provider's models are offered only where the server takes them.
 */
@Composable
fun AgentModelsScreen(onBack: () -> Unit) {
    val capabilities = rememberModelCapabilities()
    val knownModels = capabilities.map { capability -> capability.name }
    AgentSettingsFormScreen(stringResource(R.string.manage_models), onBack, { settings -> settings.models }, { draft -> draft.agentParameters }) { draft, onChange, settings ->
        val choices = AgentModelChoices.of(knownModels, settings.providers)
        AgentModelAssignments.BASE_FIELD_NAMES.forEach { fieldName -> ModelRow(fieldName, choices.forWork, draft, onChange) }
        FooterText(stringResource(R.string.manage_models_footer))
        TeaNodeSectionHeader(stringResource(R.string.manage_per_kind_of_work))
        AgentModelAssignments.WORK_FIELD_NAMES.filter { fieldName -> fieldName != "transcription" }.forEach { fieldName -> ModelRow(fieldName, choices.forWork, draft, onChange) }
        ModelRow("decide", choices.forDeciding, draft, onChange)
        ModelRow("categorize", choices.forDeciding + choices.forWork, draft, onChange)
        TeaNodeSectionHeader(stringResource(R.string.manage_recording_transcription))
        ModelRow("transcription", capabilities.filter { capability -> capability.canAttributeRecordingSpeakers }.map { capability -> capability.name }, draft, onChange)
        val capability = capabilities.firstOrNull { candidate -> candidate.name == draft.transcription }
        if (capability != null && capability.canAttributeRecordingSpeakers) {
            TeaNodeRow(
                headline = stringResource(R.string.manage_voice_references),
                supporting = stringResource(if (capability.hasSpeakerReferences == true) R.string.manage_supported else R.string.manage_unavailable),
            )
            capability.maximumAudioBytes?.takeIf { bytes -> bytes > 0 }?.let { bytes ->
                TeaNodeRow(headline = stringResource(R.string.manage_audio_per_request), supporting = android.text.format.Formatter.formatFileSize(androidx.compose.ui.platform.LocalContext.current, bytes))
            }
            capability.maximumSpeakerReferenceCount?.takeIf { count -> count > 0 }?.let { count ->
                TeaNodeRow(headline = stringResource(R.string.manage_references_per_request), supporting = count.toString())
            }
        }
        FooterText(stringResource(R.string.manage_transcription_footer))
        TeaNodeSectionHeader(stringResource(R.string.manage_models_a_person_may_pick))
        draft.choices.forEach { choice ->
            TeaNodeRow(
                headline = choice,
                trailing = {
                    androidx.compose.material3.TextButton(onClick = { onChange(draft.copy(choices = draft.choices - choice)) }) { Text(stringResource(R.string.settings_remove)) }
                },
            )
        }
        var isAdding by remember { mutableStateOf(false) }
        TeaNodeNavigationRow(stringResource(R.string.manage_add_a_model_people_may_pick), { isAdding = true }, leadingIcon = Icons.Outlined.Add)
        if (isAdding) {
            ModelChoiceDialog(
                title = stringResource(R.string.manage_add_a_model_people_may_pick), emptyTitle = null,
                knownModels = choices.forWork.filter { model -> !draft.choices.contains(model) }, chosen = "",
                onChoose = { added -> isAdding = false; if (added.isNotEmpty() && !draft.choices.contains(added)) onChange(draft.copy(choices = draft.choices + added)) },
                onDismiss = { isAdding = false },
            )
        }
        FooterText(stringResource(R.string.manage_models_pick_footer))
    }
}

@Composable
private fun ModelRow(fieldName: String, known: List<String>, draft: AgentModelAssignments, onChange: (AgentModelAssignments) -> Unit) {
    var isChoosing by remember { mutableStateOf(false) }
    val chosen = draft.assignment(fieldName)
    val placeholder = modelPlaceholder(fieldName)
    TeaNodeRow(headline = modelLabel(fieldName), supporting = chosen.ifEmpty { placeholder }, supportingMaxLines = 3, onClick = { isChoosing = true })
    if (isChoosing) {
        ModelChoiceDialog(
            title = modelLabel(fieldName), emptyTitle = placeholder, knownModels = known, chosen = chosen,
            onChoose = { picked -> isChoosing = false; onChange(draft.assigning(fieldName, picked)) }, onDismiss = { isChoosing = false },
        )
    }
}

/** Picks one model: what the providers offer, or a name the list does not know yet, written provider:model. */
@Composable
fun ModelChoiceDialog(title: String, emptyTitle: String?, knownModels: List<String>, chosen: String, onChoose: (String) -> Unit, onDismiss: () -> Unit) {
    var typedName by remember { mutableStateOf("") }
    com.teanode.app.settings.FormDialog(
        title = title, confirmLabel = stringResource(R.string.manage_use_this_name), isConfirmEnabled = typedName.trim().isNotEmpty(), onConfirm = { onChoose(typedName.trim()) }, onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            FormField(typedName, { typed -> typedName = typed }, "provider:model", supporting = stringResource(R.string.manage_model_unlisted_hint))
            if (emptyTitle != null) ModelChoiceRow(emptyTitle, chosen.isEmpty()) { onChoose("") }
            AgentModelChoices.listed(knownModels, chosen).forEach { modelName -> ModelChoiceRow(modelName, chosen == modelName) { onChoose(modelName) } }
        }
    }
}

@Composable
private fun ModelChoiceRow(text: String, isChosen: Boolean, onClick: () -> Unit) {
    TeaNodeRow(
        headline = text, onClick = onClick,
        leading = { androidx.compose.material3.RadioButton(selected = isChosen, onClick = null) },
    )
}

/** Talking to the agent like a phone call: transcribed and read aloud by a provider of kind openai. */
@Composable
fun AgentVoiceScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_voice), onBack, { settings -> AgentServerVoiceDraft.of(settings.voice) }, { draft -> draft.agentParameters }) { draft, onChange, settings ->
        SwitchRow(stringResource(R.string.manage_enabled), draft.isEnabled, { isOn -> onChange(draft.copy(isEnabled = isOn)) })
        FooterText(stringResource(R.string.manage_voice_footer))
        val providerNames = settings.providers.filter { provider -> provider.providerKind == "openai" }.map { provider -> provider.name }
        val firstOne = stringResource(R.string.manage_the_first_one)
        ChoiceRow(stringResource(R.string.manage_voice_provider), listOf("") + providerNames, draft.provider, label = { name -> name.ifEmpty { firstOne } }, onSelect = { name -> onChange(draft.copy(provider = name)) })
        FormField(draft.transcriptionModel, { typed -> onChange(draft.copy(transcriptionModel = typed)) }, stringResource(R.string.manage_transcription_model), placeholder = "gpt-4o-transcribe")
        FormField(
            draft.silenceMSText, { typed -> onChange(draft.copy(silenceMSText = SettingsList.digits(typed))) }, stringResource(R.string.manage_voice_pause),
            placeholder = "500", keyboardType = KeyboardType.Number,
        )
        FormField(draft.askModel, { typed -> onChange(draft.copy(askModel = typed)) }, stringResource(R.string.manage_voice_calls_model), placeholder = stringResource(R.string.manage_voice_calls_model_prompt))
        FormField(draft.speechModel, { typed -> onChange(draft.copy(speechModel = typed)) }, stringResource(R.string.manage_speech_model), placeholder = "gpt-4o-mini-tts")
        ChoiceRow(
            stringResource(R.string.manage_speech_voice), listOf("") + AgentServerSettings.SPEECH_VOICES.filter { voice -> voice != "marin" }, draft.speechVoice,
            label = { voice -> voice.ifEmpty { "marin" } }, onSelect = { voice -> onChange(draft.copy(speechVoice = voice)) },
        )
    }
}

/** What the server does with a recording while it is captured and after: the live words, and the second transcription of each speaker's turn. */
@Composable
fun AgentRecordingScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_recording), onBack, { settings -> AgentServerRecordingDraft.of(settings.recording) }, { draft -> draft.agentParameters }) { draft, onChange, _ ->
        TeaNodeSectionHeader(stringResource(R.string.manage_recording_while_capturing))
        SwitchRow(stringResource(R.string.manage_live_transcription), draft.isLiveTranscriptionEnabled, { isOn -> onChange(draft.copy(isLiveTranscriptionEnabled = isOn)) })
        FooterText(stringResource(R.string.manage_live_transcription_footer))
        SwitchRow(stringResource(R.string.manage_accurate_words), draft.isAccurateWordsEnabled, { isOn -> onChange(draft.copy(isAccurateWordsEnabled = isOn)) })
        FooterText(stringResource(R.string.manage_accurate_words_footer))
    }
}

@Composable
private fun featureLabel(feature: String): String = stringResource(
    when (feature) {
        "triage" -> R.string.manage_feature_triage
        "summaries" -> R.string.manage_feature_summaries
        "draftReplies" -> R.string.manage_feature_draft_replies
        "search" -> R.string.manage_feature_search
        "research" -> R.string.manage_feature_research
        "autoReply" -> R.string.manage_feature_auto_reply
        "ask" -> R.string.manage_feature_ask
        "schedules" -> R.string.manage_feature_schedules
        "browser" -> R.string.manage_feature_browser
        "connectedServers" -> R.string.manage_feature_connected_servers
        "computer" -> R.string.manage_feature_computer
        "chatApps" -> R.string.manage_feature_chat_apps
        "skills" -> R.string.manage_feature_skills
        "subagents" -> R.string.manage_feature_subagents
        "remember" -> R.string.manage_feature_remember
        "knowledge" -> R.string.manage_feature_knowledge
        else -> R.string.manage_feature_dreaming
    },
)

/** What people may turn on at all. A feature off here is hidden from every person and refused by the API. */
@Composable
fun AgentFeaturesScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_features), onBack, { settings -> settings.features }, { draft -> AgentFeatureSwitches.agentParameters(draft) }) { draft, onChange, _ ->
        AgentServerSettings.FEATURE_NAMES.forEach { feature ->
            SwitchRow(featureLabel(feature), AgentFeatureSwitches.isOn(feature, draft), { isOn -> onChange(draft + (feature to isOn)) })
        }
        FooterText(stringResource(R.string.manage_features_footer))
    }
}

@Composable
private fun limitLabel(limitName: String): String = stringResource(
    when (limitName) {
        "dailyTokensPerAgent" -> R.string.manage_limit_daily_tokens
        "dailyCostPerAgent" -> R.string.manage_limit_daily_cost
        "monthlyCostPerServer" -> R.string.manage_limit_monthly_cost
        // "monthlyTokensPerServer"
        else -> R.string.manage_limit_monthly_tokens
    },
)

/** The caps on what one person and the whole server may spend, in tokens and in money, and how long transcripts and corrections are kept. */
@Composable
fun AgentLimitsScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_limits), onBack, { settings -> AgentLimitsDraft.of(settings) }, { draft -> draft.agentParameters }) { draft, onChange, _ ->
        FooterText(stringResource(R.string.manage_limits_footer))
        AgentLimitsDraft.LIMIT_GROUPS.forEach { limitNames ->
            limitNames.forEach { limitName ->
                FormField(
                    draft.limitTexts[limitName].orEmpty(), { typed -> onChange(draft.copy(limitTexts = draft.limitTexts + (limitName to typed))) }, limitLabel(limitName),
                    keyboardType = KeyboardType.Decimal,
                )
            }
            if (limitNames.contains("dailyCostPerAgent")) {
                FormField(draft.currency, { typed -> onChange(draft.copy(currency = typed.uppercase().take(3))) }, stringResource(R.string.manage_currency), placeholder = "USD")
            }
        }
        FormField(draft.retention.runs, { typed -> onChange(draft.copy(retention = draft.retention.copy(runs = typed))) }, stringResource(R.string.manage_keep_runs))
        FormField(draft.retention.corrections, { typed -> onChange(draft.copy(retention = draft.retention.copy(corrections = typed))) }, stringResource(R.string.manage_keep_corrections))
    }
}
