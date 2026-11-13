package com.teanode.app.chats

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Chats. Register every screen of this part here, with `Routes.child(AppSection.Agent, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.chatsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Agent), deepLinks = Routes.deepLinks(Routes.home(AppSection.Agent))) {
        ChatsSection()
    }
}

/** What Chats plugs into the shell; listed in `Areas`. */
val chatsArea = Area(
    section = AppSection.Agent,
    routes = NavGraphBuilder::chatsRoutes,
    demoAnswerers = chatsDemoAnswerers,
)
