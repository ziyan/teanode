package com.teanode.app.shell

import androidx.compose.runtime.staticCompositionLocalOf
import androidx.navigation.NavController
import androidx.navigation.NavGraph.Companion.findStartDestination
import com.teanode.kit.AppSection

/**
 * AppNavigator is how a screen goes to another: inside its part by route, to another part, or back. It wraps the one
 * navigation controller of the shell; a screen gets it from [LocalAppNavigator].
 */
class AppNavigator(private val controller: NavController) {
    /** Goes to a route, such as `Routes.child(AppSection.Mail, "thread/$threadId")`. */
    fun navigate(route: String) {
        controller.navigate(route) { launchSingleTop = true }
    }

    /** Opens a part at its first screen, the way a tab does: the screens it had open come back as they were left. */
    fun openSection(section: AppSection) {
        openRoute(Routes.home(section))
    }

    /** Opens the search screen. */
    fun openSearch() = openRoute(Routes.SEARCH)

    private fun openRoute(route: String) {
        controller.navigate(route) {
            popUpTo(controller.graph.findStartDestination().id) { saveState = true }
            launchSingleTop = true
            restoreState = true
        }
    }

    /** Goes back one screen; false when there is none. */
    fun back(): Boolean = controller.popBackStack()

    /** The route on screen now. */
    val currentRoute: String? get() = controller.currentDestination?.route
}

val LocalAppNavigator = staticCompositionLocalOf<AppNavigator> { error("no AppNavigator: draw the screen inside AppShell or DemoHost") }
