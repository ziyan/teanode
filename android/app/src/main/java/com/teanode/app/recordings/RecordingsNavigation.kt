package com.teanode.app.recordings

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Recordings. Register every screen of this part here, with `Routes.child(AppSection.Recordings, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.recordingsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Recordings), deepLinks = Routes.deepLinks(Routes.home(AppSection.Recordings))) {
        RecordingsSection()
    }
}

/** What Recordings plugs into the shell; listed in `Areas`. */
val recordingsArea = Area(
    section = AppSection.Recordings,
    routes = NavGraphBuilder::recordingsRoutes,
    demoAnswerers = recordingsDemoAnswerers,
)
