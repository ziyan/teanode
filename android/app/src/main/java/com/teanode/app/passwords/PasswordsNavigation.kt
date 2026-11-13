package com.teanode.app.passwords

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Passwords. Register every screen of this part here, with `Routes.child(AppSection.Passwords, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.passwordsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Passwords), deepLinks = Routes.deepLinks(Routes.home(AppSection.Passwords))) {
        PasswordsSection()
    }
}

/** What Passwords plugs into the shell; listed in `Areas`. */
val passwordsArea = Area(
    section = AppSection.Passwords,
    routes = NavGraphBuilder::passwordsRoutes,
    demoAnswerers = passwordsDemoAnswerers,
)
