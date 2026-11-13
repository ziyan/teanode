package com.teanode.app.recordings

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.recordings.RecordingLink

/**
 * The routes of Recordings: the list, a recording (opened at a passage from a link the agent or the dashboard wrote), one the
 * phone still holds, the recorder, sharing, and the recordings shared with the person.
 */
fun NavGraphBuilder.recordingsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Recordings), deepLinks = Routes.deepLinks(Routes.home(AppSection.Recordings))) { RecordingsSection() }
    val idArgument = listOf(navArgument("recordingId") { type = NavType.StringType })
    val detailRoute = Routes.child(AppSection.Recordings, "detail/{recordingId}")
    composable(detailRoute, arguments = idArgument, deepLinks = Routes.deepLinks(detailRoute)) { entry ->
        RecordingDetailScreen(entry.arguments?.getString("recordingId").orEmpty(), onBack = { navigator.back() })
    }
    val citationRoute = Routes.child(AppSection.Recordings, "cite/{recordingId}/{runId}/{startMS}/{endMS}")
    composable(
        citationRoute,
        arguments = idArgument + listOf(navArgument("runId") { type = NavType.StringType }, navArgument("startMS") { type = NavType.LongType }, navArgument("endMS") { type = NavType.LongType }),
        deepLinks = Routes.deepLinks(citationRoute),
    ) { entry ->
        val recordingId = entry.arguments?.getString("recordingId").orEmpty()
        val link = RecordingLink(recordingId, entry.arguments?.getString("runId"), entry.arguments?.getLong("startMS"), entry.arguments?.getLong("endMS"))
        RecordingDetailScreen(recordingId, onBack = { navigator.back() }, link = link)
    }
    composable(Routes.child(AppSection.Recordings, "local/{recordingId}"), arguments = idArgument) { entry ->
        RecordingLocalScreen(entry.arguments?.getString("recordingId").orEmpty(), onBack = { navigator.back() })
    }
    val captureRoute = Routes.child(AppSection.Recordings, "capture")
    composable(captureRoute, deepLinks = Routes.deepLinks(captureRoute)) { RecordingCaptureScreen(onBack = { navigator.back() }) }
    composable(Routes.child(AppSection.Recordings, "share/{recordingId}"), arguments = idArgument) { entry ->
        RecordingShareScreen(entry.arguments?.getString("recordingId").orEmpty(), onBack = { navigator.back() })
    }
    composable(Routes.child(AppSection.Recordings, "shared")) { SharedRecordingsScreen(onBack = { navigator.back() }) }
    composable(Routes.child(AppSection.Recordings, "shared/{recordingId}"), arguments = idArgument) { entry ->
        SharedRecordingScreen(entry.arguments?.getString("recordingId").orEmpty(), onBack = { navigator.back() })
    }
}

/** What Recordings plugs into the shell; listed in `Areas`. */
val recordingsArea = Area(
    section = AppSection.Recordings,
    routes = NavGraphBuilder::recordingsRoutes,
    demoAnswerers = recordingsDemoAnswerers,
)
