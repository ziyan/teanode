package com.teanode.app.contacts

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/**
 * The routes of Contacts. Register every screen of this part here, with `Routes.child(AppSection.Contacts, "...")`
 * names, and open one from a screen with `LocalAppNavigator.current.navigate(route)`.
 */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.contactsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Contacts), deepLinks = Routes.deepLinks(Routes.home(AppSection.Contacts))) {
        ContactsSection()
    }
}

/** What Contacts plugs into the shell; listed in `Areas`. */
val contactsArea = Area(
    section = AppSection.Contacts,
    routes = NavGraphBuilder::contactsRoutes,
    demoAnswerers = contactsDemoAnswerers,
)
