package com.teanode.app.coding

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

private const val SESSION_ROUTE = "coding/session/{computer}/{paneId}"

/** The routes of Coding: the list, one session, opening a session, the reporting hooks and what a session sees. */
fun NavGraphBuilder.codingRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Coding), deepLinks = Routes.deepLinks(Routes.home(AppSection.Coding))) {
        CodingSection()
    }
    composable(SESSION_ROUTE, deepLinks = Routes.deepLinks(SESSION_ROUTE)) { entry ->
        val computer = entry.arguments?.getString("computer").orEmpty()
        val paneId = entry.arguments?.getString("paneId").orEmpty()
        CodingSessionScreen(initialId = "$computer/$paneId", onBack = { navigator.back() })
    }
    composable("coding/open", deepLinks = Routes.deepLinks("coding/open")) {
        CodingOpenScreen(onBack = { navigator.back() }, onOpened = { computer, paneId ->
            navigator.back()
            navigator.navigate(codingSessionRoute(computer, paneId))
        })
    }
    composable("coding/hooks", deepLinks = Routes.deepLinks("coding/hooks")) { CodingHooksScreen(onBack = { navigator.back() }) }
    composable("coding/preview", deepLinks = Routes.deepLinks("coding/preview")) { CodingPreviewScreen(onBack = { navigator.back() }) }
}

/** What Coding plugs into the shell; listed in `Areas`. */
val codingArea = Area(
    section = AppSection.Coding,
    routes = NavGraphBuilder::codingRoutes,
    demoAnswerers = codingDemoAnswerers,
)
