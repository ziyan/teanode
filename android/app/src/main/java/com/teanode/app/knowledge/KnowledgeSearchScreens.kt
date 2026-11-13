package com.teanode.app.knowledge

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.ChoiceRow
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.knowledge.DocumentExtract
import com.teanode.kit.knowledge.DocumentSearch
import com.teanode.kit.knowledge.KnowledgeRecall
import com.teanode.kit.knowledge.KnowledgeSource
import com.teanode.kit.knowledge.knowledgeSources
import com.teanode.kit.knowledge.readDocument
import com.teanode.kit.knowledge.recallMemory
import com.teanode.kit.knowledge.searchDocuments
import kotlinx.coroutines.launch

/** RecallScreen asks the agent's memory a question and shows the pages and facts it brings up (ios KnowledgeViews.swift, RecallSheet). */
@Composable
fun RecallScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    var question by remember { mutableStateOf("") }
    var recall by remember { mutableStateOf<KnowledgeRecall?>(null) }
    var isAsking by remember { mutableStateOf(false) }
    var problemText by remember { mutableStateOf<String?>(null) }
    fun ask() {
        val asked = question.trim()
        if (asked.isEmpty()) return
        isAsking = true
        scope.launch {
            try {
                recall = connection.recallMemory(asked)
                problemText = null
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                problemText = session.describe(failure)
            }
            isAsking = false
        }
    }
    TeaNodeScreen(title = stringResource(R.string.knowledge_ask_the_memory), onBack = { navigator.back() }) { contentPadding ->
        LazyColumn(Modifier.fillMaxSize().padding(contentPadding)) {
            item {
                Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = question, onValueChange = { text -> question = text; recall = null }, label = { Text(stringResource(R.string.knowledge_what_do_you_want_to_know)) },
                        modifier = Modifier.fillMaxWidth(), minLines = 2,
                    )
                    Button(onClick = { ask() }, enabled = question.isNotBlank() && !isAsking) { Text(stringResource(R.string.knowledge_ask_button)) }
                }
            }
            if (isAsking) item { LoadingState(Modifier.padding(32.dp)) }
            problemText?.let { text -> item { Text(text, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 16.dp)) } }
            val read = recall
            if (read != null) {
                val pages = read.pages.filterNotNull()
                if (pages.isEmpty()) item { Text(stringResource(R.string.knowledge_nothing_answers_that), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                for (page in pages) {
                    item(key = "page-${page.path}") {
                        Text(
                            page.path, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.fillMaxWidth().clickable { navigator.navigate(KnowledgeRoutes.page(page.path)) }.padding(horizontal = 16.dp, vertical = 12.dp),
                        )
                    }
                    items(page.facts.filterNotNull(), key = { fact -> "fact-${page.path}-${fact.number}" }) { fact ->
                        Text("#${fact.number} ${fact.text}", style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
                    }
                }
            }
        }
    }
}

/** DocumentsScreen searches what the agent's sources read, and reads a document a slice at a time (ios KnowledgeViews.swift, DocumentsSheet). */
@Composable
fun DocumentsScreen() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    var words by remember { mutableStateOf("") }
    var sources by remember { mutableStateOf<List<KnowledgeSource>>(emptyList()) }
    var sourceId by remember { mutableStateOf("") }
    var directory by remember { mutableStateOf("") }
    var found by remember { mutableStateOf<DocumentSearch?>(null) }
    var passages by remember { mutableStateOf<List<DocumentSearch.Passage>>(emptyList()) }
    var problemText by remember { mutableStateOf<String?>(null) }
    var isSearching by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { sources = runCatching { connection.knowledgeSources() }.getOrDefault(emptyList()) }
    fun search(isMore: Boolean, chosenSourceId: String = sourceId, chosenDirectory: String = directory) {
        val asked = words.trim()
        if (asked.isEmpty()) return
        isSearching = true
        scope.launch {
            try {
                val read = connection.searchDocuments(asked, chosenSourceId, chosenDirectory.trim(), offset = if (isMore) found?.nextOffset ?: 0 else 0)
                passages = (if (isMore) passages else emptyList()) + read.passages.filterNotNull()
                found = read
                problemText = null
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                problemText = session.describe(failure)
            }
            isSearching = false
        }
    }
    TeaNodeScreen(title = stringResource(R.string.knowledge_search_documents), onBack = { navigator.back() }) { contentPadding ->
        LazyColumn(Modifier.fillMaxSize().padding(contentPadding)) {
            item {
                Column {
                    OutlinedTextField(
                        value = words, onValueChange = { text -> words = text }, label = { Text(stringResource(R.string.knowledge_words)) },
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
                    )
                    val anySource = stringResource(R.string.knowledge_any_source)
                    ChoiceRow(
                        stringResource(R.string.knowledge_source), sources.firstOrNull { source -> source.id == sourceId }?.name ?: anySource,
                        listOf(anySource) + sources.map { source -> source.name },
                    ) { index -> sourceId = if (index == 0) "" else sources[index - 1].id }
                    OutlinedTextField(
                        value = directory, onValueChange = { text -> directory = text }, label = { Text(stringResource(R.string.knowledge_in_a_directory)) },
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
                    )
                    Button(onClick = { search(false) }, enabled = words.isNotBlank() && !isSearching, modifier = Modifier.padding(horizontal = 16.dp)) { Text(stringResource(R.string.knowledge_search_button)) }
                }
            }
            problemText?.let { text -> item { Text(text, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(16.dp)) } }
            val read = found
            if (read != null) {
                if (!read.meaningful) {
                    item { Text(stringResource(R.string.knowledge_found_by_words_alone), modifier = Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
                val definitions = read.definitions.filterNotNull()
                if (definitions.isNotEmpty()) {
                    item(key = "definitions-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_defined_in)) }
                    items(definitions.size, key = { index -> "definition-$index" }) { index ->
                        val definition = definitions[index]
                        Text(
                            "${definition.symbol} · ${definition.kind} · ${definition.title}", style = MaterialTheme.typography.bodyMedium,
                            modifier = Modifier.fillMaxWidth().clickable { navigator.navigate(KnowledgeRoutes.document(definition.documentId)) }.padding(horizontal = 16.dp, vertical = 10.dp),
                        )
                    }
                }
                val directories = read.directories.filterNotNull()
                if (directories.isNotEmpty()) {
                    item(key = "directories-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_where_these_are)) }
                    items(directories.size, key = { index -> "directory-$index" }) { index ->
                        val place = directories[index]
                        Text(
                            "${place.directory} · ${place.passageCount}", style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace,
                            modifier = Modifier.fillMaxWidth().clickable {
                                directory = place.directory
                                sourceId = place.sourceId
                                search(false, place.sourceId, place.directory)
                            }.padding(horizontal = 16.dp, vertical = 10.dp),
                        )
                    }
                }
                val order = passages.map { passage -> passage.documentId }.distinct()
                for (documentId in order) {
                    val group = passages.filter { passage -> passage.documentId == documentId }
                    val first = group.first()
                    item(key = "document-header-$documentId") {
                        TeaNodeSectionHeader(listOf(first.title.ifEmpty { first.externalId }, first.author, first.source).filter { part -> part.isNotEmpty() }.joinToString(" · "))
                    }
                    items(group.size, key = { index -> "passage-$documentId-$index" }) { index ->
                        Text(group[index].text, style = MaterialTheme.typography.bodyMedium, maxLines = 8, modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp))
                    }
                    item(key = "document-read-$documentId") {
                        TextButton(onClick = { navigator.navigate(KnowledgeRoutes.document(documentId)) }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.knowledge_read)) }
                    }
                }
                if (read.nextOffset > 0) {
                    item { TextButton(onClick = { search(true) }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.knowledge_show_more)) } }
                }
            }
        }
    }
}

/** DocumentReaderScreen reads a document a slice at a time (ios KnowledgeViews.swift, DocumentReader). */
@Composable
fun DocumentReaderScreen(documentId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    var extract by remember { mutableStateOf<DocumentExtract?>(null) }
    var text by remember { mutableStateOf("") }
    var isGone by remember { mutableStateOf(false) }
    suspend fun read(from: Int) {
        val slice = runCatching { connection.readDocument(documentId, from) }.getOrNull()
        if (slice == null) {
            isGone = extract == null
            return
        }
        text += slice.text
        extract = slice
    }
    LaunchedEffect(documentId) { read(0) }
    TeaNodeScreen(title = extract?.title.orEmpty(), onBack = { navigator.back() }) { contentPadding ->
        val shown = extract
        when {
            shown != null -> Column(Modifier.fillMaxSize().padding(contentPadding).padding(16.dp).verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(listOf(shown.author, shown.source).filter { part -> part.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                androidx.compose.foundation.text.selection.SelectionContainer { Text(text) }
                if (shown.next > 0) {
                    OutlinedButton(onClick = { scope.launch { read(shown.next) } }) { Text(stringResource(R.string.knowledge_read_on, shown.total - shown.next)) }
                }
            }
            isGone -> EmptyState(Icons.Outlined.Description, stringResource(R.string.knowledge_document_gone), Modifier.padding(contentPadding))
            else -> LoadingState(Modifier.padding(contentPadding))
        }
    }
}
