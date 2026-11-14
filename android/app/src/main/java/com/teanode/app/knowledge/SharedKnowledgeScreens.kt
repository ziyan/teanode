package com.teanode.app.knowledge

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.PersonRemove
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
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
import com.teanode.app.calendar.ShareTargetPicker
import com.teanode.app.calendar.shareTargetName
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.isNotFoundRefusal
import com.teanode.kit.calendar.CalendarShareCircle
import com.teanode.kit.calendar.CalendarSharePerson
import com.teanode.kit.calendar.ShareTarget
import com.teanode.kit.calendar.calendarShareCircles
import com.teanode.kit.calendar.calendarSharePeople
import com.teanode.kit.knowledge.KnowledgeShare
import com.teanode.kit.knowledge.KnowledgeSharePreview
import com.teanode.kit.knowledge.SharedKnowledgePage
import com.teanode.kit.knowledge.knowledgeShares
import com.teanode.kit.knowledge.previewKnowledgeShare
import com.teanode.kit.knowledge.shareKnowledgePage
import com.teanode.kit.knowledge.sharedKnowledgePage
import com.teanode.kit.knowledge.unshareKnowledgePage
import kotlinx.coroutines.launch

/**
 * SharedKnowledgeSections is a shared page as it is read, and as its owner sees it before they confirm a share: the
 * opening, the facts with where they were read (and the words, where the owner shared them), the links to other shared
 * pages and the pages under it. With an owner the links and the pages under it open; without one (the preview) they do
 * not (ios KnowledgeSharingViews.swift).
 */
internal fun LazyListScope.sharedKnowledgeSections(page: SharedKnowledgePage, openingOwnerUserId: String?, onOpen: (String, String) -> Unit) {
    item(key = "shared-opening") {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            if (page.aliases.isNotEmpty()) {
                Text(stringResource(R.string.knowledge_also_called, page.aliases.joinToString(", ")), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (page.opening.isEmpty()) Text(stringResource(R.string.knowledge_no_summary_yet), color = MaterialTheme.colorScheme.onSurfaceVariant) else MarkdownText(page.opening)
            Text(stringResource(R.string.knowledge_overview_not_shared), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
    item(key = "shared-facts-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_facts)) }
    if (page.presentFacts.isEmpty()) {
        item(key = "shared-facts-empty") { Text(stringResource(R.string.knowledge_no_facts_shared), modifier = Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
    items(page.presentFacts, key = { fact -> "shared-fact-${fact.number}" }) { fact ->
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("#${fact.number} ${fact.text}")
            if (fact.isInferred) Chip(stringResource(R.string.knowledge_worked_out_not_told))
            fact.evidenceSource?.takeIf { source -> source.isNotEmpty() }?.let { source -> Text(source, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            fact.evidenceQuote?.takeIf { quote -> quote.isNotEmpty() }?.let { quote ->
                Text(quote, style = MaterialTheme.typography.bodySmall, fontStyle = FontStyle.Italic, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    if (page.presentEdges.isNotEmpty()) {
        item(key = "shared-edges-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_links_to_other_shared_pages)) }
        items(page.presentEdges.size, key = { index -> "shared-edge-$index" }) { index ->
            val edge = page.presentEdges[index]
            val far = if (edge.fromPath == page.path) edge.toPath else edge.fromPath
            Column(
                Modifier.fillMaxWidth().then(if (openingOwnerUserId != null) Modifier.clickable { onOpen(openingOwnerUserId, far) } else Modifier).padding(horizontal = 16.dp, vertical = 10.dp),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Text("${edge.relation.replace('_', ' ')} $far")
                if (edge.isProposed) Text(stringResource(R.string.knowledge_the_agents_guess), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    if (page.presentChildren.isNotEmpty()) {
        item(key = "shared-children-header") { TeaNodeSectionHeader(stringResource(R.string.knowledge_pages_under_it)) }
        items(page.presentChildren, key = { child -> "shared-child-${child.path}" }) { child ->
            Column(
                Modifier.fillMaxWidth().then(if (openingOwnerUserId != null) Modifier.clickable { onOpen(openingOwnerUserId, child.path) } else Modifier).padding(horizontal = 16.dp, vertical = 10.dp),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Text(child.name.ifEmpty { child.path })
                child.opening?.takeIf { opening -> opening.isNotEmpty() }?.let { opening ->
                    Text(opening, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2)
                }
            }
        }
    }
}

/** SharedKnowledgePageScreen is a page another person shared, read only, with whose it is (ios KnowledgeSharingViews.swift, SharedKnowledgePageView). */
@Composable
fun SharedKnowledgePageScreen(ownerUserId: String, pagePath: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    var page by remember(ownerUserId, pagePath) { mutableStateOf<SharedKnowledgePage?>(null) }
    var problem by remember(ownerUserId, pagePath) { mutableStateOf<Throwable?>(null) }
    // Whether the server answered that the page is not shared, as against a read that did not get through.
    var isNotShared by remember(ownerUserId, pagePath) { mutableStateOf(false) }
    var attempt by remember(ownerUserId, pagePath) { mutableStateOf(0) }
    androidx.compose.runtime.LaunchedEffect(ownerUserId, pagePath, attempt) {
        try {
            page = connection.sharedKnowledgePage(ownerUserId, pagePath)
            problem = null
            isNotShared = false
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (page == null) {
                // A page that is not shared answers as not found, whoever asks and whatever the reason.
                if (failure.isNotFoundRefusal) isNotShared = true else problem = failure
            }
        }
    }
    val read = page
    TeaNodeScreen(title = read?.shownName.orEmpty(), onBack = { navigator.back() }) { contentPadding ->
        when {
            isNotShared -> EmptyState(Icons.Outlined.Lock, stringResource(R.string.knowledge_page_not_available), Modifier.padding(contentPadding), description = stringResource(R.string.knowledge_page_not_shared_with_you))
            read == null && problem != null -> ErrorState(session.describeLoadProblem(problem!!), onRetry = { problem = null; attempt += 1 }, modifier = Modifier.padding(contentPadding))
            read == null -> LoadingState(Modifier.padding(contentPadding))
            else -> LazyColumn(Modifier.fillMaxSize().padding(contentPadding)) {
                item {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            AvatarView(read.shownOwner, read.ownerAvatarUrl, read.ownerAvatarColorIndex, size = 28.dp)
                            Column {
                                Text(stringResource(R.string.knowledge_shared_by, read.shownOwner))
                                Text(read.path, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            Chip(stringResource(R.string.knowledge_read_only))
                            if (read.isQuoteShared) Chip(stringResource(R.string.knowledge_quotes_shown))
                        }
                    }
                }
                sharedKnowledgeSections(read, ownerUserId) { owner, path -> navigator.navigate(KnowledgeRoutes.shared(owner, path)) }
            }
        }
    }
}

/** Who the page can be shared with, read once: the shares so far, and the people and circles it could be shared with. */
private data class KnowledgeSharingSheet(val shares: List<KnowledgeShare>, val people: List<CalendarSharePerson>, val circles: List<CalendarShareCircle>)

/**
 * KnowledgeShareScreen shares one of the person's pages, and everything under it, with a person they share a circle
 * with or a circle they are in. It lists who the page is already shared with (a share of a page above it reaches this
 * one too), with a way to stop each, and offers the people and circles the server would accept. Sharing opens exactly
 * what the other side will be shown first, and is confirmed there, once that has been read (ios KnowledgeShareSheet).
 */
@Composable
fun KnowledgeShareScreen(pagePath: String, onClose: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    val sheet = rememberLoad(pagePath) { KnowledgeSharingSheet(connection.knowledgeShares(), connection.calendarSharePeople(), connection.calendarShareCircles()) }
    var chosen by remember { mutableStateOf<ShareTarget?>(null) }
    var isQuoteShared by remember { mutableStateOf(false) }
    var stopping by remember { mutableStateOf<KnowledgeShare?>(null) }
    var isPreviewing by remember { mutableStateOf(false) }
    var isWorking by remember { mutableStateOf(false) }
    val failedText = stringResource(R.string.knowledge_sharing_not_changed)
    val loadedSheet = sheet.value
    val sharedWithWords = stringResource(R.string.calendar_shared_with_name, shareTargetName(chosen, loadedSheet?.people.orEmpty(), loadedSheet?.circles.orEmpty()))

    if (isPreviewing) {
        KnowledgeSharePreviewScreen(
            pagePath, chosen?.granteeUserId, chosen?.granteeCircleId, isQuoteShared, onBack = { isPreviewing = false },
            onShare = {
                val target = chosen ?: return@KnowledgeSharePreviewScreen false
                isWorking = true
                val isShared = try {
                    connection.shareKnowledgePage(pagePath, target.granteeUserId, target.granteeCircleId, isQuoteShared)
                    snackbar.show(sharedWithWords)
                    chosen = null
                    isQuoteShared = false
                    sheet.refresh()
                    true
                } catch (failure: kotlinx.coroutines.CancellationException) {
                    throw failure
                } catch (failure: Throwable) {
                    snackbar.showProblem(session.describe(failure).ifEmpty { failedText })
                    false
                }
                isWorking = false
                isShared
            },
        )
        return
    }

    TeaNodeScreen(title = stringResource(R.string.knowledge_share_this_page), onBack = onClose) { contentPadding ->
        LoadedContent(sheet, modifier = Modifier.fillMaxSize().padding(contentPadding)) { loaded ->
            val covering = loaded.shares.filter { share -> share.covers(pagePath) }
            LazyColumn(Modifier.fillMaxSize()) {
                item { TeaNodeSectionHeader(stringResource(R.string.knowledge_shared_with)) }
                if (covering.isEmpty()) item { Text(stringResource(R.string.knowledge_not_shared_yet), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                items(covering, key = { share -> share.id }) { share ->
                    val name = if (share.isWithCircle) stringResource(R.string.calendar_everyone_in, share.granteeDisplayName) else share.granteeDisplayName
                    Row(Modifier.fillMaxWidth().padding(start = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                            Text(name)
                            if (share.path != pagePath) Text(stringResource(R.string.knowledge_shared_as_part_of, share.path), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            if (share.isQuoteShared) Text(stringResource(R.string.knowledge_quotes_shown), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        IconButton(onClick = { stopping = share }) { Icon(Icons.Outlined.PersonRemove, contentDescription = stringResource(R.string.calendar_stop_sharing)) }
                    }
                }
                if (loaded.people.isEmpty() && loaded.circles.isEmpty()) {
                    item { Text(stringResource(R.string.knowledge_no_circle_yet), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                } else {
                    item { ShareTargetPicker(loaded.people, loaded.circles, chosen) { target -> chosen = target } }
                    item { Text(stringResource(R.string.knowledge_choose_person_or_circle_footer), modifier = Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    item {
                        Row(Modifier.fillMaxWidth().clickable { isQuoteShared = !isQuoteShared }.padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                            Text(stringResource(R.string.knowledge_also_show_words), Modifier.weight(1f))
                            Switch(checked = isQuoteShared, onCheckedChange = { on -> isQuoteShared = on })
                        }
                    }
                    item { Text(stringResource(R.string.knowledge_quotes_footer), modifier = Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    item {
                        androidx.compose.material3.Button(enabled = chosen != null && !isWorking, onClick = { isPreviewing = true }, modifier = Modifier.padding(16.dp)) {
                            Text(stringResource(R.string.calendar_share))
                        }
                    }
                    item { Text(stringResource(R.string.knowledge_preview_footer), modifier = Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
            }
        }
    }
    stopping?.let { share ->
        val name = if (share.isWithCircle) stringResource(R.string.calendar_everyone_in, share.granteeDisplayName) else share.granteeDisplayName
        val stoppedWords = stringResource(R.string.calendar_stopped_sharing_with, name)
        ConfirmationDialog(
            title = stringResource(R.string.calendar_stop_sharing),
            text = stringResource(R.string.knowledge_will_no_longer_read, name),
            confirmLabel = stringResource(R.string.calendar_stop_sharing),
            isDestructive = true,
            onConfirm = {
                stopping = null
                isWorking = true
                scope.launch {
                    try {
                        connection.unshareKnowledgePage(share.id)
                        snackbar.show(stoppedWords)
                        sheet.refresh()
                    } catch (failure: kotlinx.coroutines.CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        snackbar.showProblem(session.describe(failure).ifEmpty { failedText })
                    }
                    isWorking = false
                }
            },
            onDismiss = { stopping = null },
        )
    }
}

/**
 * KnowledgeSharePreviewScreen is exactly what the person or circle chosen would be shown of the page, drawn the way they
 * would read it, and how many pages the share reaches. Nothing is shared by looking: Share is here, and is on only once
 * the preview has been read, so a page is never shared unseen (ios KnowledgeSharingViews.swift).
 */
@Composable
fun KnowledgeSharePreviewScreen(
    pagePath: String,
    granteeUserId: String?,
    granteeCircleId: String?,
    isQuoteShared: Boolean,
    onBack: () -> Unit,
    onShare: suspend () -> Boolean,
) {
    val connection = LocalConnection.current
    val scope = rememberCoroutineScope()
    val preview = rememberLoad(pagePath, granteeUserId, granteeCircleId, isQuoteShared) { connection.previewKnowledgeShare(pagePath, granteeUserId, granteeCircleId, isQuoteShared) }
    var isSharing by remember { mutableStateOf(false) }
    TeaNodeScreen(
        title = stringResource(R.string.knowledge_what_they_will_see), onBack = onBack,
        actions = {
            TextButton(enabled = preview.value != null && !isSharing, onClick = {
                isSharing = true
                scope.launch {
                    if (onShare()) onBack()
                    isSharing = false
                }
            }) { Text(stringResource(R.string.calendar_share)) }
        },
    ) { contentPadding ->
        LoadedContent(preview, modifier = Modifier.fillMaxSize().padding(contentPadding)) { read ->
            LazyColumn(Modifier.fillMaxSize()) {
                item {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(pagesText(read))
                        Text(stringResource(R.string.knowledge_added_later_shared_too), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                if (read.reachedPagePaths.isNotEmpty()) {
                    items(read.reachedPagePaths, key = { reached -> "reached-$reached" }) { reached ->
                        Text(reached, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
                    }
                    val morePageCount = read.reachedPageCount - read.reachedPagePaths.size
                    if (morePageCount > 0) item { Text(stringResource(R.string.knowledge_and_n_more, morePageCount), modifier = Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
                if (read.withheldFactCount > 0) {
                    item {
                        Text(
                            if (read.withheldFactCount == 1) stringResource(R.string.knowledge_withheld_one) else stringResource(R.string.knowledge_withheld_many, read.withheldFactCount),
                            modifier = Modifier.padding(16.dp),
                        )
                    }
                }
                sharedKnowledgeSections(read.page, null) { _, _ -> }
            }
        }
    }
}

/** How many pages the share reaches, as the dashboard says it. */
@Composable
private fun pagesText(preview: KnowledgeSharePreview): String {
    val count = preview.reachedPageCount
    return when {
        !preview.isReachedPageCountCut && count == 1 -> stringResource(R.string.knowledge_shares_one_page)
        !preview.isReachedPageCountCut -> stringResource(R.string.knowledge_shares_pages, count)
        count == 1 -> stringResource(R.string.knowledge_shares_at_least_one_page)
        else -> stringResource(R.string.knowledge_shares_at_least_pages, count)
    }
}
