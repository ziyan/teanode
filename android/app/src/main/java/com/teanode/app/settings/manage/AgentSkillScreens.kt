package com.teanode.app.settings.manage

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.settings.ChoiceRow
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.FormDialog
import com.teanode.app.settings.FormField
import com.teanode.app.settings.LoadedSettingsPage
import com.teanode.app.settings.rememberActionRunner
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.asReasonSentence
import com.teanode.kit.manage.InstalledSkill
import com.teanode.kit.manage.InstalledSourceType
import com.teanode.kit.manage.RegistryOffer
import com.teanode.kit.manage.SkillSecretsDraft
import com.teanode.kit.manage.filledSkillSecrets
import com.teanode.kit.manage.installSkill
import com.teanode.kit.manage.installSourceType
import com.teanode.kit.manage.installedSkills
import com.teanode.kit.manage.installedSourceTypes
import com.teanode.kit.manage.removeSkill
import com.teanode.kit.manage.removeSourceType
import com.teanode.kit.manage.saveSkillSecrets
import com.teanode.kit.manage.setSkillEnabled
import com.teanode.kit.manage.setSkillScope
import com.teanode.kit.manage.skillRegistry
import com.teanode.kit.manage.sourceTypeRegistry

/** What the registry offers, a skill or a source type a row, each with the button that installs it, updates it, or says it is installed. */
@Composable
private fun RegistryRows(
    title: String,
    description: String,
    offers: List<RegistryOffer>?,
    failedText: String?,
    filterText: String,
    busyName: String?,
    install: (RegistryOffer) -> Unit,
) {
    TeaNodeSectionHeader(title)
    if (offers == null) {
        TeaNodeRow(headline = failedText ?: stringResource(R.string.manage_asking_registry))
    } else if (offers.isEmpty()) {
        TeaNodeRow(headline = stringResource(R.string.manage_registry_nothing))
    }
    (offers ?: emptyList()).filter { offer -> offer.matches(filterText) }.forEach { offer ->
        TeaNodeRow(
            headline = offer.name, overline = offer.version.ifEmpty { null },
            supporting = listOfNotNull(offer.description.ifEmpty { null }, offer.tags.takeIf { tags -> tags.isNotEmpty() }?.joinToString(" · ")).joinToString("\n").ifEmpty { null }, supportingMaxLines = 4,
            trailing = {
                if (busyName == offer.name) {
                    androidx.compose.material3.CircularProgressIndicator(Modifier.padding(8.dp), strokeWidth = 2.dp)
                } else {
                    OutlinedButton(onClick = { install(offer) }, enabled = !offer.isCurrent && busyName == null) {
                        Text(
                            if (offer.isNewer) stringResource(R.string.manage_update_to, offer.version) else stringResource(if (offer.isInstalled) R.string.manage_installed else R.string.manage_install),
                        )
                    }
                }
            },
        )
    }
    FooterText(description)
}

@Composable
private fun skillDetails(skill: InstalledSkill): String {
    val parts = mutableListOf(skill.description)
    if (skill.tools.isNotEmpty()) parts += stringResource(R.string.manage_skill_brings, skill.tools.joinToString(", ") { tool -> tool.name })
    if (skill.secrets.isNotEmpty()) parts += stringResource(R.string.manage_skill_needs, skill.secrets.joinToString(", "))
    if (skill.personalSecrets.isNotEmpty()) parts += stringResource(R.string.manage_skill_needs_each, skill.personalSecrets.joinToString(", "))
    if (!skill.isReadable) parts += skill.problem.orEmpty()
    return parts.filter { part -> part.isNotEmpty() }.joinToString(" · ")
}

/**
 * The skills installed on this server, each opening to what it brings and the controls for it, and what the registry offers, filtered by what
 * is typed. Uploading a skill package is done on the dashboard.
 */
@Composable
fun AgentSkillsScreen(onBack: () -> Unit, onOpenSkill: (String) -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val resources = LocalContext.current.resources
    val installed = rememberLoad { connection.installedSkills() }
    val registry = rememberLoad { connection.skillRegistry() }
    var filterText by remember { mutableStateOf("") }
    var busyName by remember { mutableStateOf<String?>(null) }
    LoadedSettingsPage(stringResource(R.string.manage_skills), onBack, installed) { skills ->
        item { TeaNodeSectionHeader(stringResource(R.string.manage_skills)) }
        if (skills.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.manage_no_skills)) }
        items(skills.size) { index ->
            val skill = skills[index]
            TeaNodeRow(
                headline = skill.name,
                overline = listOfNotNull(
                    skill.version.ifEmpty { null }, if (skill.doesRunCommands) stringResource(R.string.manage_runs_commands) else null,
                    if (!skill.isReadable) stringResource(R.string.manage_cannot_be_read) else null, if (!skill.isEnabled) stringResource(R.string.manage_off) else null,
                ).joinToString(" · ").ifEmpty { null },
                supporting = skillDetails(skill), supportingMaxLines = 3, onClick = { onOpenSkill(skill.name) },
            )
        }
        item {
            FooterText(stringResource(R.string.manage_skills_footer))
            if (RegistryOffer.isBehind(skills.size, registry.value ?: emptyList())) FooterText(stringResource(R.string.manage_registry_newer))
            FormField(filterText, { typed -> filterText = typed }, stringResource(R.string.manage_filter_registry))
            RegistryRows(
                stringResource(R.string.manage_registry_offers), stringResource(R.string.manage_skills_registry_footer), registry.value,
                if (registry.error != null) stringResource(R.string.manage_registry_unreachable) else null, filterText, busyName,
            ) { offer ->
                busyName = offer.name
                actions.run(resources.getString(R.string.manage_installed_named, offer.name, offer.version), onSuccess = { installed.refresh(); registry.refresh() }) {
                    try { connection.installSkill(offer.name) } finally { busyName = null }
                }
            }
        }
    }
}

/** One skill: what it brings, the keys it asks for and who fills them in, whether it is offered, and removing it. */
@Composable
fun InstalledSkillScreen(skillName: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val resources = LocalContext.current.resources
    val installed = rememberLoad { connection.installedSkills() }
    val filled = rememberLoad { runCatching { connection.filledSkillSecrets() }.getOrDefault(emptySet()) }
    var isFillingKeys by remember { mutableStateOf(false) }
    var isRemoving by remember { mutableStateOf(false) }
    LoadedSettingsPage(skillName, onBack, installed, isEmpty = { list -> list.none { candidate -> candidate.name == skillName } }, empty = { EmptyState(Icons.Outlined.Extension, stringResource(R.string.manage_skill_gone)) }) { skills ->
        val skill = skills.first { candidate -> candidate.name == skillName }
        val filledNames = filled.value ?: emptySet()
        item {
            if (skill.description.isNotEmpty()) TeaNodeRow(headline = skill.description, supportingMaxLines = 6)
            TeaNodeRow(headline = stringResource(R.string.settings_version), supporting = skill.version)
            if (skill.publisher.isNotEmpty()) TeaNodeRow(headline = stringResource(R.string.manage_skill_publisher), supporting = skill.publisher)
            if (!skill.isReadable) Text((skill.problem ?: stringResource(R.string.manage_cannot_be_read)).asReasonSentence(), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.error)
        }
        if (skill.tools.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.manage_skill_tools)) }
            items(skill.tools.size) { index ->
                val tool = skill.tools[index]
                TeaNodeRow(headline = tool.name, overline = if (tool.doesNeedComputer) stringResource(R.string.manage_runs_commands) else null, supporting = tool.description.ifEmpty { null }, supportingMaxLines = 4)
            }
        }
        if (skill.hasSecrets) {
            item {
                TeaNodeSectionHeader(stringResource(R.string.manage_skill_keys))
                if (skill.secrets.isNotEmpty()) TeaNodeRow(
                    headline = stringResource(R.string.manage_skill_keys), supporting = stringResource(R.string.manage_skill_keys_set, skill.filledSecretCount(filledNames), skill.secrets.size),
                    onClick = { isFillingKeys = true },
                )
                val asSkillSays = stringResource(R.string.manage_scope_as_skill_says)
                val forEverybody = stringResource(R.string.manage_scope_operator)
                val eachPerson = stringResource(R.string.manage_scope_person)
                ChoiceRow(
                    stringResource(R.string.manage_skill_who_fills), listOf("", "operator", "person"), skill.skillScope,
                    label = { scope -> when (scope) { "operator" -> forEverybody; "person" -> eachPerson; else -> asSkillSays } },
                    onSelect = { scope ->
                        actions.run(resources.getString(R.string.manage_skill_changed_who, skill.name), onSuccess = { installed.refresh() }) { connection.setSkillScope(skill.name, scope) }
                    },
                )
                if (skill.secrets.isNotEmpty()) FooterText(stringResource(R.string.manage_skill_needs_period, skill.secrets.joinToString(", ")))
                if (skill.personalSecrets.isNotEmpty()) FooterText(stringResource(R.string.manage_skill_needs_each_period, skill.personalSecrets.joinToString(", ")))
            }
        }
        item {
            TeaNodeRow(
                headline = stringResource(if (skill.isEnabled) R.string.manage_stop_offering else R.string.manage_offer_it),
                onClick = if (actions.isWorking) null else ({
                    val isEnabled = !skill.isEnabled
                    actions.run(resources.getString(if (isEnabled) R.string.manage_skill_offered_again else R.string.manage_skill_not_offered, skill.name), onSuccess = { installed.refresh() }) { connection.setSkillEnabled(skill.name, isEnabled) }
                }),
            )
            TeaNodeRow(headline = stringResource(R.string.settings_remove), leadingIcon = Icons.Outlined.Delete, onClick = { isRemoving = true })
        }
    }
    val shownSkill = installed.value?.firstOrNull { candidate -> candidate.name == skillName }
    if (isFillingKeys && shownSkill != null) SkillSecretsDialog(shownSkill, filled.value ?: emptySet(), onDismiss = { isFillingKeys = false }, onSaved = { isFillingKeys = false; filled.refresh() })
    if (isRemoving) ConfirmationDialog(
        title = stringResource(R.string.manage_remove_named, skillName), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.manage_remove_skill_detail),
        onConfirm = { isRemoving = false; actions.run(resources.getString(R.string.manage_removed_named, skillName), onSuccess = onBack) { connection.removeSkill(skillName) } },
        onDismiss = { isRemoving = false },
    )
}

/** Fills in the keys a skill asks the operator for, one value each for everybody. A value is written and never shown again; a key left blank keeps its value. */
@Composable
private fun SkillSecretsDialog(skill: InstalledSkill, filledNames: Set<String>, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val resources = LocalContext.current.resources
    var draft by remember { mutableStateOf(SkillSecretsDraft(skill.name)) }
    FormDialog(
        title = stringResource(R.string.manage_keys_for, skill.name), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = true, isWorking = actions.isWorking, onDismiss = onDismiss,
        onConfirm = {
            val typedKeys = draft.typedKeys(skill.secrets)
            if (typedKeys.isEmpty()) onDismiss() else actions.run(resources.getString(R.string.manage_kept_keys, typedKeys.joinToString(", "), skill.name), onSuccess = onSaved) { connection.saveSkillSecrets(draft, skill.secrets) }
        },
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            Text(stringResource(R.string.manage_keys_footer), style = MaterialTheme.typography.bodySmall)
            skill.secrets.forEach { key ->
                val isFilled = filledNames.contains("${skill.name}/$key")
                FormField(
                    draft.valueByKey[key].orEmpty(), { typed -> draft = draft.copy(valueByKey = draft.valueByKey + (key to typed)) }, key, isPassword = true,
                    placeholder = if (isFilled) stringResource(R.string.manage_keys_leave_blank) else null, supporting = stringResource(if (isFilled) R.string.manage_keys_set else R.string.manage_keys_not_set),
                )
            }
        }
    }
}

@Composable
private fun readerWord(reader: String): String = stringResource(
    when (reader) {
        "files" -> R.string.manage_reader_files
        "journal" -> R.string.manage_reader_journal
        "sent" -> R.string.manage_reader_sent
        "web" -> R.string.manage_reader_web
        "" -> R.string.manage_reader_commands
        else -> R.string.manage_reader_unknown
    },
)

@Composable
private fun sourceTypeDetails(sourceType: InstalledSourceType): String {
    val parts = mutableListOf(sourceType.description)
    if (sourceType.runs.isNotEmpty()) {
        val onServer = stringResource(R.string.manage_runs_on_server)
        val onComputer = stringResource(R.string.manage_runs_on_computer)
        val places = sourceType.runs.joinToString(", ") { place -> when (place) { "server" -> onServer; "computer" -> onComputer; else -> place } }
        parts += stringResource(R.string.manage_source_runs_on, places)
    }
    if (sourceType.requires.isNotEmpty()) parts += stringResource(R.string.manage_skill_needs, sourceType.requires.joinToString(", "))
    if (!sourceType.isReadable) parts += sourceType.problem.orEmpty()
    return parts.filter { part -> part.isNotEmpty() }.joinToString(" · ")
}

/** How a knowledge source is read: the types installed here, each with what it says about itself, and what the registry offers. */
@Composable
fun AgentSourceTypesScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val resources = LocalContext.current.resources
    val installed = rememberLoad { connection.installedSourceTypes() }
    val registry = rememberLoad { connection.sourceTypeRegistry() }
    var filterText by remember { mutableStateOf("") }
    var busyName by remember { mutableStateOf<String?>(null) }
    var reading by remember { mutableStateOf<InstalledSourceType?>(null) }
    var removing by remember { mutableStateOf<InstalledSourceType?>(null) }
    LoadedSettingsPage(stringResource(R.string.manage_source_types), onBack, installed) { sourceTypes ->
        item { TeaNodeSectionHeader(stringResource(R.string.manage_source_types)) }
        if (sourceTypes.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.manage_no_source_types)) }
        items(sourceTypes.size) { index ->
            val sourceType = sourceTypes[index]
            TeaNodeRow(
                headline = sourceType.name,
                overline = listOfNotNull(
                    sourceType.version.ifEmpty { null }, if (sourceType.isLocal) stringResource(R.string.manage_local) else null,
                    if (sourceType.isReadable) readerWord(sourceType.reader) else stringResource(R.string.manage_cannot_be_read),
                ).joinToString(" · "),
                supporting = sourceTypeDetails(sourceType), supportingMaxLines = 3, onClick = if (sourceType.hasGuide) ({ reading = sourceType }) else null,
                trailing = { androidx.compose.material3.TextButton(onClick = { removing = sourceType }, enabled = busyName != sourceType.name) { Text(stringResource(R.string.settings_remove)) } },
            )
        }
        item {
            FooterText(stringResource(R.string.manage_source_types_footer))
            if (RegistryOffer.isBehind(sourceTypes.size, registry.value ?: emptyList())) FooterText(stringResource(R.string.manage_source_registry_newer))
            FormField(filterText, { typed -> filterText = typed }, stringResource(R.string.manage_filter_registry))
            RegistryRows(
                stringResource(R.string.manage_source_registry_offers), stringResource(R.string.manage_source_registry_footer), registry.value,
                if (registry.error != null) stringResource(R.string.manage_source_registry_unreachable) else null, filterText, busyName,
            ) { offer ->
                busyName = offer.name
                actions.run(resources.getString(R.string.manage_installed_named, offer.name, offer.version), onSuccess = { installed.refresh(); registry.refresh() }) {
                    try { connection.installSourceType(offer.name) } finally { busyName = null }
                }
            }
        }
    }
    reading?.let { sourceType ->
        FormDialog(
            title = stringResource(R.string.manage_about_named, sourceType.name), confirmLabel = stringResource(R.string.common_done), isConfirmEnabled = true, onConfirm = { reading = null }, onDismiss = { reading = null },
        ) {
            Column(Modifier.verticalScroll(rememberScrollState())) { com.teanode.app.knowledge.MarkdownText(sourceType.guide) }
        }
    }
    removing?.let { sourceType ->
        ConfirmationDialog(
            title = stringResource(R.string.manage_remove_named, sourceType.name), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.manage_remove_source_type_detail),
            onConfirm = {
                removing = null
                busyName = sourceType.name
                // Refused while a source is of this type; the server says which, and the snackbar carries its words.
                actions.run(resources.getString(R.string.manage_removed_named, sourceType.name), onSuccess = { installed.refresh(); registry.refresh() }) {
                    try { connection.removeSourceType(sourceType.name) } finally { busyName = null }
                }
            },
            onDismiss = { removing = null },
        )
    }
}
