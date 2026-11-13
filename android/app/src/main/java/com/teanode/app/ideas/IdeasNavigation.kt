package com.teanode.app.ideas

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Ideas. Register every screen of this part here, with `Routes.child(AppSection.Ideas, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.ideasRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Ideas), deepLinks = Routes.deepLinks(Routes.home(AppSection.Ideas))) {
        IdeasSection()
    }
}

/** What Ideas plugs into the shell; listed in `Areas`. */
val ideasArea = Area(
    section = AppSection.Ideas,
    routes = NavGraphBuilder::ideasRoutes,
    demoAnswerers = ideasDemoAnswerers,
)
