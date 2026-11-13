package com.teanode.app.health

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Health. Register every screen of this part here, with `Routes.child(AppSection.Health, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.healthRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Health), deepLinks = Routes.deepLinks(Routes.home(AppSection.Health))) {
        HealthSection()
    }
}

/** What Health plugs into the shell; listed in `Areas`. */
val healthArea = Area(
    section = AppSection.Health,
    routes = NavGraphBuilder::healthRoutes,
    demoAnswerers = healthDemoAnswerers,
)
