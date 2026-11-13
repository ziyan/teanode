package com.teanode.app.knowledge

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.knowledge.KnowledgeNode
import com.teanode.kit.knowledge.KnowledgeRow
import com.teanode.kit.knowledge.KnowledgeSearch
import com.teanode.kit.knowledge.SharedKnowledgeHit
import com.teanode.kit.knowledge.SharedKnowledgeOwnerGroup
import com.teanode.kit.knowledge.knowledgeChildren
import com.teanode.kit.knowledge.knowledgePage
import com.teanode.kit.knowledge.searchKnowledge
import com.teanode.kit.knowledge.searchSharedKnowledge
import com.teanode.kit.knowledge.sharedKnowledgeRoots
import kotlinx.coroutines.delay

/** How many pages a folder asks for at a time. */
private const val FOLDER_PAGE_SIZE = 50

/**
 * KnowledgeFolderScreen is a folder's pages, fifty at a time, or what a search found when one is typed (ios
 * KnowledgeViews.swift, KnowledgeFolder). At the top level it is the Knowledge part's first screen, with what other
 * people shared under the person's own folders and the menu that asks the memory or searches the documents.
 */
@Composable
fun KnowledgeFolderScreen(folderPath: String, initialSearch: String = "") {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val session = LocalAppSession.current
    val isTop = folderPath.isEmpty()
    var rows by remember(folderPath) { mutableStateOf<List<KnowledgeRow>>(emptyList()) }
    var total by remember(folderPath) { mutableStateOf(0) }
    var title by remember(folderPath) { mutableStateOf("") }
    var isLoaded by remember(folderPath) { mutableStateOf(false) }
    var problem by remember(folderPath) { mutableStateOf<Throwable?>(null) }
    var searchText by remember(folderPath) { mutableStateOf(initialSearch) }
    var found by remember(folderPath) { mutableStateOf<KnowledgeSearch?>(null) }
    var foundNodes by remember(folderPath) { mutableStateOf<List<KnowledgeNode>>(emptyList()) }
    var foundFacts by remember(folderPath) { mutableStateOf<List<KnowledgeSearch.FoundFact>>(emptyList()) }
    var sharedGroups by remember(folderPath) { mutableStateOf<List<SharedKnowledgeOwnerGroup>>(emptyList()) }
    var sharedHits by remember(folderPath) { mutableStateOf<List<SharedKnowledgeHit>>(emptyList()) }
    var hasMoreSharedHits by remember(folderPath) { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var reloadCount by remember(folderPath) { mutableStateOf(0) }
    var searchCount by remember(folderPath) { mutableStateOf(0) }

    /** A failure is the whole screen only while nothing is shown. With rows on screen they stay, and the failure is told in a message. */
    fun report(failure: Throwable) {
        if (rows.isEmpty() && foundNodes.isEmpty() && foundFacts.isEmpty()) problem = failure else snackbar.showError(failure)
    }

    suspend fun load(isMore: Boolean) {
        try {
            val page = connection.knowledgeChildren(folderPath, FOLDER_PAGE_SIZE, if (isMore) rows.size else 0)
            val fresh = page.rows.filterNotNull()
            rows = KnowledgeRow.ordered(if (isMore) rows + fresh else fresh)
            total = page.total
            if (!isTop && title.isEmpty()) {
                title = connection.knowledgePage(folderPath)?.node?.shownName ?: folderPath.split("/").lastOrNull().orEmpty()
            }
            isLoaded = true
            problem = null
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            report(failure)
        }
    }

    suspend fun search(words: String, isMore: Boolean) {
        try {
            val read = connection.searchKnowledge(words, offset = if (isMore) found?.nextOffset ?: 0 else 0)
            if (words != searchText.trim()) return
            foundNodes = (if (isMore) foundNodes else emptyList()) + read.nodes.filterNotNull()
            foundFacts = (if (isMore) foundFacts else emptyList()) + read.facts.filterNotNull()
            found = read
            // What is shared is searched with the first page of the person's own, at the top level where it is listed;
            // not being able to search it leaves the person's own results as they are.
            if (!isMore && isTop) {
                val sharedSearch = runCatching { connection.searchSharedKnowledge(words) }.getOrNull()
                sharedHits = sharedSearch?.hits.orEmpty()
                hasMoreSharedHits = sharedSearch?.hasMoreHits ?: false
            }
            problem = null
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            report(failure)
        }
    }

    LaunchedEffect(folderPath, reloadCount) {
        load(isMore = false)
        // What other people shared, at the top level. Not being able to read it leaves the person's own pages as they are.
        if (isTop) sharedGroups = SharedKnowledgeOwnerGroup.grouped(runCatching { connection.sharedKnowledgeRoots() }.getOrDefault(emptyList()))
    }
    LaunchedEffect(searchText, searchCount) {
        val words = searchText.trim()
        if (words.isEmpty()) {
            found = null
            sharedHits = emptyList()
            hasMoreSharedHits = false
            return@LaunchedEffect
        }
        delay(250)
        search(words, isMore = false)
    }

    TeaNodeScreen(
        title = if (isTop) stringResource(R.string.knowledge_title) else title,
        onBack = if (isTop) null else ({ navigator.back() }),
        actions = {
            if (isTop) {
                androidx.compose.foundation.layout.Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.knowledge_ask)) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_ask_the_memory)) }, onClick = { isMenuOpen = false; navigator.navigate(KnowledgeRoutes.RECALL) })
                        DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_search_documents)) }, onClick = { isMenuOpen = false; navigator.navigate(KnowledgeRoutes.DOCUMENTS) })
                    }
                }
            }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            OutlinedTextField(
                value = searchText, onValueChange = { text -> searchText = text }, placeholder = { Text(stringResource(R.string.knowledge_search_prompt)) },
                leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
            )
            val failure = problem
            when {
                failure != null -> ErrorState(session.describeLoadProblem(failure), onRetry = { problem = null; reloadCount += 1 })
                !isLoaded -> LoadingState()
                else -> LazyColumn(Modifier.weight(1f)) {
                    if (searchText.isNotEmpty()) {
                        searchResults(found, foundNodes, foundFacts, sharedHits, hasMoreSharedHits, onOpenPage = { path -> navigator.navigate(KnowledgeRoutes.page(path)) },
                            onOpenShared = { hit -> navigator.navigate(KnowledgeRoutes.shared(hit.ownerUserId, hit.path)) }, onMore = { searchCount += 1 })
                    } else {
                        if (rows.isEmpty()) {
                            item { EmptyState(Icons.Outlined.Folder, stringResource(R.string.knowledge_nothing_filed_here_yet), Modifier.padding(top = 48.dp)) }
                        }
                        items(rows.mapNotNull { row -> row.node?.let { node -> row to node } }, key = { pair -> pair.second.path }) { (row, node) ->
                            KnowledgeRowView(row, node, connection.account.username, onClick = {
                                navigator.navigate(if (node.isFolder) KnowledgeRoutes.folder(node.path) else KnowledgeRoutes.page(node.path))
                            })
                            // The next fifty are read as the last row comes into view.
                            if (node.path == rows.lastOrNull()?.node?.path && rows.size < total) {
                                LaunchedEffect(rows.size) { load(isMore = true) }
                            }
                        }
                        // What other people shared, at the top level only: a place of its own beside the folders, not a folder among them.
                        if (isTop) sharedSection(sharedGroups) { root -> navigator.navigate(KnowledgeRoutes.shared(root.ownerUserId, root.path)) }
                    }
                }
            }
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.searchResults(
    found: KnowledgeSearch?,
    nodes: List<KnowledgeNode>,
    facts: List<KnowledgeSearch.FoundFact>,
    sharedHits: List<SharedKnowledgeHit>,
    hasMoreSharedHits: Boolean,
    onOpenPage: (String) -> Unit,
    onOpenShared: (SharedKnowledgeHit) -> Unit,
    onMore: () -> Unit,
) {
    val groups = nodes.groupBy { node -> node.path.split("/").firstOrNull().orEmpty() }
    for (root in groups.keys.sorted()) {
        item(key = "root-$root") { TeaNodeSectionHeader(root) }
        items(groups.getValue(root), key = { node -> "node-${node.path}" }) { node ->
            Column(Modifier.fillMaxWidth().clickable { onOpenPage(node.path) }.padding(horizontal = 16.dp, vertical = 8.dp)) {
                Text(node.shownName, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(node.path, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            }
        }
    }
    if (facts.isNotEmpty()) {
        item(key = "facts-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_facts)) }
        items(facts.size, key = { index -> "fact-$index" }) { index ->
            val foundFact = facts[index]
            Column(Modifier.fillMaxWidth().clickable { onOpenPage(foundFact.path) }.padding(horizontal = 16.dp, vertical = 8.dp)) {
                Text(foundFact.name.ifEmpty { foundFact.path }, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text("#${foundFact.fact?.number ?: 0} ${foundFact.fact?.text.orEmpty()}", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
        }
    }
    // What other people shared, found by the same words: pages and facts, each saying whose it is.
    if (sharedHits.isNotEmpty()) {
        item(key = "shared-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_shared_with_you)) }
        items(sharedHits, key = { hit -> "hit-${hit.id}" }) { hit ->
            Column(Modifier.fillMaxWidth().clickable { onOpenShared(hit) }.padding(horizontal = 16.dp, vertical = 8.dp)) {
                Text(if (hit.isFact) "${hit.shownName} #${hit.factNumber}" else hit.shownName, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(stringResource(R.string.knowledge_shared_by, hit.shownOwner), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                if (hit.shownText.isNotEmpty()) {
                    Text(hit.shownText, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
            }
        }
        if (hasMoreSharedHits) {
            item(key = "shared-more") {
                Text(stringResource(R.string.knowledge_there_may_be_more), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    if (found != null) {
        if (found.nextOffset > 0) {
            item(key = "show-more") { TextButton(onClick = onMore, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.knowledge_show_more)) } }
        } else if (nodes.isEmpty() && facts.isEmpty() && sharedHits.isEmpty()) {
            item(key = "nothing") { Text(stringResource(R.string.knowledge_nothing_matches), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
    }
}

/** A page in a folder: its name, a pin, what it is about, and how many pages it holds. */
@Composable
fun KnowledgeRowView(row: KnowledgeRow, node: KnowledgeNode, selfName: String?, onClick: () -> Unit) {
    val pinnedLabel = stringResource(R.string.knowledge_always_in_prompt)
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(KnowledgeSymbols.icon(node.kind), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(18.dp))
                Text(
                    if (node.isSelf) selfName ?: stringResource(R.string.knowledge_you) else node.shownName,
                    style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis,
                )
                if (node.pinned == true) {
                    Icon(Icons.Filled.PushPin, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(14.dp).semantics { contentDescription = pinnedLabel })
                }
            }
            val line = row.line
            if (line.isNotEmpty()) {
                Text(line, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        if (row.children > 0) {
            Text(row.children.toString(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** The "Shared with you" part of the Knowledge list at the top level: a row for each page another person shared, under their picture and name. */
private fun androidx.compose.foundation.lazy.LazyListScope.sharedSection(groups: List<SharedKnowledgeOwnerGroup>, onOpen: (com.teanode.kit.knowledge.SharedKnowledgeRoot) -> Unit) {
    if (groups.isEmpty()) return
    item(key = "shared-section-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_shared_with_you)) }
    for (group in groups) {
        item(key = "owner-${group.ownerUserId}") {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                AvatarView(group.ownerName, group.ownerAvatarUrl, group.ownerAvatarColorIndex, size = 24.dp)
                Text(group.ownerName, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        items(group.roots, key = { root -> "root-${root.id}" }) { root ->
            Column(Modifier.fillMaxWidth().clickable { onOpen(root) }.padding(horizontal = 16.dp, vertical = 8.dp)) {
                Text(root.shownName, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                if (root.opening.isNotEmpty()) {
                    Text(root.opening, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
            }
        }
    }
}
