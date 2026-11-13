package com.teanode.app.photos

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Photos. Register every screen of this part here, with `Routes.child(AppSection.Photos, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.photosRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Photos), deepLinks = Routes.deepLinks(Routes.home(AppSection.Photos))) {
        PhotosSection()
    }
}

/** What Photos plugs into the shell; listed in `Areas`. */
val photosArea = Area(
    section = AppSection.Photos,
    routes = NavGraphBuilder::photosRoutes,
    demoAnswerers = photosDemoAnswerers,
)
