package com.teanode.app.photos

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Photos: the library, an album (and who it is shared with), a person, and photo backup. A photo opens in
 * the viewer over the screen it was chosen on, not as a route of its own, so going back lands where the person was.
 */
fun NavGraphBuilder.photosRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Photos), deepLinks = Routes.deepLinks(Routes.home(AppSection.Photos))) {
        PhotosSection()
    }
    val albumRoute = Routes.child(AppSection.Photos, "album/{albumId}")
    composable(albumRoute, arguments = listOf(navArgument("albumId") { type = NavType.StringType }), deepLinks = Routes.deepLinks(albumRoute)) { entry ->
        AlbumScreen(entry.arguments?.getString("albumId").orEmpty(), onBack = { navigator.back() })
    }
    val sharesRoute = Routes.child(AppSection.Photos, "album/{albumId}/shares")
    composable(sharesRoute, arguments = listOf(navArgument("albumId") { type = NavType.StringType })) { entry ->
        AlbumSharesScreen(entry.arguments?.getString("albumId").orEmpty(), onBack = { navigator.back() })
    }
    val personRoute = Routes.child(AppSection.Photos, "person/{personId}")
    composable(personRoute, arguments = listOf(navArgument("personId") { type = NavType.StringType })) { entry ->
        PersonScreen(entry.arguments?.getString("personId").orEmpty(), onBack = { navigator.back() })
    }
    val backupRoute = Routes.child(AppSection.Photos, "backup")
    composable(backupRoute, deepLinks = Routes.deepLinks(backupRoute)) {
        PhotoBackupScreen(onBack = { navigator.back() })
    }
}

/** What Photos plugs into the shell; listed in `Areas`. */
val photosArea = Area(
    section = AppSection.Photos,
    routes = NavGraphBuilder::photosRoutes,
    demoAnswerers = photosDemoAnswerers,
)

