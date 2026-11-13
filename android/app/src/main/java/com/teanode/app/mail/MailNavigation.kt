package com.teanode.app.mail

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Mail. Register every screen of this part here, with `Routes.child(AppSection.Mail, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.mailRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Mail), deepLinks = Routes.deepLinks(Routes.home(AppSection.Mail))) {
        MailSection()
    }
}

/** What Mail plugs into the shell; listed in `Areas`. */
val mailArea = Area(
    section = AppSection.Mail,
    routes = NavGraphBuilder::mailRoutes,
    demoAnswerers = mailDemoAnswerers,
)
