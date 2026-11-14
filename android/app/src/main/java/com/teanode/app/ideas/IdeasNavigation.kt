package com.teanode.app.ideas

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** The routes of Ideas: its one screen. */
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
