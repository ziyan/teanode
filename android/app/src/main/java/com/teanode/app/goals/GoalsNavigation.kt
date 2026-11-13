package com.teanode.app.goals

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Goals. Register every screen of this part here, with `Routes.child(AppSection.Goals, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.goalsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Goals), deepLinks = Routes.deepLinks(Routes.home(AppSection.Goals))) {
        GoalsSection()
    }
}

/** What Goals plugs into the shell; listed in `Areas`. */
val goalsArea = Area(
    section = AppSection.Goals,
    routes = NavGraphBuilder::goalsRoutes,
    demoAnswerers = goalsDemoAnswerers,
)
