package com.teanode.app.ideas

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.clickable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowUpward
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.goals.ChatHandoff
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.Connection
import com.teanode.kit.ideas.IdeaEntry
import com.teanode.kit.ideas.allIdeas
import com.teanode.kit.ideas.setIdeaStatus
import com.teanode.kit.ideas.startIdea
import kotlinx.coroutines.launch

/**
 * IdeasSection is the dashboard's Ideas tab (ios IdeasView.swift, IdeasScreen): ideas found for the person in their
 * own mail and memory, the catalog's ideas by category, and what became of the rest. Trying one starts a conversation
 * of its own and sends the request it opens with; nothing starts before that.
 */
@Composable
fun IdeasSection() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val language = Connection.shownLanguage()
    val ideas = rememberLoad { connection.allIdeas(language) }
    var isBusy by remember { mutableStateOf(false) }
    val saidDismissed = stringResource(R.string.ideas_said_dismissed)
    val saidDone = stringResource(R.string.ideas_said_done)
    val saidRestored = stringResource(R.string.ideas_said_restored)

    fun setStatus(idea: IdeaEntry, ideaStatus: String, said: String) {
        scope.launch {
            try {
                connection.setIdeaStatus(idea.id, ideaStatus)
                snackbar.show(said)
                ideas.refresh()
            } catch (failure: Exception) {
                snackbar.showError(failure)
            }
        }
    }

    /** Starts the idea in a conversation of its own and opens it, which sends the request the idea opens with. */
    fun start(idea: IdeaEntry) {
        isBusy = true
        scope.launch {
            try {
                val started = connection.startIdea(idea.id, null, language)
                val conversationId = started.conversationId
                if (conversationId != null) ChatHandoff.open(navigator, conversationId, started.openingRequest)
            } catch (failure: Exception) {
                snackbar.showError(failure)
            }
            isBusy = false
        }
    }

    TeaNodeScreen(title = stringResource(R.string.ideas_title)) { contentPadding ->
        LoadedContent(ideas, modifier = Modifier.fillMaxSize().padding(contentPadding)) { list ->
            LazyColumn(Modifier.fillMaxSize()) {
                item(key = "for-you") { TeaNodeSectionHeader(stringResource(R.string.ideas_for_you)) }
                if (list.forYou.isEmpty()) {
                    item(key = "for-you-empty") {
                        Text(
                            stringResource(R.string.ideas_for_you_empty),
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                items(list.forYou, key = { idea -> "you-${idea.id}" }) { idea ->
                    OpenIdeaRow(idea, isBusy, onTry = { start(idea) }, onDismiss = { setStatus(idea, "dismissed", saidDismissed) })
                }
                item(key = "for-you-footer") {
                    Text(
                        stringResource(R.string.ideas_for_you_footer),
                        modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                for (group in list.byCategory) {
                    item(key = "category-${group.ideaCategory}") { TeaNodeSectionHeader(IdeaWords.category(group.ideaCategory)) }
                    items(group.ideas, key = { idea -> "catalog-${idea.id}" }) { idea ->
                        OpenIdeaRow(idea, isBusy, onTry = { start(idea) }, onDismiss = { setStatus(idea, "dismissed", saidDismissed) })
                    }
                }
                historySection(list.history, onOpenConversation = { conversationId -> ChatHandoff.open(navigator, conversationId) },
                    onDone = { idea -> setStatus(idea, "done", saidDone) }, onRestore = { idea -> setStatus(idea, "open", saidRestored) })
            }
        }
    }
}

private fun LazyListScope.historySection(
    history: List<IdeaEntry>,
    onOpenConversation: (String) -> Unit,
    onDone: (IdeaEntry) -> Unit,
    onRestore: (IdeaEntry) -> Unit,
) {
    if (history.isEmpty()) return
    item(key = "history") { TeaNodeSectionHeader(stringResource(R.string.ideas_history)) }
    items(history, key = { idea -> "history-${idea.id}" }) { idea ->
        HistoryIdeaRow(idea, onOpenConversation, onDone, onRestore)
    }
}

/** An open idea: its emoji, headline, body and what prompted it, with "Try it" on the tap and Dismiss in the menu. */
@Composable
private fun OpenIdeaRow(idea: IdeaEntry, isBusy: Boolean, onTry: () -> Unit, onDismiss: () -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    val tryLabel = stringResource(R.string.ideas_try_it)
    Row(
        Modifier.fillMaxWidth().clickable(enabled = !isBusy, onClickLabel = tryLabel, onClick = onTry).padding(horizontal = 16.dp, vertical = 10.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Text(idea.emoji ?: "💡", style = MaterialTheme.typography.headlineSmall)
        androidx.compose.foundation.layout.Column(Modifier.weight(1f)) {
            Text(idea.headline, style = MaterialTheme.typography.bodyLarge)
            idea.body?.takeIf { text -> text.isNotEmpty() }?.let { text ->
                Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
            idea.suggestionReason?.takeIf { text -> text.isNotEmpty() }?.let { text ->
                Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.outline, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
        Icon(Icons.Outlined.ArrowUpward, contentDescription = tryLabel, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(top = 4.dp).size(20.dp))
        androidx.compose.foundation.layout.Box {
            IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.ideas_more_actions)) }
            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.ideas_dismiss)) }, onClick = {
                    isMenuOpen = false
                    onDismiss()
                })
            }
        }
    }
}

/**
 * What became of an idea; a tap opens the conversation that carried it out while it is still there, as the
 * dashboard's history does. The server forgets the conversation when it is deleted.
 */
@Composable
private fun HistoryIdeaRow(idea: IdeaEntry, onOpenConversation: (String) -> Unit, onDone: (IdeaEntry) -> Unit, onRestore: (IdeaEntry) -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    val conversationId = idea.startedConversationId?.takeIf { id -> id.isNotEmpty() }
    TeaNodeRow(
        headline = idea.headline,
        supporting = IdeaWords.status(idea.ideaStatus),
        leading = { Text(idea.emoji ?: "💡", style = MaterialTheme.typography.titleLarge) },
        onClick = conversationId?.let { id -> { onOpenConversation(id) } },
        trailing = if (idea.ideaStatus == "started" || idea.canRestore) {
            {
                androidx.compose.foundation.layout.Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.ideas_more_actions)) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        if (idea.ideaStatus == "started") {
                            DropdownMenuItem(text = { Text(stringResource(R.string.ideas_done)) }, onClick = {
                                isMenuOpen = false
                                onDone(idea)
                            })
                        }
                        if (idea.canRestore) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.ideas_restore)) }, onClick = {
                                isMenuOpen = false
                                onRestore(idea)
                            })
                        }
                    }
                }
            }
        } else {
            null
        },
    )
}

/** IdeaWords are an idea's category and status, as the dashboard says them. */
object IdeaWords {
    @Composable
    fun category(ideaCategory: String): String = when (ideaCategory) {
        "assistant" -> stringResource(R.string.ideas_category_assistant)
        "family" -> stringResource(R.string.ideas_category_family)
        "fun" -> stringResource(R.string.ideas_category_fun)
        "health" -> stringResource(R.string.ideas_category_health)
        "home" -> stringResource(R.string.ideas_category_home)
        "mail" -> stringResource(R.string.ideas_category_mail)
        "money" -> stringResource(R.string.ideas_category_money)
        "paperwork" -> stringResource(R.string.ideas_category_paperwork)
        "shopping" -> stringResource(R.string.ideas_category_shopping)
        "travel" -> stringResource(R.string.ideas_category_travel)
        "work" -> stringResource(R.string.ideas_category_work)
        else -> ideaCategory
    }

    @Composable
    fun status(ideaStatus: String): String = when (ideaStatus) {
        "done" -> stringResource(R.string.ideas_status_done)
        "dismissed" -> stringResource(R.string.ideas_status_dismissed)
        "started" -> stringResource(R.string.ideas_status_started)
        "expired" -> stringResource(R.string.ideas_status_expired)
        else -> ideaStatus
    }
}
