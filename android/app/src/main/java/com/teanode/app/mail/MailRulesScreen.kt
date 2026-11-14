package com.teanode.app.mail

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ArrowDownward
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Pause
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilterChip
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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.mail.MailFolder
import com.teanode.kit.mail.MailFolders
import com.teanode.kit.mail.MailRule
import com.teanode.kit.mail.MailRuleTrial
import com.teanode.kit.mail.testMailboxRules
import com.teanode.kit.mail.updateMailboxRules
import kotlinx.coroutines.launch

/** The rules that sort mail as it arrives: in the order they run, each turned off, moved or removed, with a try on recent mail. */
@Composable
fun MailRulesScreen(mail: MailModel, onBack: () -> Unit, startsEditing: Boolean = false) {
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    var rules by remember { mutableStateOf(mail.mailbox?.rules ?: emptyList()) }
    var editingIndex by remember { mutableStateOf<Int?>(null) }
    var isEditing by remember { mutableStateOf(startsEditing) }
    var editedRule by remember { mutableStateOf(MailRule()) }
    var removingIndex by remember { mutableStateOf<Int?>(null) }
    var trials by remember { mutableStateOf<List<MailRuleTrial>?>(null) }
    var isTrying by remember { mutableStateOf(false) }
    var menuIndex by remember { mutableStateOf<Int?>(null) }
    val mailboxId = mail.mailbox?.id ?: ""
    val savedRules = mail.mailbox?.rules
    LaunchedEffect(mail) { if (!mail.isLoaded) mail.load() }
    LaunchedEffect(savedRules) {
        rules = savedRules ?: emptyList()
        trials = null
    }
    val words = MailRuleWords(mail.folders, context)

    suspend fun save(next: List<MailRule>): Boolean {
        rules = next
        return mail.act(context.getString(R.string.mail_saved)) { mail.connection.updateMailboxRules(mailboxId, next) }
    }

    if (isEditing) {
        BackHandler { isEditing = false }
        MailRuleEditor(mail, editedRule, isNew = editingIndex == null, words = words, onCancel = { isEditing = false }, onSave = { rule ->
            val next = rules.toMutableList()
            val index = editingIndex
            if (index != null && next.indices.contains(index)) next[index] = rule else next.add(rule)
            val isSaved = save(next)
            if (isSaved) isEditing = false
            isSaved
        })
        return
    }

    TeaNodeScreen(
        title = stringResource(R.string.mail_rules_title),
        onBack = onBack,
        actions = {
            IconButton(onClick = {
                editingIndex = null
                editedRule = MailRule()
                isEditing = true
            }) { Icon(Icons.Outlined.Add, stringResource(R.string.mail_add_rule)) }
        },
    ) { contentPadding ->
        LazyColumn(Modifier.padding(contentPadding).fillMaxSize()) {
            if (rules.isEmpty()) item { Text(stringResource(R.string.mail_no_rules_yet), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            itemsIndexed(rules) { index, rule ->
                Row(Modifier.fillMaxWidth().clickable {
                    editingIndex = index
                    editedRule = rule
                    isEditing = true
                }.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                    MailRuleRow(rule, words, Modifier.weight(1f))
                    Box {
                        IconButton(onClick = { menuIndex = index }) { Icon(Icons.Outlined.MoreVert, stringResource(R.string.mail_actions)) }
                        DropdownMenu(expanded = menuIndex == index, onDismissRequest = { menuIndex = null }) {
                            DropdownMenuItem(
                                text = { Text(stringResource(if (rule.enabled) R.string.mail_turn_off else R.string.mail_turn_on)) },
                                leadingIcon = { Icon(if (rule.enabled) Icons.Outlined.Pause else Icons.Outlined.PlayArrow, null) },
                                onClick = { menuIndex = null; val next = rules.toMutableList(); next[index] = rule.copy(enabled = !rule.enabled); scope.launch { save(next) } },
                            )
                            if (index > 0) DropdownMenuItem(text = { Text(stringResource(R.string.mail_move_up)) }, leadingIcon = { Icon(Icons.Outlined.ArrowUpward, null) }, onClick = {
                                menuIndex = null
                                val next = rules.toMutableList()
                                next.add(index - 1, next.removeAt(index))
                                scope.launch { save(next) }
                            })
                            if (index < rules.size - 1) DropdownMenuItem(text = { Text(stringResource(R.string.mail_move_down)) }, leadingIcon = { Icon(Icons.Outlined.ArrowDownward, null) }, onClick = {
                                menuIndex = null
                                val next = rules.toMutableList()
                                next.add(index + 1, next.removeAt(index))
                                scope.launch { save(next) }
                            })
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.mail_remove_rule), color = MaterialTheme.colorScheme.error) },
                                leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
                                onClick = { menuIndex = null; removingIndex = index },
                            )
                        }
                    }
                }
                HorizontalDivider()
            }
            item { Text(stringResource(R.string.mail_rules_footer), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            if (rules.isNotEmpty()) {
                item {
                    TextButton(onClick = {
                        scope.launch {
                            isTrying = true
                            try {
                                trials = mail.connection.testMailboxRules(mailboxId, rules)
                            } catch (failed: kotlinx.coroutines.CancellationException) {
                                throw failed
                            } catch (failed: Throwable) {
                                mail.snackbar?.showProblem(context.getString(R.string.mail_that_did_not_work, failed.message.orEmpty()))
                            } finally {
                                isTrying = false
                            }
                        }
                    }, enabled = !isTrying, modifier = Modifier.padding(horizontal = 8.dp)) {
                        Text(stringResource(R.string.mail_try_on_recent_mail))
                        if (isTrying) {
                            Spacer(Modifier.width(12.dp))
                            CircularProgressIndicator(Modifier.size(18.dp))
                        }
                    }
                }
                val shownTrials = trials
                if (shownTrials != null) {
                    item { Text(stringResource(R.string.mail_what_rules_would_do), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary) }
                    itemsIndexed(shownTrials) { _, trial ->
                        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
                            Text(trial.item.mail?.subject?.ifEmpty { null } ?: stringResource(R.string.mail_no_subject), maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text(trial.item.mail?.from ?: trial.item.mail?.sender ?: "", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                            val names = trial.matched.mapNotNull { match -> rules.getOrNull(match)?.let { rule -> words.ruleName(rule) } }
                            Text(if (names.isEmpty()) stringResource(R.string.mail_no_rule) else names.joinToString(", "), style = MaterialTheme.typography.bodySmall, color = if (trial.matched.isEmpty()) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.primary)
                        }
                    }
                    item { Text(stringResource(R.string.mail_rules_trial_footer), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
            }
        }
    }

    removingIndex?.let { index ->
        val removed = rules.getOrNull(index)
        if (removed != null) {
            ConfirmationDialog(
                title = stringResource(R.string.mail_remove_rule), text = stringResource(R.string.mail_remove_rule_question, words.ruleName(removed)), confirmLabel = stringResource(R.string.mail_remove_rule), isDestructive = true,
                onConfirm = {
                    removingIndex = null
                    val next = rules.toMutableList()
                    if (next.indices.contains(index)) next.removeAt(index)
                    scope.launch { save(next) }
                },
                onDismiss = { removingIndex = null },
            )
        }
    }
}

@Composable
fun MailRuleRow(rule: MailRule, words: MailRuleWords, modifier: Modifier = Modifier) {
    Column(modifier, verticalArrangement = Arrangement.spacedBy(3.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(words.ruleName(rule), color = if (rule.enabled) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant)
            if (!rule.enabled) Text(stringResource(R.string.mail_off), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (rule.stop) Text(stringResource(R.string.mail_stops_here), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Text(words.summary(rule), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
    }
}

/** The form of one rule: its name, its conditions that must all match and the actions to run. */
@Composable
fun MailRuleEditor(mail: MailModel, starting: MailRule, isNew: Boolean, words: MailRuleWords, onCancel: () -> Unit, onSave: suspend (MailRule) -> Boolean) {
    val scope = rememberCoroutineScope()
    var rule by remember { mutableStateOf(starting) }
    val conditions = remember { mutableStateListOf<MailRule.Condition>().also { list -> list.addAll(starting.conditions) } }
    val actions = remember { mutableStateListOf<MailRule.Action>().also { list -> list.addAll(starting.actions) } }
    var isSaving by remember { mutableStateOf(false) }
    TeaNodeScreen(
        title = stringResource(if (isNew) R.string.mail_add_a_rule else R.string.mail_edit_rule),
        onBack = onCancel,
        actions = {
            TextButton(enabled = !isSaving && conditions.isNotEmpty() && actions.isNotEmpty(), onClick = {
                isSaving = true
                scope.launch {
                    onSave(rule.copy(conditions = conditions.toList(), actions = actions.toList()))
                    isSaving = false
                }
            }) { Text(stringResource(R.string.mail_save)) }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedTextField(rule.name, { typed -> rule = rule.copy(name = typed) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_rule_name)) }, singleLine = true)
            Row(verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.mail_on), Modifier.weight(1f)); Switch(rule.enabled, { checked -> rule = rule.copy(enabled = checked) }) }
            Row(verticalAlignment = Alignment.CenterVertically) { Text(stringResource(R.string.mail_stop_after_this_rule), Modifier.weight(1f)); Switch(rule.stop, { checked -> rule = rule.copy(stop = checked) }) }
            Text(stringResource(R.string.mail_when_all_match), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            conditions.forEachIndexed { index, condition ->
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.weight(1f)) { MailConditionEditor(condition, { changed -> conditions[index] = changed }, words) }
                        IconButton(onClick = { conditions.removeAt(index) }) { Icon(Icons.Outlined.Close, stringResource(R.string.mail_remove)) }
                    }
                    HorizontalDivider()
                }
            }
            OutlinedButton(onClick = { conditions.add(MailRule.Condition()) }) { Icon(Icons.Outlined.Add, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text(stringResource(R.string.mail_add_a_condition)) }
            Text(stringResource(R.string.mail_do_this), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            actions.forEachIndexed { index, action ->
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.weight(1f)) { MailActionEditor(mail, action, { changed -> actions[index] = changed }, words) }
                        IconButton(onClick = { actions.removeAt(index) }) { Icon(Icons.Outlined.Close, stringResource(R.string.mail_remove)) }
                    }
                    HorizontalDivider()
                }
            }
            OutlinedButton(onClick = { actions.add(MailRule.Action()) }) { Icon(Icons.Outlined.Add, null, Modifier.size(18.dp)); Spacer(Modifier.width(8.dp)); Text(stringResource(R.string.mail_add_an_action)) }
        }
    }
}

@Composable
private fun <Choice> ChoiceMenu(label: String, choices: List<Choice>, selected: Choice, name: @Composable (Choice) -> String, onChoose: (Choice) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    Box {
        OutlinedButton(onClick = { isOpen = true }, modifier = Modifier.fillMaxWidth()) {
            Text("$label: ", color = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(name(selected), modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            for (choice in choices) DropdownMenuItem(text = { Text(name(choice)) }, onClick = { isOpen = false; onChoose(choice) })
        }
    }
}

@Composable
private fun MailActionEditor(mail: MailModel, action: MailRule.Action, onChange: (MailRule.Action) -> Unit, words: MailRuleWords) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        ChoiceMenu(stringResource(R.string.mail_action), MailRule.actionKinds, action.kind, { kind -> words.action(kind) }) { kind -> onChange(action.copy(kind = kind)) }
        when (action.kind) {
            "move" -> {
                val rows = MailFolders.rows(mail.folders)
                val chosen = rows.firstOrNull { row -> row.folder.id == action.folderId }?.folder
                ChoiceMenu<MailFolder?>(
                    stringResource(R.string.mail_folder), listOf<MailFolder?>(null) + rows.map { row -> row.folder }, chosen,
                    { folder -> if (folder == null) stringResource(R.string.mail_choose_a_folder) else MailNames.folder(folder) },
                ) { folder -> onChange(action.copy(folderId = folder?.id ?: "")) }
            }
            "forward" -> OutlinedTextField(
                action.address ?: "", { typed -> onChange(action.copy(address = typed)) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_address_to_forward_to)) }, singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false),
            )
        }
    }
}

@Composable
private fun MailConditionEditor(condition: MailRule.Condition, onChange: (MailRule.Condition) -> Unit, words: MailRuleWords) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        ChoiceMenu(stringResource(R.string.mail_field), MailRule.fields, condition.field, { field -> words.field(field) }) { field ->
            val operators = MailRule.Condition.operators(field)
            onChange(
                condition.copy(
                    field = field, comparisonOperator = if (operators.contains(condition.comparisonOperator)) condition.comparisonOperator else operators[0],
                    value = if (field == "priority") "" else condition.value,
                ),
            )
        }
        if (condition.field == "header") {
            OutlinedTextField(condition.header ?: "", { typed -> onChange(condition.copy(header = typed)) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_header_name)) }, singleLine = true)
        }
        if (!condition.isBare) {
            val operators = MailRule.Condition.operators(condition.field)
            if (operators.size > 1) {
                ChoiceMenu(stringResource(R.string.mail_how_it_compares), operators, condition.comparisonOperator, { comparison -> words.comparison(comparison) }) { comparison -> onChange(condition.copy(comparisonOperator = comparison)) }
            }
            when (condition.field) {
                "priority" -> ChoiceMenu(stringResource(R.string.mail_what_it_looks_for), listOf("") + MailRule.priorities, condition.value ?: "", { priority -> if (priority.isEmpty()) stringResource(R.string.mail_choose) else words.priority(priority) }) { priority -> onChange(condition.copy(value = priority)) }
                "score" -> OutlinedTextField(condition.value ?: "", { typed -> onChange(condition.copy(value = typed)) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_what_it_looks_for)) }, singleLine = true, keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal))
                "category" -> OutlinedTextField(condition.value ?: "", { typed -> onChange(condition.copy(value = typed)) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_category_hint)) }, singleLine = true, keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None))
                else -> OutlinedTextField(condition.value ?: "", { typed -> onChange(condition.copy(value = typed)) }, Modifier.fillMaxWidth(), label = { Text(stringResource(R.string.mail_what_it_looks_for)) }, singleLine = true, keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false))
            }
        }
    }
}

/** The words of rules: what a field, a comparison and an action are called, and a rule said in a line. */
class MailRuleWords(private val folders: List<MailFolder>, private val context: android.content.Context) {
    fun ruleName(rule: MailRule): String = if (rule.name.isBlank()) context.getString(R.string.mail_unnamed_rule) else rule.name

    fun summary(rule: MailRule): String {
        val conditions = rule.conditions.joinToString(", ") { condition -> describe(condition) }
        val actions = rule.actions.joinToString(", ") { action -> describe(action) }
        return context.getString(R.string.mail_rule_when, conditions) + " · " + context.getString(R.string.mail_rule_then, actions)
    }

    fun field(field: String): String = context.getString(
        when (field) {
            "from" -> R.string.mail_from
            "to" -> R.string.mail_to
            "subject" -> R.string.mail_subject
            "header" -> R.string.mail_header
            "score" -> R.string.mail_spam_score
            "sender-known" -> R.string.mail_sender_is_contact
            "category" -> R.string.mail_category_as_sorted
            "priority" -> R.string.mail_priority_as_sorted
            "needs-reply" -> R.string.mail_agent_says_needs_reply
            else -> R.string.mail_any_message
        },
    )

    private fun shortField(field: String): String = when (field) {
        "category" -> context.getString(R.string.mail_category)
        "priority" -> context.getString(R.string.mail_priority_label)
        "needs-reply" -> context.getString(R.string.mail_needs_a_reply)
        else -> field(field)
    }

    fun comparison(comparison: String): String = context.getString(
        when (comparison) {
            "equals" -> R.string.mail_compare_is
            "matches" -> R.string.mail_compare_matches
            "above" -> R.string.mail_compare_above
            "below" -> R.string.mail_compare_below
            else -> R.string.mail_compare_contains
        },
    )

    fun action(kind: String): String = context.getString(
        when (kind) {
            "markRead" -> R.string.mail_action_mark_read
            "flag" -> R.string.mail_star
            "forward" -> R.string.mail_action_forward_to
            "delete" -> R.string.mail_delete
            else -> R.string.mail_action_move_to_folder
        },
    )

    fun priority(priority: String): String = context.getString(
        when (priority) {
            "high" -> R.string.mail_priority_high
            "low" -> R.string.mail_priority_low
            else -> R.string.mail_priority_normal
        },
    )

    fun describe(condition: MailRule.Condition): String {
        val name = shortField(condition.field)
        if (condition.isBare) return name
        val header = if (condition.field == "header") " " + (condition.header ?: "") else ""
        val value = if (condition.field == "priority" && !condition.value.isNullOrEmpty()) priority(condition.value ?: "") else condition.value ?: ""
        return "$name$header ${comparison(condition.comparisonOperator)} $value".trim()
    }

    private fun describe(action: MailRule.Action): String {
        val kind = action(action.kind)
        return when (action.kind) {
            "move" -> kind + " " + (folders.firstOrNull { folder -> folder.id == action.folderId }?.let { folder -> MailNames.folder(folder, context) } ?: context.getString(R.string.mail_choose_a_folder))
            "forward" -> "$kind ${action.address ?: ""}".trim()
            else -> kind
        }
    }
}
