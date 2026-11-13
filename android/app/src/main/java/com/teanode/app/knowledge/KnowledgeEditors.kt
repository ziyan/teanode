package com.teanode.app.knowledge

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.ChoiceRow
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.knowledge.KnowledgeContactChoice
import com.teanode.kit.knowledge.KnowledgeNode
import com.teanode.kit.knowledge.KnowledgeWords
import com.teanode.kit.knowledge.contactChoices
import com.teanode.kit.knowledge.linkKnowledgeNodes
import com.teanode.kit.knowledge.saveKnowledgeFact
import com.teanode.kit.knowledge.saveKnowledgeNode
import com.teanode.kit.knowledge.searchKnowledge
import com.teanode.kit.knowledge.setKnowledgeContact
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** FactDraft is a fact being added or changed. */
internal data class FactDraft(
    val number: Int? = null,
    val text: String = "",
    val kind: String = "fact",
    val happened: String = "",
    val audiences: Set<String> = emptySet(),
    val originalAudiences: Set<String> = emptySet(),
) {
    companion object {
        fun of(fact: com.teanode.kit.knowledge.KnowledgeFact): FactDraft {
            val audiences = fact.audiences.orEmpty().toSet()
            return FactDraft(
                number = fact.number, text = fact.text, kind = if (fact.kind in KnowledgeWords.factKinds) fact.kind else "fact",
                happened = (fact.happenedAt ?: "").take(10), audiences = audiences, originalAudiences = audiences,
            )
        }
    }
}

/** Draws whichever screen a page has opened over itself. */
@Composable
internal fun PageSheets(model: KnowledgePageModel, node: KnowledgeNode, sheet: PageSheet, onClose: () -> Unit, onChosen: (PageChoice, String) -> Unit) {
    when (sheet) {
        PageSheet.Edit -> PageEditor(model, node, onClose)
        is PageSheet.Fact -> FactEditor(model, sheet.draft, onClose)
        is PageSheet.Choose -> PagePicker(
            title = stringResource(
                when (sheet.choice) {
                    PageChoice.Move -> R.string.knowledge_file_it_under
                    PageChoice.Merge -> R.string.knowledge_merge_it_into
                    is PageChoice.MoveFact -> R.string.knowledge_move_it_to
                },
            ),
            excluding = model.pagePath, initial = sheet.choice.initial.ifEmpty { if (sheet.choice == PageChoice.Move) node.parentPath else "" }, onClose = onClose,
        ) { chosen -> onChosen(sheet.choice, chosen) }
        PageSheet.Link -> LinkEditor(model, onClose)
        PageSheet.LinkContact -> ContactPicker(model, node.contactId.orEmpty(), onClose)
        PageSheet.Share -> KnowledgeShareScreen(model.pagePath, onClose)
    }
}

/** PageEditor changes a page's name, other names and summary. */
@Composable
internal fun PageEditor(model: KnowledgePageModel, node: KnowledgeNode, onClose: () -> Unit) {
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf(node.name) }
    var aliases by remember { mutableStateOf(node.aliases.orEmpty().joinToString(", ")) }
    var summary by remember { mutableStateOf(node.summary) }
    val saidSaved = stringResource(R.string.knowledge_saved)
    TeaNodeScreen(
        title = stringResource(R.string.knowledge_edit_page), onBack = onClose,
        actions = {
            TextButton(enabled = name.isNotBlank(), onClick = {
                val list = aliases.split(",").map { each -> each.trim() }.filter { each -> each.isNotEmpty() }
                // Other names are sent only when changed, so a change made elsewhere meanwhile is not undone.
                val changed = if (list == node.aliases.orEmpty()) null else list
                scope.launch {
                    if (model.act(saidSaved) { model.connection.saveKnowledgeNode(node.path, name.trim(), summary.trim(), changed, null) }) onClose()
                }
            }) { Text(stringResource(R.string.knowledge_save)) }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).padding(16.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedTextField(value = name, onValueChange = { text -> name = text }, label = { Text(stringResource(R.string.knowledge_name)) }, modifier = Modifier.fillMaxWidth(), singleLine = true)
            OutlinedTextField(
                value = aliases, onValueChange = { text -> aliases = text }, label = { Text(stringResource(R.string.knowledge_other_names)) },
                supportingText = { Text(stringResource(R.string.knowledge_separated_by_commas)) }, modifier = Modifier.fillMaxWidth(), singleLine = true,
            )
            OutlinedTextField(value = summary, onValueChange = { text -> summary = text }, label = { Text(stringResource(R.string.knowledge_summary)) }, modifier = Modifier.fillMaxWidth(), minLines = 6)
        }
    }
}

/** FactEditor adds a fact or changes one: its words, kind, when it was true and which runs read it. */
@Composable
internal fun FactEditor(model: KnowledgePageModel, initial: FactDraft, onClose: () -> Unit) {
    val scope = rememberCoroutineScope()
    var draft by remember { mutableStateOf(initial) }
    val saidSaved = stringResource(R.string.knowledge_saved)
    val isHappenedValid = KnowledgeWords.isHappenedValid(draft.happened)
    TeaNodeScreen(
        title = stringResource(if (draft.number == null) R.string.knowledge_add_fact else R.string.knowledge_edit_fact), onBack = onClose,
        actions = {
            TextButton(enabled = draft.text.isNotBlank() && isHappenedValid, onClick = {
                val audiences = if (draft.audiences == draft.originalAudiences) null else KnowledgeWords.audiences.filter { audience -> audience in draft.audiences }
                scope.launch {
                    if (model.act(saidSaved) { model.connection.saveKnowledgeFact(model.pagePath, draft.number, draft.kind, draft.text.trim(), draft.happened, audiences) }) onClose()
                }
            }) { Text(stringResource(R.string.knowledge_save)) }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).padding(vertical = 8.dp).verticalScroll(rememberScrollState())) {
            OutlinedTextField(
                value = draft.text, onValueChange = { text -> draft = draft.copy(text = text) }, label = { Text(stringResource(R.string.knowledge_what_is_known)) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), minLines = 3,
            )
            ChoiceRow(stringResource(R.string.knowledge_kind), draft.kind, KnowledgeWords.factKinds) { index -> draft = draft.copy(kind = KnowledgeWords.factKinds[index]) }
            OutlinedTextField(
                value = draft.happened, onValueChange = { text -> draft = draft.copy(happened = text) }, label = { Text(stringResource(R.string.knowledge_when_it_was_true)) },
                isError = !isHappenedValid,
                supportingText = { if (!isHappenedValid) Text(stringResource(R.string.knowledge_happened_format), color = MaterialTheme.colorScheme.error) },
                keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = KeyboardType.Number),
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
            )
            TeaNodeSectionHeader(stringResource(R.string.knowledge_which_runs_read_it))
            for (audience in KnowledgeWords.audiences) {
                val isOn = audience in draft.audiences
                Row(
                    Modifier.fillMaxWidth().clickable { draft = draft.copy(audiences = if (isOn) draft.audiences - audience else draft.audiences + audience) }.padding(horizontal = 16.dp, vertical = 8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(audience, Modifier.weight(1f))
                    Switch(checked = isOn, onCheckedChange = { on -> draft = draft.copy(audiences = if (on) draft.audiences + audience else draft.audiences - audience) })
                }
            }
        }
    }
}

/** PagePicker chooses a page: searched, or typed as a path. */
@Composable
internal fun PagePicker(title: String, excluding: String, initial: String, onClose: () -> Unit, onChoose: (String) -> Unit) {
    val connection = com.teanode.app.session.LocalConnection.current
    var typed by remember { mutableStateOf(initial) }
    var found by remember { mutableStateOf<List<KnowledgeNode>>(emptyList()) }
    LaunchedEffect(typed) {
        if (typed.trim().length < 2) {
            found = emptyList()
            return@LaunchedEffect
        }
        delay(250)
        found = runCatching { connection.searchKnowledge(typed.trim(), first = 20) }.getOrNull()?.nodes?.filterNotNull().orEmpty()
    }
    TeaNodeScreen(
        title = title, onBack = onClose,
        actions = {
            TextButton(enabled = typed.isNotBlank() && typed.trim() != excluding && typed.trim() != initial, onClick = { onChoose(typed.trim()) }) { Text(stringResource(R.string.knowledge_choose)) }
        },
    ) { contentPadding ->
        LazyColumn(Modifier.fillMaxSize().padding(contentPadding)) {
            item {
                OutlinedTextField(
                    value = typed, onValueChange = { text -> typed = text }, label = { Text(stringResource(R.string.knowledge_page_path)) },
                    textStyle = MaterialTheme.typography.bodyLarge.copy(fontFamily = FontFamily.Monospace), modifier = Modifier.fillMaxWidth().padding(16.dp), singleLine = true,
                )
            }
            items(found.filter { node -> node.path != excluding }, key = { node -> node.path }) { node ->
                Column(Modifier.fillMaxWidth().clickable { onChoose(node.path) }.padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Text(node.shownName)
                    Text(node.path, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
    }
}

/** LinkEditor links the page to another, with how they are related. */
@Composable
internal fun LinkEditor(model: KnowledgePageModel, onClose: () -> Unit) {
    val scope = rememberCoroutineScope()
    var to by remember { mutableStateOf("") }
    var relation by remember { mutableStateOf("related_to") }
    var note by remember { mutableStateOf("") }
    var isPicking by remember { mutableStateOf(false) }
    val saidLinked = stringResource(R.string.knowledge_linked)
    if (isPicking) {
        PagePicker(stringResource(R.string.knowledge_link_to), model.pagePath, "", onClose = { isPicking = false }) { chosen ->
            to = chosen
            isPicking = false
        }
        return
    }
    TeaNodeScreen(
        title = stringResource(R.string.knowledge_link), onBack = onClose,
        actions = {
            TextButton(enabled = to.isNotEmpty() && to != model.pagePath, onClick = {
                scope.launch { if (model.act(saidLinked) { model.connection.linkKnowledgeNodes(model.pagePath, to, relation, note.trim()) }) onClose() }
            }) { Text(stringResource(R.string.knowledge_link)) }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState())) {
            Row(Modifier.fillMaxWidth().clickable { isPicking = true }.padding(horizontal = 16.dp, vertical = 14.dp)) {
                Text(stringResource(R.string.knowledge_link_to), Modifier.weight(1f))
                Text(to.ifEmpty { stringResource(R.string.knowledge_choose_a_page) }, color = MaterialTheme.colorScheme.primary)
            }
            val relations = KnowledgeWords.relations
            ChoiceRow(stringResource(R.string.knowledge_relation), relation.replace('_', ' '), relations.map { each -> each.replace('_', ' ') }) { index -> relation = relations[index] }
            OutlinedTextField(
                value = note, onValueChange = { text -> note = text }, label = { Text(stringResource(R.string.knowledge_note_optional)) },
                modifier = Modifier.fillMaxWidth().padding(16.dp), singleLine = true,
            )
        }
    }
}

/** ContactPicker chooses the card a person's page is about, from the cards in the person's own address books. It never makes a card. */
@Composable
internal fun ContactPicker(model: KnowledgePageModel, currentContactId: String, onClose: () -> Unit) {
    val scope = rememberCoroutineScope()
    var searchText by remember { mutableStateOf("") }
    var contacts by remember { mutableStateOf<List<KnowledgeContactChoice>>(emptyList()) }
    var isLoaded by remember { mutableStateOf(false) }
    val saidLinked = stringResource(R.string.knowledge_contact_linked)
    val saidUnlinked = stringResource(R.string.knowledge_contact_unlinked)
    LaunchedEffect(searchText) {
        if (searchText.isNotEmpty()) delay(250)
        contacts = runCatching { model.connection.contactChoices(searchText) }.getOrDefault(emptyList())
        isLoaded = true
    }
    fun choose(contactId: String) {
        scope.launch {
            if (model.act(if (contactId.isEmpty()) saidUnlinked else saidLinked) { model.connection.setKnowledgeContact(model.pagePath, contactId) }) onClose()
        }
    }
    TeaNodeScreen(title = stringResource(R.string.knowledge_link_a_contact), onBack = onClose) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            OutlinedTextField(
                value = searchText, onValueChange = { text -> searchText = text }, placeholder = { Text(stringResource(R.string.knowledge_name_address_or_phone)) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
            )
            when {
                !isLoaded -> LoadingState()
                contacts.isEmpty() && searchText.isNotEmpty() -> EmptyState(Icons.Outlined.Check, stringResource(R.string.knowledge_no_results_for, searchText))
                else -> LazyColumn(Modifier.weight(1f)) {
                    if (currentContactId.isNotEmpty()) {
                        item {
                            TextButton(onClick = { choose("") }, modifier = Modifier.padding(horizontal = 8.dp)) {
                                Text(stringResource(R.string.knowledge_unlink_contact), color = MaterialTheme.colorScheme.error)
                            }
                        }
                    }
                    items(contacts, key = { contact -> contact.id }) { contact ->
                        Row(Modifier.fillMaxWidth().clickable { choose(contact.id) }.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                            Column(Modifier.weight(1f)) {
                                Text(contact.shownName)
                                contact.emails?.firstOrNull()?.takeIf { email -> email != contact.shownName }?.let { email ->
                                    Text(email, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                            }
                            if (contact.id == currentContactId) Icon(Icons.Outlined.Check, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                        }
                    }
                }
            }
        }
    }
}
