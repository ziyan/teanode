package com.teanode.app.knowledge

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Knowledge. Register every screen of this part here, with `Routes.child(AppSection.Knowledge, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.knowledgeRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Knowledge), deepLinks = Routes.deepLinks(Routes.home(AppSection.Knowledge))) {
        KnowledgeSection()
    }
}

/** What Knowledge plugs into the shell; listed in `Areas`. */
val knowledgeArea = Area(
    section = AppSection.Knowledge,
    routes = NavGraphBuilder::knowledgeRoutes,
    demoAnswerers = knowledgeDemoAnswerers,
)
