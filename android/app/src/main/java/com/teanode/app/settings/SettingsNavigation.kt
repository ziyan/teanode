package com.teanode.app.settings

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Settings. Register every screen of this part here, with `Routes.child(AppSection.Settings, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.settingsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Settings), deepLinks = Routes.deepLinks(Routes.home(AppSection.Settings))) {
        SettingsSection()
    }
}

/** What Settings plugs into the shell; listed in `Areas`. */
val settingsArea = Area(
    section = AppSection.Settings,
    routes = NavGraphBuilder::settingsRoutes,
    demoAnswerers = settingsDemoAnswerers,
)
