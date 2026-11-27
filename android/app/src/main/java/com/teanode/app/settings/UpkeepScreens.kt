package com.teanode.app.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.padding
import com.teanode.kit.goals.goals
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material.icons.outlined.NightsStay
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberLoad
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.kit.agent.AgentAlert
import com.teanode.kit.agent.AgentBackgroundWork
import com.teanode.kit.agent.AgentDream
import com.teanode.kit.agent.AgentUpkeepPreferences
import com.teanode.kit.agent.UpkeepClock
import com.teanode.kit.agent.agentAlerts
import com.teanode.kit.agent.agentCorrections
import com.teanode.kit.agent.agentDreamState
import com.teanode.kit.agent.agentDreams
import com.teanode.kit.agent.agentRuns
import com.teanode.kit.agent.agentUpkeep
import com.teanode.kit.agent.backgroundWork
import com.teanode.kit.agent.connectedServers
import com.teanode.kit.agent.disconnectServer
import com.teanode.kit.agent.dreamNow
import com.teanode.kit.agent.gradeMemoryNow
import com.teanode.kit.agent.learnedFacts
import com.teanode.kit.agent.attachedComputers
import com.teanode.kit.agent.knowledgeSourceSecrets
import com.teanode.kit.agent.knowledgeSourceTypes
import com.teanode.kit.agent.saveKnowledgeSource
import com.teanode.kit.agent.setKnowledgeSourceSecret
import com.teanode.kit.agent.memoryCheckRuns
import com.teanode.kit.agent.memoryCheckAnswers
import com.teanode.kit.agent.updateMemoryCheckQuestion
import com.teanode.kit.agent.knowledgeSourceDetails
import com.teanode.kit.agent.syncKnowledgeSource
import com.teanode.kit.agent.switchKnowledgeSource
import com.teanode.kit.agent.deleteKnowledgeSource
import com.teanode.kit.agent.memoryCheckQuestions
import com.teanode.kit.agent.muteAgentAlert
import com.teanode.kit.agent.readingProgress
import com.teanode.kit.agent.startMemoryCheck
import com.teanode.kit.agent.stopBackgroundWork
import com.teanode.kit.agent.unmuteAgentAlert
import com.teanode.kit.agent.updateAgentUpkeep
import com.teanode.kit.photos.ServerTime

@Composable
internal fun dreamOutcomeText(outcome: AgentDream.Outcome): String = stringResource(
    when (outcome.outcomeKind) {
        AgentDream.OutcomeKind.Read -> R.string.settings_dreams_read
        AgentDream.OutcomeKind.Filed -> R.string.settings_dreams_filed
        AgentDream.OutcomeKind.Rewritten -> R.string.settings_dreams_rewritten
        AgentDream.OutcomeKind.Merged -> R.string.settings_dreams_merged
        AgentDream.OutcomeKind.Moved -> R.string.settings_dreams_moved
        AgentDream.OutcomeKind.Retired -> R.string.settings_dreams_retired
        AgentDream.OutcomeKind.Embedded -> R.string.settings_dreams_embedded
        AgentDream.OutcomeKind.Strengthened -> R.string.settings_dreams_strengthened
        AgentDream.OutcomeKind.Associated -> R.string.settings_dreams_associated
        AgentDream.OutcomeKind.Rehearsed -> R.string.settings_dreams_rehearsed
        AgentDream.OutcomeKind.Gaps -> R.string.settings_dreams_gaps
        AgentDream.OutcomeKind.Unknown -> R.string.settings_dreams_unknown
        AgentDream.OutcomeKind.Revised -> R.string.settings_dreams_revised
        AgentDream.OutcomeKind.OverviewsWritten -> R.string.settings_dreams_overviews
        AgentDream.OutcomeKind.ThemesMade -> R.string.settings_dreams_themes_made
        AgentDream.OutcomeKind.ThemesUpdated -> R.string.settings_dreams_themes_updated
        AgentDream.OutcomeKind.ReflectionsWritten -> R.string.settings_dreams_reflections
    },
    outcome.outcomeCount,
)

/** Makes or changes a source: its type, name, where it reads, the type's settings, and its secrets, which are sealed on the server and never shown again. */
@Composable
internal fun KnowledgeSourceEditorDialog(edited: com.teanode.kit.agent.KnowledgeSourceRow?, actions: ActionRunner, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val connection = LocalConnection.current
    val types = rememberLoad { connection.knowledgeSourceTypes() }
    val computers = rememberLoad { runCatching { connection.attachedComputers() }.getOrDefault(emptyList()) }
    val secretStates = rememberLoad(edited?.id) { edited?.id?.let { id -> runCatching { connection.knowledgeSourceSecrets(id) }.getOrDefault(emptyList()) } ?: emptyList() }
    val savedText = stringResource(R.string.settings_saved)
    val offered = com.teanode.kit.agent.KnowledgeSourceType.offered(types.value.orEmpty())
    val filesName = stringResource(R.string.settings_sources_files_type)
    var typeName by remember(edited?.id) { mutableStateOf(edited?.specification?.type?.ifEmpty { null } ?: if (edited != null) FILES_CHOICE else null) }
    val chosenType = offered.firstOrNull { type -> type.name == typeName }
    var name by remember(edited?.id) { mutableStateOf(edited?.name.orEmpty()) }
    var computer by remember(edited?.id) { mutableStateOf(edited?.specification?.computer.orEmpty()) }
    var path by remember(edited?.id) { mutableStateOf(edited?.specification?.path.orEmpty()) }
    var rootPath by remember(edited?.id) { mutableStateOf(edited?.rootPath.orEmpty()) }
    var cron by remember(edited?.id) { mutableStateOf(edited?.cron.orEmpty()) }
    var draft by remember(edited?.id, chosenType?.name) { mutableStateOf(chosenType?.draft(edited?.specification?.storedSettings.orEmpty()) ?: com.teanode.kit.agent.KnowledgeSourceType.Draft()) }
    var secretValues by remember(edited?.id, chosenType?.name) { mutableStateOf(mapOf<String, String>()) }
    val isFiles = typeName == FILES_CHOICE
    val isEditable = edited == null || edited.kind == "computer" || !edited.specification.type.isEmpty()
    val problem = chosenType?.firstProblem(draft)
    val isReady = isEditable && name.isNotBlank() && (isFiles && path.isNotBlank() && computer.isNotBlank() || chosenType != null && problem == null && (!chosenType.isReadOnComputer || computer.isNotBlank()))
    FormDialog(
        title = stringResource(if (edited == null) R.string.settings_sources_add else R.string.settings_sources_edit), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = isReady, isWorking = actions.isWorking,
        onConfirm = {
            actions.run(savedText, onSuccess = onSaved) {
                val savedId = if (isFiles) {
                    connection.saveKnowledgeSource(com.teanode.kit.agent.KnowledgeSourceChange(sourceId = edited?.id, name = name.trim(), kind = "computer", computer = computer.trim(), path = path.trim(), format = "files", rootPath = rootPath.trim().ifEmpty { null }, cron = cron.trim().ifEmpty { null }))
                } else {
                    val type = chosenType ?: return@run
                    connection.saveKnowledgeSource(
                        com.teanode.kit.agent.KnowledgeSourceChange(
                            sourceId = edited?.id, name = name.trim(), computer = if (type.isReadOnComputer) computer.trim() else null, rootPath = rootPath.trim().ifEmpty { null }, cron = cron.trim().ifEmpty { null },
                            type = type.name, settings = type.values(draft),
                        ),
                    )
                }
                chosenType?.secrets?.forEach { secret ->
                    val value = secretValues[secret.key].orEmpty()
                    if (value.isNotEmpty()) connection.setKnowledgeSourceSecret(savedId, secret.key, value)
                }
            }
        },
        onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            if (!isEditable) Text(stringResource(R.string.settings_sources_not_editable), style = MaterialTheme.typography.bodyMedium)
            if (edited == null) {
                ChoiceRow(
                    stringResource(R.string.settings_sources_type), listOf<String?>(null, FILES_CHOICE) + offered.filter { type -> type.reader != "files" }.map { type -> type.name }, typeName,
                    label = { choice -> when (choice) { null -> stringResource(R.string.settings_skills_choose); FILES_CHOICE -> filesName; else -> choice } }, onSelect = { typeName = it },
                )
            }
            chosenType?.description?.takeIf { text -> text.isNotEmpty() }?.let { text -> Text(com.teanode.kit.agent.KnowledgeSourceType.asSentence(text), style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(horizontal = 16.dp)) }
            if (typeName != null) {
                FormField(name, { name = it }, stringResource(R.string.settings_profile_name))
                if (isFiles || chosenType?.isReadOnComputer == true) {
                    val names = computers.value.orEmpty().map { attached -> attached.name }
                    ChoiceRow(stringResource(R.string.settings_sources_computer), listOf("") + names, computer, label = { choice -> choice.ifEmpty { stringResource(R.string.settings_skills_choose) } }, onSelect = { computer = it })
                }
                if (isFiles) FormField(path, { path = it }, stringResource(R.string.settings_sources_path))
                chosenType?.settings?.forEach { setting ->
                    val label = com.teanode.kit.agent.KnowledgeSourceType.label(setting.name)
                    if (setting.isBoolean) {
                        SwitchRow(label, draft.switches[setting.name] == true, { isOn -> draft = draft.copy(switches = draft.switches + (setting.name to isOn)) }, supporting = setting.description.ifEmpty { null }?.let { text -> com.teanode.kit.agent.KnowledgeSourceType.asSentence(text) })
                    } else if (setting.choices != null) {
                        ChoiceRow(label, listOf("") + setting.choices.orEmpty(), draft.texts[setting.name].orEmpty(), label = { choice -> choice.ifEmpty { stringResource(R.string.settings_sources_default) } }, onSelect = { chosen -> draft = draft.copy(texts = draft.texts + (setting.name to chosen)) })
                    } else {
                        FormField(
                            draft.texts[setting.name].orEmpty(), { typed -> draft = draft.copy(texts = draft.texts + (setting.name to typed)) }, label + if (setting.isRequired) " *" else "",
                            supporting = setting.description.ifEmpty { null }?.let { text -> com.teanode.kit.agent.KnowledgeSourceType.asSentence(text) }, isError = setting.problem(draft) != null,
                            keyboardType = if (setting.settingType == "integer") KeyboardType.Number else KeyboardType.Text,
                        )
                    }
                }
                chosenType?.secrets?.forEach { secret ->
                    val isKept = secretStates.value.orEmpty().any { state -> state.key == secret.key && state.isSet }
                    FormField(
                        secretValues[secret.key].orEmpty(), { typed -> secretValues = secretValues + (secret.key to typed) }, com.teanode.kit.agent.KnowledgeSourceType.label(secret.key), isPassword = true,
                        supporting = stringResource(if (isKept) R.string.settings_sources_secret_kept else R.string.settings_sources_secret_new) + (secret.description.ifEmpty { null }?.let { text -> " " + com.teanode.kit.agent.KnowledgeSourceType.asSentence(text) } ?: ""),
                    )
                }
                FormField(rootPath, { rootPath = it }, stringResource(R.string.settings_sources_root_path))
                FormField(cron, { cron = it }, stringResource(R.string.settings_sources_cron_label), supporting = stringResource(R.string.settings_sources_cron_hint))
                chosenType?.problem?.takeIf { text -> text.isNotEmpty() }?.let { text -> Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 16.dp)) }
            }
        }
    }
}

internal const val FILES_CHOICE = "__files__"
