package com.teanode.app.shopping

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Shopping. Register every screen of this part here, with `Routes.child(AppSection.Shopping, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.shoppingRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Shopping), deepLinks = Routes.deepLinks(Routes.home(AppSection.Shopping))) {
        ShoppingSection()
    }
}

/** What Shopping plugs into the shell; listed in `Areas`. */
val shoppingArea = Area(
    section = AppSection.Shopping,
    routes = NavGraphBuilder::shoppingRoutes,
    demoAnswerers = shoppingDemoAnswerers,
)
