package com.teanode.app.health

import androidx.compose.foundation.layout.padding
import androidx.compose.ui.Modifier
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Settings
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.AppPlace
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.LoadingState
import com.teanode.kit.AppSection
import androidx.compose.material.icons.outlined.FavoriteBorder

/** The routes of Health: the kinds, one kind's chart and readings, a workout's route, and the settings. */
fun NavGraphBuilder.healthRoutes(navigator: AppNavigator) {
    composable(HealthRoutes.home, deepLinks = Routes.deepLinks(HealthRoutes.home)) { HealthSection() }
    composable(HealthRoutes.settings, deepLinks = Routes.deepLinks(HealthRoutes.settings)) { HealthSettingsScreen(onBack = { navigator.back() }) }
    composable(
        HealthRoutes.type, arguments = listOf(navArgument(HealthRoutes.TYPE_ARGUMENT) { type = NavType.StringType }), deepLinks = Routes.deepLinks(HealthRoutes.type),
    ) { entry ->
        val model = rememberHealthModel()
        val healthType = entry.arguments?.getString(HealthRoutes.TYPE_ARGUMENT).orEmpty()
        val text = rememberHealthText()
        LaunchedEffect(Unit) { if (!model.hasLoaded) model.load() }
        val type = model.types.firstOrNull { each -> each.healthType == healthType }
        when {
            type != null -> HealthTypeScreen(
                model, type, onBack = { navigator.back() },
                onOpenRoute = { sample ->
                    model.routeRequests[sample.healthSampleId] = HealthRouteRequest.of(sample, text)
                    navigator.navigate(HealthRoutes.route(sample.healthSampleId))
                },
            )
            !model.hasLoaded -> LoadingState()
            else -> HealthTypeMissing(onBack = { navigator.back() })
        }
    }
    composable(
        HealthRoutes.route, arguments = listOf(navArgument(HealthRoutes.SAMPLE_ARGUMENT) { type = NavType.StringType }), deepLinks = Routes.deepLinks(HealthRoutes.route),
    ) { entry ->
        val model = rememberHealthModel()
        val sampleId = entry.arguments?.getString(HealthRoutes.SAMPLE_ARGUMENT).orEmpty()
        val text = rememberHealthText()
        val request = model.routeRequests[sampleId] ?: HealthRouteRequest(sampleId, text.workoutName(""), null, 0.0, null, null)
        HealthRouteScreen(LocalConnection.current, request, onBack = { navigator.back() })
    }
}

/** A link to a kind that does not exist, or that was erased or is not shared. */
@Composable
private fun HealthTypeMissing(onBack: () -> Unit) {
    com.teanode.app.ui.components.TeaNodeScreen(title = androidx.compose.ui.res.stringResource(R.string.health_title), onBack = onBack) { padding ->
        com.teanode.app.ui.components.EmptyState(
            Icons.Outlined.FavoriteBorder, androidx.compose.ui.res.stringResource(R.string.health_kind_not_here), Modifier.padding(padding),
            description = androidx.compose.ui.res.stringResource(R.string.health_kind_not_here_description),
        )
    }
}


/** The places Search offers by name. */
private fun healthPlaces(): List<AppPlace> = listOf(AppPlace("health-settings", R.string.health_settings_title, HealthRoutes.settings, Icons.Outlined.Settings, listOf(R.string.health_title)))

/** What Health plugs into the shell; listed in `Areas`. */
val healthArea = Area(
    section = AppSection.Health,
    routes = NavGraphBuilder::healthRoutes,
    places = ::healthPlaces,
    demoAnswerers = healthDemoAnswerers,
)
