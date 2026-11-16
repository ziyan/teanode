package com.teanode.app.photos

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Collections
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.PhotoLibrary
import com.teanode.app.R
import com.teanode.app.shell.AppPlace
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** The routes other parts open Photos on. */
object PhotoRoutes {
    val albums: String = Routes.child(AppSection.Photos, "albums")

    /** The library opened in one of its sections, which Search offers by name. */
    fun section(choice: PhotoSectionChoice): String = Routes.child(AppSection.Photos, "section/${choice.name.lowercase()}")
}

/**
 * The routes of Photos: the library, an album (and who it is shared with), a person, and photo backup. A photo opens in
 * the viewer over the screen it was chosen on, not as a route of its own, so going back lands where the person was.
 */
fun NavGraphBuilder.photosRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Photos), deepLinks = Routes.deepLinks(Routes.home(AppSection.Photos))) {
        PhotosSection()
    }
    // Where another part's "Show all" lands for the albums.
    composable(PhotoRoutes.albums) { PhotosSection(initialSection = PhotoSectionChoice.Albums) }
    composable(Routes.child(AppSection.Photos, "section/{section}")) { entry ->
        val choice = PhotoSectionChoice.entries.firstOrNull { candidate -> candidate.name.equals(entry.arguments?.getString("section"), ignoreCase = true) } ?: PhotoSectionChoice.Library
        PhotosSection(initialSection = choice)
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
        PhotoBackupScreen(onBack = { navigator.back() }, onOpenFiles = { navigator.navigate(Routes.child(AppSection.Photos, "backup/files")) })
    }
    composable(Routes.child(AppSection.Photos, "backup/files")) { PhotoBackupFilesScreen(onBack = { navigator.back() }) }
}

/** What Photos plugs into the shell; listed in `Areas`. */
val photosArea = Area(
    section = AppSection.Photos,
    routes = NavGraphBuilder::photosRoutes,
    places = {
        listOf(
            PhotoSectionChoice.Library to (R.string.photos_section_library to Icons.Outlined.PhotoLibrary),
            PhotoSectionChoice.Favorites to (R.string.photos_section_favorites to Icons.Outlined.FavoriteBorder),
            PhotoSectionChoice.Albums to (R.string.photos_section_albums to Icons.Outlined.Collections),
            PhotoSectionChoice.People to (R.string.photos_section_people to Icons.Outlined.People),
            PhotoSectionChoice.Trash to (R.string.photos_section_trash to Icons.Outlined.Delete),
        ).map { (choice, titleAndIcon) ->
            AppPlace("photos-${choice.name.lowercase()}", titleAndIcon.first, PhotoRoutes.section(choice), titleAndIcon.second, listOf(R.string.section_photos))
        }
    },
    demoAnswerers = photosDemoAnswerers,
)

