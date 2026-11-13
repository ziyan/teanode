package com.teanode.app.location

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Location. Register every screen of this part here, with `Routes.child(AppSection.Location, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.locationRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Location), deepLinks = Routes.deepLinks(Routes.home(AppSection.Location))) {
        LocationSection()
    }
}

/** What Location plugs into the shell; listed in `Areas`. */
val locationArea = Area(
    section = AppSection.Location,
    routes = NavGraphBuilder::locationRoutes,
    demoAnswerers = locationDemoAnswerers,
)
