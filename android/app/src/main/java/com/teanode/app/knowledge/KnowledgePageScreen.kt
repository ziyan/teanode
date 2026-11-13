package com.teanode.app.knowledge

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AddCircleOutline
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DriveFileMove
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PersonAdd
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.DateWords
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.calendar.ServerTime
import com.teanode.kit.knowledge.KnowledgeAttachment
import com.teanode.kit.knowledge.KnowledgeFact
import com.teanode.kit.knowledge.KnowledgeNode
import com.teanode.kit.knowledge.KnowledgePage
import com.teanode.kit.knowledge.KnowledgeRecordKinds
import com.teanode.kit.knowledge.KnowledgeRecordLink
import com.teanode.kit.knowledge.deleteKnowledgeFact
import com.teanode.kit.knowledge.deleteKnowledgeNode
import com.teanode.kit.knowledge.confirmKnowledgeRecord
import com.teanode.kit.knowledge.mergeKnowledgeNodes
import com.teanode.kit.knowledge.moveKnowledgeFact
import com.teanode.kit.knowledge.moveKnowledgeNode
import com.teanode.kit.knowledge.saveKnowledgeNode
import com.teanode.kit.knowledge.setKnowledgeContact
import com.teanode.kit.knowledge.unlinkKnowledgeNodes
import com.teanode.kit.knowledge.unlinkKnowledgeRecord
import kotlinx.coroutines.launch

/** The screens a page opens over itself: its editors, the choosers and the sharing. Each fills the screen and the way back closes it. */
internal sealed class PageSheet {
    data object Edit : PageSheet()
    data class Fact(val draft: FactDraft) : PageSheet()
    data class Choose(val choice: PageChoice) : PageSheet()
    data object Link : PageSheet()
    data object LinkContact : PageSheet()
    data object Share : PageSheet()
}

/** What a chosen page is for. */
internal sealed class PageChoice(val initial: String) {
    data object Move : PageChoice("")
    data object Merge : PageChoice("")
    data class MoveFact(val number: Int) : PageChoice("")
}

/**
 * KnowledgePageScreen is a page whole: its summary, overview, reflections, facts, connections, the pages under it and
 * its history, and every change the dashboard makes to them (ios KnowledgeViews.swift, KnowledgePageView).
 */
@Composable
fun KnowledgePageScreen(initialPath: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    val model = remember(initialPath) { KnowledgePageModel(connection, initialPath, snackbar) }
    val saidMoved = stringResource(R.string.knowledge_moved)
    var sheet by remember { mutableStateOf<PageSheet?>(null) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isConfirmingForget by remember { mutableStateOf(false) }
    var mergingInto by remember { mutableStateOf<String?>(null) }
    var forgettingFact by remember { mutableStateOf<KnowledgeFact?>(null) }
    var shownFactCount by remember { mutableStateOf(50) }
    LaunchedEffect(model) { model.load() }
    BackHandler(enabled = sheet != null) { sheet = null }

    val node = model.page?.node
    val openSheet = sheet
    if (openSheet != null && node != null) {
        PageSheets(model, node, openSheet, onClose = { sheet = null }, onChosen = { choice, chosen ->
            sheet = null
            scope.launch {
                val pagePath = model.pagePath
                when (choice) {
                    PageChoice.Move -> {
                        var newPath: String? = null
                        val isMoved = model.act(saidMoved) { newPath = model.connection.moveKnowledgeNode(pagePath, chosen) }
                        if (isMoved && newPath != null && newPath != pagePath) {
                            model.moved(newPath!!)
                            model.load()
                        }
                    }
                    PageChoice.Merge -> mergingInto = chosen
                    is PageChoice.MoveFact -> model.act(saidMoved) { model.connection.moveKnowledgeFact(pagePath, choice.number, chosen) }
                }
            }
        })
        return
    }

    val titleText = node?.let { found -> if (found.isSelf) connection.account.username else found.shownName }.orEmpty()
    val saidPinned = stringResource(R.string.knowledge_pinned)
    val saidUnpinned = stringResource(R.string.knowledge_unpinned)
    val saidForgotten = stringResource(R.string.knowledge_forgotten)
    val saidMerged = stringResource(R.string.knowledge_merged)
    val saidLinkConfirmed = stringResource(R.string.knowledge_link_confirmed)
    val saidLinkRemoved = stringResource(R.string.knowledge_link_removed)
    val saidUnlinked = stringResource(R.string.knowledge_unlinked)
    TeaNodeScreen(
        title = titleText,
        onBack = { navigator.back() },
        actions = {
            if (node != null) {
                androidx.compose.foundation.layout.Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.knowledge_page_actions)) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_edit)) }, onClick = { isMenuOpen = false; sheet = PageSheet.Edit })
                        DropdownMenuItem(text = { Text(stringResource(if (node.pinned == true) R.string.knowledge_unpin else R.string.knowledge_pin)) }, onClick = {
                            isMenuOpen = false
                            val pagePath = model.pagePath
                            scope.launch {
                                model.act(if (node.pinned == true) saidUnpinned else saidPinned) {
                                    model.connection.saveKnowledgeNode(pagePath, node.name, node.summary, null, !(node.pinned ?: false))
                                }
                            }
                        })
                        if (node.parentPath.isNotEmpty()) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_move)) }, onClick = { isMenuOpen = false; sheet = PageSheet.Choose(PageChoice.Move) })
                            DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_merge_into)) }, onClick = { isMenuOpen = false; sheet = PageSheet.Choose(PageChoice.Merge) })
                        }
                        DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_share_menu)) }, onClick = { isMenuOpen = false; sheet = PageSheet.Share })
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.knowledge_forget_page), color = MaterialTheme.colorScheme.error) },
                            onClick = { isMenuOpen = false; isConfirmingForget = true },
                        )
                    }
                }
            }
        },
    ) { contentPadding ->
        val page = model.page
        val failure = model.problem
        when {
            model.isMissing -> EmptyState(Icons.Outlined.Link, stringResource(R.string.knowledge_there_is_no_page_here), Modifier.padding(contentPadding))
            page == null && failure != null -> ErrorState(session.describeLoadProblem(failure), onRetry = { scope.launch { model.load() } }, modifier = Modifier.padding(contentPadding))
            page == null || node == null -> LoadingState(Modifier.padding(contentPadding))
            else -> LazyColumn(Modifier.fillMaxSize().padding(contentPadding)) {
                header(page, node, onLinkContact = { sheet = PageSheet.LinkContact }, onOpenContact = { id -> openRecord(navigator, "/contacts?contact=$id") })
                linkedRecords(model, onOpen = { link -> openRecord(navigator, link.dashboardPath) }, onConfirm = { link ->
                    scope.launch { model.act(saidLinkConfirmed) { model.connection.confirmKnowledgeRecord(model.pagePath, link) } }
                }, onRemove = { link ->
                    scope.launch { model.act(saidLinkRemoved) { model.connection.unlinkKnowledgeRecord(model.pagePath, link) } }
                }, onShowMore = { scope.launch { model.loadMoreRecordLinks() } })
                reflections(page, onOpenCited = { cited -> navigator.navigate(KnowledgeRoutes.page(cited)) })
                facts(
                    page, shownFactCount, onShowMore = { shownFactCount += 50 }, onEdit = { fact -> sheet = PageSheet.Fact(FactDraft.of(fact)) },
                    onMove = { fact -> sheet = PageSheet.Choose(PageChoice.MoveFact(fact.number)) }, onForget = { fact -> forgettingFact = fact },
                    onAdd = { sheet = PageSheet.Fact(FactDraft()) }, onOpenRecord = { path -> openRecord(navigator, path) },
                )
                connections(model, onOpenPage = { path -> navigator.navigate(KnowledgeRoutes.page(path)) }, onUnlink = { to, relation ->
                    scope.launch { model.act(saidUnlinked) { model.connection.unlinkKnowledgeNodes(model.pagePath, to, relation) } }
                }, onLink = { sheet = PageSheet.Link })
                val children = page.children.filterNotNull()
                if (children.isNotEmpty()) {
                    item(key = "children-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_filed_under_this)) }
                    items(children, key = { child -> "child-${child.path}" }) { child ->
                        TeaNodeRow(
                            headline = child.shownName, supporting = child.summary.ifEmpty { null },
                            onClick = { navigator.navigate(if (child.isFolder) KnowledgeRoutes.folder(child.path) else KnowledgeRoutes.page(child.path)) },
                        )
                    }
                }
                historySection(model)
            }
        }
    }
    forgettingFact?.let { fact ->
        ConfirmationDialog(
            title = stringResource(R.string.knowledge_forget_fact_question),
            text = stringResource(R.string.knowledge_forget_fact_message, fact.text),
            confirmLabel = stringResource(R.string.knowledge_forget),
            isDestructive = true,
            onConfirm = {
                forgettingFact = null
                scope.launch { model.act(saidForgotten) { model.connection.deleteKnowledgeFact(model.pagePath, fact.number) } }
            },
            onDismiss = { forgettingFact = null },
        )
    }
    if (isConfirmingForget) {
        ConfirmationDialog(
            title = stringResource(R.string.knowledge_forget_page_question),
            text = stringResource(R.string.knowledge_forget_page_message, model.pagePath, model.page?.presentFacts?.size ?: 0, model.page?.children?.size ?: 0),
            confirmLabel = stringResource(R.string.knowledge_forget_page),
            isDestructive = true,
            onConfirm = {
                isConfirmingForget = false
                scope.launch {
                    val pagePath = model.pagePath
                    if (model.act(saidForgotten) { model.connection.deleteKnowledgeNode(pagePath) }) navigator.back()
                }
            },
            onDismiss = { isConfirmingForget = false },
        )
    }
    mergingInto?.let { into ->
        ConfirmationDialog(
            title = stringResource(R.string.knowledge_merge_page_question),
            text = stringResource(R.string.knowledge_merge_page_message, model.pagePath, into, model.page?.presentFacts?.size ?: 0, model.page?.children?.size ?: 0),
            confirmLabel = stringResource(R.string.knowledge_merge),
            isDestructive = true,
            onConfirm = {
                mergingInto = null
                scope.launch {
                    val pagePath = model.pagePath
                    var left: String? = null
                    if (model.act(saidMerged) { left = model.connection.mergeKnowledgeNodes(pagePath, into) }) {
                        model.moved(left ?: into)
                        model.load()
                    }
                }
            },
            onDismiss = { mergingInto = null },
        )
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.header(page: KnowledgePage, node: KnowledgeNode, onLinkContact: () -> Unit, onOpenContact: (String) -> Unit) {
    item(key = "header") {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                Text(node.path, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Chip(node.kind)
                if (node.pinned == true) Chip(stringResource(R.string.knowledge_always_in_prompt))
            }
            node.aliases?.takeIf { names -> names.isNotEmpty() }?.let { names ->
                Text(stringResource(R.string.knowledge_also_called, names.joinToString(", ")), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            page.contact?.let { contact ->
                // The linked card: it opens in Contacts.
                TextButton(onClick = { onOpenContact(contact.id) }) {
                    Text(listOf(contact.name, contact.emails.firstOrNull() ?: contact.organization).filter { part -> part.isNotEmpty() }.joinToString(" · "))
                }
            }
            if (node.kind == "person" && !node.isSelf) {
                TextButton(onClick = onLinkContact) {
                    Icon(Icons.Outlined.PersonAdd, contentDescription = null, modifier = Modifier.padding(end = 8.dp))
                    Text(stringResource(if (page.contact == null) R.string.knowledge_link_contact else R.string.knowledge_change_contact))
                }
            }
            if (node.summary.isEmpty()) {
                Text(stringResource(R.string.knowledge_no_summary_yet), color = MaterialTheme.colorScheme.onSurfaceVariant)
            } else {
                MarkdownText(node.summary)
            }
        }
    }
    val overview = node.overview
    if (!overview.isNullOrEmpty()) {
        item(key = "overview") {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(stringResource(R.string.knowledge_overview), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                MarkdownText(overview)
                ServerTime.instant(node.overviewWrittenAt)?.let { written ->
                    Text(
                        stringResource(R.string.knowledge_written, DateWords.abbreviatedDate(written.atZone(java.time.ZoneId.systemDefault()).toLocalDate())),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

/** The person's own records the page is linked to, by kind. A guess is marked and is confirmed or removed in its menu. */
private fun androidx.compose.foundation.lazy.LazyListScope.linkedRecords(
    model: KnowledgePageModel,
    onOpen: (KnowledgeRecordLink) -> Unit,
    onConfirm: (KnowledgeRecordLink) -> Unit,
    onRemove: (KnowledgeRecordLink) -> Unit,
    onShowMore: () -> Unit,
) {
    val links = model.recordLinks
    if (links.isEmpty()) return
    val groups = KnowledgeRecordKinds.grouped(links)
    for (group in groups) {
        item(key = "records-${group.recordKind}") {
            Row(Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(KnowledgeSymbols.recordIcon(group.recordKind), contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                Text(KnowledgeRecordWords.kind(group.recordKind), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            }
        }
        items(group.links, key = { link -> "link-${link.id}" }) { link -> LinkedRecordRow(link, onOpen, onConfirm, onRemove) }
        if (group.recordKind == groups.last().recordKind && links.size < model.recordLinkCount) {
            item(key = "records-more") {
                TextButton(onClick = onShowMore, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.knowledge_show_n_more, model.recordLinkCount - links.size)) }
            }
        }
    }
}

/** A record a page is linked to: its title, what the page is to it, its date, and a guess marked as one. */
@Composable
fun LinkedRecordRow(link: KnowledgeRecordLink, onOpen: (KnowledgeRecordLink) -> Unit, onConfirm: (KnowledgeRecordLink) -> Unit, onRemove: (KnowledgeRecordLink) -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth().clickable(enabled = link.canOpen) { onOpen(link) }.padding(horizontal = 16.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            if (link.isUnavailable) {
                Text(stringResource(R.string.knowledge_no_longer_available), color = MaterialTheme.colorScheme.onSurfaceVariant)
            } else {
                Text(link.recordTitle.ifEmpty { KnowledgeRecordWords.kind(link.recordKind) })
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), itemVerticalAlignment = Alignment.CenterVertically) {
                Text(KnowledgeRecordWords.relation(link.recordRelation), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (link.coveredPassageCount > 1) {
                    Text(stringResource(R.string.knowledge_passages, link.coveredPassageCount), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                link.date?.let { date ->
                    Text(DateWords.abbreviatedDate(date.atZone(java.time.ZoneId.systemDefault()).toLocalDate()), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (link.isProposed) Chip(stringResource(R.string.knowledge_a_guess))
            }
        }
        androidx.compose.foundation.layout.Box {
            IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.knowledge_more_actions)) }
            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                if (link.isProposed) DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_confirm_link)) }, onClick = { isMenuOpen = false; onConfirm(link) })
                DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_remove_link), color = MaterialTheme.colorScheme.error) }, onClick = { isMenuOpen = false; onRemove(link) })
            }
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.reflections(page: KnowledgePage, onOpenCited: (String) -> Unit) {
    val reflections = page.presentFacts.filter { fact -> fact.kind == "reflection" }
    if (reflections.isEmpty()) return
    item(key = "reflections-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_reflections)) }
    items(reflections, key = { fact -> "reflection-${fact.id}" }) { fact ->
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            fact.reflectionKind?.let { kind -> Chip(kind) }
            Text(fact.text)
            for (citation in fact.citations) {
                Text(
                    citation, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.clickable { onOpenCited(citation.split("#").first()) },
                )
            }
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.facts(
    page: KnowledgePage,
    shownFactCount: Int,
    onShowMore: () -> Unit,
    onEdit: (KnowledgeFact) -> Unit,
    onMove: (KnowledgeFact) -> Unit,
    onForget: (KnowledgeFact) -> Unit,
    onAdd: () -> Unit,
    onOpenRecord: (String) -> Unit,
) {
    val facts = page.presentFacts.filter { fact -> fact.kind != "reflection" }
    item(key = "facts-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_facts)) }
    for (fact in facts.take(shownFactCount)) {
        item(key = "fact-${fact.id}") { FactRow(fact, page.attachments(fact), onEdit = { onEdit(fact) }, onMove = { onMove(fact) }, onForget = { onForget(fact) }) }
        // Where the evidence is one of the person's own records, the record opens, at the passage for a recording.
        items(page.records(fact), key = { record -> "evidence-${fact.id}-${record.evidenceIndex}" }) { record ->
            TextButton(onClick = { onOpenRecord(record.dashboardPath) }, enabled = record.canOpen, modifier = Modifier.padding(horizontal = 8.dp)) {
                Icon(KnowledgeSymbols.recordIcon(record.recordKind), contentDescription = null, modifier = Modifier.padding(end = 8.dp).size(16.dp))
                Text(
                    if (record.isUnavailable) stringResource(R.string.knowledge_kind_no_longer_available, KnowledgeRecordWords.kind(record.recordKind))
                    else record.recordTitle.ifEmpty { KnowledgeRecordWords.kind(record.recordKind) },
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
    }
    if (facts.size > shownFactCount) {
        item(key = "facts-more") { TextButton(onClick = onShowMore, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.knowledge_show_n_more, facts.size - shownFactCount)) } }
    }
    items(page.folded.filterNotNull(), key = { folded -> "folded-${folded.fact?.id}-${folded.into}" }) { folded ->
        Text(
            stringResource(R.string.knowledge_folded_into, folded.fact?.number ?: 0, folded.fact?.text.orEmpty(), folded.into),
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
    item(key = "facts-add") {
        TextButton(onClick = onAdd, modifier = Modifier.padding(horizontal = 8.dp)) {
            Icon(Icons.Outlined.AddCircleOutline, contentDescription = null, modifier = Modifier.padding(end = 8.dp))
            Text(stringResource(R.string.knowledge_add_a_fact))
        }
    }
    if (facts.isEmpty()) {
        item(key = "facts-empty") { Text(stringResource(R.string.knowledge_nothing_known_yet), modifier = Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/** A fact: its number and words, its kind, whether it was worked out, what it came from and when, and its files. */
@Composable
fun FactRow(fact: KnowledgeFact, attachments: List<KnowledgeAttachment>, onEdit: () -> Unit, onMove: () -> Unit, onForget: () -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth().clickable(onClick = onEdit).padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.Top) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("#${fact.number} ${fact.text}")
            FlowRow(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                if (fact.kind != "fact") Chip(fact.kind)
                if (fact.inferred) Chip(stringResource(R.string.knowledge_worked_out_not_told), isAlert = true)
            }
            fact.evidence.firstOrNull()?.let { first ->
                Text(
                    if (first.quote.isNotEmpty()) stringResource(R.string.knowledge_quote_source, first.quote, KnowledgeWordsShown.source(first.kind)) else KnowledgeWordsShown.source(first.kind),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3,
                )
            }
            ServerTime.instant(fact.happenedAt)?.let { happened ->
                Text(DateWords.abbreviatedDate(happened.atZone(java.time.ZoneOffset.UTC).toLocalDate()), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            for (file in attachments) {
                when {
                    file.path.isEmpty() -> Text(stringResource(R.string.knowledge_file_missing, file.name), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    file.contentType.startsWith("image/") -> RemotePicture(file.path, file.name)
                    else -> Text(file.name, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.primary)
                }
            }
        }
        androidx.compose.foundation.layout.Box {
            IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.knowledge_more_actions)) }
            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_move)) }, leadingIcon = { Icon(Icons.Outlined.DriveFileMove, contentDescription = null) }, onClick = { isMenuOpen = false; onMove() })
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.knowledge_forget), color = MaterialTheme.colorScheme.error) },
                    leadingIcon = { Icon(Icons.Outlined.Delete, contentDescription = null) }, onClick = { isMenuOpen = false; onForget() },
                )
            }
        }
    }
}

/** A picture the server keeps, drawn when it has loaded. */
@Composable
fun RemotePicture(path: String, name: String, modifier: Modifier = Modifier) {
    val picture = rememberServerPicture(path)
    if (picture != null) {
        androidx.compose.foundation.Image(
            picture, contentDescription = name, modifier = modifier.fillMaxWidth().heightIn(max = 160.dp),
            contentScale = androidx.compose.ui.layout.ContentScale.Fit, alignment = Alignment.CenterStart,
        )
    } else {
        Text(name, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.connections(
    model: KnowledgePageModel,
    onOpenPage: (String) -> Unit,
    onUnlink: (String, String) -> Unit,
    onLink: () -> Unit,
) {
    item(key = "connections-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_connections)) }
    val neighbours = model.neighbours
    if (neighbours != null) {
        neighbours.parent?.let { parent ->
            item(key = "connection-parent") {
                Row(Modifier.fillMaxWidth().clickable { onOpenPage(parent.path) }.padding(horizontal = 16.dp, vertical = 12.dp)) {
                    Text(stringResource(R.string.knowledge_filed_under), modifier = Modifier.weight(1f))
                    Text(parent.shownName, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        val present = neighbours.neighbours.filterNotNull().filter { neighbour -> neighbour.node != null }
        items(present.size, key = { index -> "connection-$index" }) { index ->
            val neighbour = present[index]
            val node = neighbour.node ?: return@items
            var isMenuOpen by remember { mutableStateOf(false) }
            Row(Modifier.fillMaxWidth().clickable { onOpenPage(node.path) }.padding(start = 16.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                        val relation = neighbour.relation.replace('_', ' ')
                        Text(if (neighbour.outward) "$relation →" else "← $relation", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        if (neighbour.status == "proposed") Chip(stringResource(R.string.knowledge_a_guess))
                    }
                    Text(node.shownName)
                    if (neighbour.note.isNotEmpty()) Text(neighbour.note, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (neighbour.outward) {
                    androidx.compose.foundation.layout.Box {
                        IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.knowledge_more_actions)) }
                        DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.knowledge_unlink), color = MaterialTheme.colorScheme.error) }, onClick = { isMenuOpen = false; onUnlink(node.path, neighbour.relation) })
                        }
                    }
                }
            }
        }
        val leftOut = neighbours.links - present.size
        if (leftOut > 0) {
            item(key = "connections-left-out") {
                Text(stringResource(R.string.knowledge_n_more_links_not_shown, leftOut), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    item(key = "connections-link") {
        TextButton(onClick = onLink, modifier = Modifier.padding(horizontal = 8.dp)) {
            Icon(Icons.Outlined.Link, contentDescription = null, modifier = Modifier.padding(end = 8.dp))
            Text(stringResource(R.string.knowledge_link_to_another_page))
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.historySection(model: KnowledgePageModel) {
    item(key = "history-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_history)) }
    val history = model.history
    if (history == null) {
        item(key = "history-show") { HistoryLoader(model) }
        return
    }
    if (history.isEmpty()) {
        item(key = "history-empty") { Text(stringResource(R.string.knowledge_no_changes_recorded), modifier = Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
    items(history, key = { revision -> "revision-${revision.revision}" }) { revision ->
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(revision.change)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(KnowledgeWordsShown.actor(revision.actor), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                ServerTime.instant(revision.createdAt)?.let { at ->
                    Text(DateWords.abbreviatedDateTime(at), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            if (revision.before.isNotEmpty()) {
                Text(stringResource(R.string.knowledge_was, revision.before.take(200)), style = MaterialTheme.typography.bodySmall, fontStyle = FontStyle.Normal, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun HistoryLoader(model: KnowledgePageModel) {
    val scope = rememberCoroutineScope()
    TextButton(onClick = { scope.launch { model.loadHistory() } }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.knowledge_show_history)) }
}

