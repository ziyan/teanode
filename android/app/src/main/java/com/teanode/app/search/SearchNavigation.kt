package com.teanode.app.search

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes

/**
 * The routes of search, the last item of the bottom bar. Search is not a part of its own: its route is `Routes.SEARCH`,
 * registered beside the parts' graphs rather than inside one.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.searchRoutes(navigator: AppNavigator) {
    composable(Routes.SEARCH, deepLinks = Routes.deepLinks(Routes.SEARCH)) {
        SearchSection()
    }
}

/** What search plugs into the shell; listed in `Areas`. The shell opens it from the bar, so it has no part of its own. */
val searchArea = Area(
    section = null,
    routes = NavGraphBuilder::searchRoutes,
    demoAnswerers = searchDemoAnswerers,
)
