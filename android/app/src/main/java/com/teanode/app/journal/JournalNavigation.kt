package com.teanode.app.journal

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Journal. Register every screen of this part here, with `Routes.child(AppSection.Journal, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.journalRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Journal), deepLinks = Routes.deepLinks(Routes.home(AppSection.Journal))) {
        JournalSection()
    }
}

/** What Journal plugs into the shell; listed in `Areas`. */
val journalArea = Area(
    section = AppSection.Journal,
    routes = NavGraphBuilder::journalRoutes,
    demoAnswerers = journalDemoAnswerers,
)
