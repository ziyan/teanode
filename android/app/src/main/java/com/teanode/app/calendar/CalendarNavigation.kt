package com.teanode.app.calendar

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Calendar. Register every screen of this part here, with `Routes.child(AppSection.Calendar, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.calendarRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Calendar), deepLinks = Routes.deepLinks(Routes.home(AppSection.Calendar))) {
        CalendarSection()
    }
}

/** What Calendar plugs into the shell; listed in `Areas`. */
val calendarArea = Area(
    section = AppSection.Calendar,
    routes = NavGraphBuilder::calendarRoutes,
    demoAnswerers = calendarDemoAnswerers,
)
