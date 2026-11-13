package com.teanode.app.goals

import android.net.Uri
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * ChatHandoff is how Goals, Ideas and the Journal open a conversation in Chats: the person is taken to
 * `agent/conversation/{conversationId}`, with the request an idea opens with in the optional `openingRequest` query
 * argument, which Chats sends once as the conversation opens (the iPhone app's `openingRequest` and
 * `openedConversationId`). Chats registers the route; this is only the address.
 */
object ChatHandoff {
    const val CONVERSATION_PATTERN = "conversation/{conversationId}?openingRequest={openingRequest}"

    /** The route Chats registers for a conversation. */
    val conversationRoute: String get() = Routes.child(AppSection.Agent, CONVERSATION_PATTERN)

    /**
     * Takes the person to a conversation. When the Chats part has not registered the place yet, it opens Chats at its
     * first screen instead of failing.
     */
    fun open(navigator: com.teanode.app.shell.AppNavigator, conversationId: String, openingRequest: String? = null) {
        try {
            navigator.navigate(conversation(conversationId, openingRequest))
        } catch (_: IllegalArgumentException) {
            navigator.openSection(AppSection.Agent)
        }
    }

    /** The address of a conversation, with the request it opens with when there is one. */
    fun conversation(conversationId: String, openingRequest: String? = null): String {
        val base = Routes.child(AppSection.Agent, "conversation/${Uri.encode(conversationId)}")
        return if (openingRequest.isNullOrEmpty()) base else "$base?openingRequest=${Uri.encode(openingRequest)}"
    }
}
