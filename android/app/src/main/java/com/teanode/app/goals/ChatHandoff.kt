package com.teanode.app.goals

import com.teanode.app.chats.ChatsHooks
import com.teanode.app.shell.AppNavigator

/**
 * ChatHandoff is how Goals, Ideas, Knowledge and the Journal open a conversation in Chats. It is Chats' own hook: the
 * conversation opens, and the request an idea starts with is sent once as it opens.
 */
object ChatHandoff {
    fun open(navigator: AppNavigator, conversationId: String, openingRequest: String? = null) {
        ChatsHooks.openConversation(navigator, conversationId, openingRequest)
    }
}
