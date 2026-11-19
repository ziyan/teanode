package com.teanode.app.feed

import com.teanode.app.coding.codingSessionRoute
import com.teanode.app.shell.AppLinkOpener
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.feed.AgentFeedChoice
import com.teanode.kit.feed.AgentFeedChoiceRole
import com.teanode.kit.feed.AgentFeedItemView
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch

/**
 * FeedActions is what a person does with a card, wired to where it goes (ios FeedList): an action or dismiss choice is
 * sent to the server by the [model]; Discuss and a discuss choice open the main conversation, where the server has
 * attached the card to the next message; an open choice opens the coding session the card is about, or the Coding part.
 */
class FeedActions(
    val model: FeedModel,
    private val scope: CoroutineScope,
    private val navigator: AppNavigator,
    private val linkOpener: AppLinkOpener,
) {
    fun isBusy(item: AgentFeedItemView): Boolean = model.busyFeedItemIds.containsKey(item.feedItemId)

    fun choose(item: AgentFeedItemView, choice: AgentFeedChoice) {
        when (choice.role) {
            AgentFeedChoiceRole.Discuss -> discuss(item)
            AgentFeedChoiceRole.Open -> open(item)
            AgentFeedChoiceRole.Action, AgentFeedChoiceRole.Dismiss -> model.choose(item, choice)
        }
    }

    fun discuss(item: AgentFeedItemView) {
        scope.launch { if (model.discuss(item)) openMainChat() }
    }

    /** The main conversation, where an action choice is carried out; it needs no server call. */
    fun openMainChat() {
        navigator.navigate(Routes.child(AppSection.Agent, "main"))
    }

    fun open(item: AgentFeedItemView) {
        val place = item.codingSessionPlace
        if (place != null) navigator.openInSection(AppSection.Coding, codingSessionRoute(place.first, place.second)) else navigator.openSection(AppSection.Coding)
    }

    fun openGoals() = navigator.openSection(AppSection.Goals)

    fun followLink(address: String) = linkOpener.followLink(address)
}
