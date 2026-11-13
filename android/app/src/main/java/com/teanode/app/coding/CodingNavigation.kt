package com.teanode.app.coding

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Coding. Register every screen of this part here, with `Routes.child(AppSection.Coding, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.codingRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Coding), deepLinks = Routes.deepLinks(Routes.home(AppSection.Coding))) {
        CodingSection()
    }
}

/** What Coding plugs into the shell; listed in `Areas`. */
val codingArea = Area(
    section = AppSection.Coding,
    routes = NavGraphBuilder::codingRoutes,
    demoAnswerers = codingDemoAnswerers,
)
