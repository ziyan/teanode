package com.teanode.app.chats

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

private val mainRoute = Routes.child(AppSection.Agent, "main")
private val conversationRoute = Routes.child(AppSection.Agent, "conversation/{conversationId}")

/** The routes of Chats: the list, the main conversation, and any other conversation by its id. */
fun NavGraphBuilder.chatsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Agent), deepLinks = Routes.deepLinks(Routes.home(AppSection.Agent))) { ChatsSection() }
    composable(mainRoute, deepLinks = Routes.deepLinks(mainRoute)) {
        ConversationScreen(conversationId = null, onBack = { navigator.back() }, onOpenConversation = { id -> navigator.navigate(Routes.child(AppSection.Agent, "conversation/$id")) })
    }
    composable(conversationRoute, arguments = listOf(navArgument("conversationId") { type = NavType.StringType }), deepLinks = Routes.deepLinks(conversationRoute)) { entry ->
        val conversationId = entry.arguments?.getString("conversationId").orEmpty()
        ConversationScreen(conversationId = conversationId, onBack = { navigator.back() }, onOpenConversation = { id -> navigator.navigate(Routes.child(AppSection.Agent, "conversation/$id")) })
    }
}

/** What Chats plugs into the shell; listed in `Areas`. */
val chatsArea = Area(
    section = AppSection.Agent,
    routes = NavGraphBuilder::chatsRoutes,
    demoAnswerers = chatsDemoAnswerers,
)
