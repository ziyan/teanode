package com.teanode.app.feed

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.app.R
import com.teanode.app.coding.codingSessionRoute
import com.teanode.app.shell.AppLinkOpener
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.AppSection
import com.teanode.kit.Connection
import com.teanode.kit.feed.AgentFeedChoice
import com.teanode.kit.feed.AgentFeedChoiceRole
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.chooseAgentFeedItem
import com.teanode.kit.feed.dismissAgentFeedItem
import com.teanode.kit.feed.discussAgentFeedItem
import com.teanode.kit.feed.likeAgentFeedItem
import com.teanode.kit.feed.showLessLikeAgentFeedItem
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * FeedActions is what a person does with a card, the same on the feed and on a single card opened from a notification:
 * answer it, like it, show less like it, close it, talk about it. Each call goes to the server, tells the person in a
 * snackbar how it went, and hands the card the server answered with to [onChanged] (or its id to [onRemoved] when it
 * is closed), so the screen shows what the server holds.
 */
class FeedActions(
    private val context: Context,
    private val connection: Connection,
    private val scope: CoroutineScope,
    private val snackbar: SnackbarController,
    private val navigator: AppNavigator,
    private val linkOpener: AppLinkOpener,
    private val onChanged: (AgentFeedItemView) -> Unit,
    private val onRemoved: (String) -> Unit,
) {
    /** The card whose choice is on its way to the server, and the choice, so that button shows it is working and the others wait. */
    var busyChoice: Pair<String, String>? by mutableStateOf(null)
        private set

    fun isBusy(feedItemId: String): Boolean = busyChoice?.first == feedItemId

    fun choose(item: AgentFeedItemView, choice: AgentFeedChoice) {
        when (choice.role) {
            // Discuss opens the main conversation about the card, with nothing recorded as chosen.
            AgentFeedChoiceRole.Discuss -> discuss(item)
            AgentFeedChoiceRole.Open -> open(item)
            AgentFeedChoiceRole.Action, AgentFeedChoiceRole.Dismiss -> answer(item, choice)
        }
    }

    private fun answer(item: AgentFeedItemView, choice: AgentFeedChoice) {
        if (busyChoice != null) return
        busyChoice = item.feedItemId to choice.choiceKey
        scope.launch {
            try {
                val answered = connection.chooseAgentFeedItem(item.feedItemId, choice.choiceKey)
                if (answered.isDismissed) {
                    onRemoved(answered.feedItemId)
                    snackbar.show(context.getString(R.string.feed_said_dismissed))
                } else {
                    onChanged(answered)
                }
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            } finally {
                busyChoice = null
            }
        }
    }

    /** Opens what the card is about: the coding session of a coding card, otherwise the main conversation about it. */
    fun open(item: AgentFeedItemView) {
        val place = item.codingSessionPlace
        if (place != null) {
            navigator.openInSection(AppSection.Coding, codingSessionRoute(place.first, place.second))
        } else {
            discuss(item)
        }
    }

    /** Opens the main conversation with the card attached to the next message, which the server arranges. */
    fun discuss(item: AgentFeedItemView) {
        scope.launch {
            try {
                connection.discussAgentFeedItem(item.feedItemId)
                openMainChat()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
        }
    }

    /** The main conversation, where an action choice is carried out; it needs no server call. */
    fun openMainChat() {
        navigator.navigate(Routes.child(AppSection.Agent, "main"))
    }

    fun toggleLike(item: AgentFeedItemView) {
        val isLiked = !item.isLiked
        // Shown at once, put back when the server refuses, so the heart answers the finger.
        onChanged(item.copy(isLiked = isLiked))
        scope.launch {
            try {
                onChanged(connection.likeAgentFeedItem(item.feedItemId, isLiked))
                snackbar.show(context.getString(if (isLiked) R.string.feed_said_liked else R.string.feed_said_unliked))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onChanged(item)
                snackbar.showError(failure)
            }
        }
    }

    fun showLessLikeThis(item: AgentFeedItemView) {
        scope.launch {
            try {
                connection.showLessLikeAgentFeedItem(item.feedItemId)
                onRemoved(item.feedItemId)
                snackbar.show(context.getString(R.string.feed_said_less))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
        }
    }

    fun followLink(address: String) = linkOpener.followLink(address)

    /** Closes a card without a choice; the feed's only other way to a card leaving is the choice of a dismiss. */
    fun dismiss(item: AgentFeedItemView) {
        scope.launch {
            try {
                connection.dismissAgentFeedItem(item.feedItemId)
                onRemoved(item.feedItemId)
                snackbar.show(context.getString(R.string.feed_said_dismissed))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
        }
    }
}
