package com.teanode.app.chats

import androidx.compose.runtime.Composable
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** The first screen of Chats: the list of conversations, with the main one first. A conversation chosen opens over it. */
@Composable
fun ChatsSection() {
    val navigator = LocalAppNavigator.current
    ConversationsList { conversation ->
        if (conversation == null || conversation.isMain) {
            navigator.navigate(Routes.child(AppSection.Agent, "main"))
        } else {
            navigator.navigate(Routes.child(AppSection.Agent, "conversation/${conversation.id}"))
        }
    }
}
