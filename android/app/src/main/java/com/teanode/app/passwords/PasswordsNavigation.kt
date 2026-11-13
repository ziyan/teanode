package com.teanode.app.passwords

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.passwords.PasswordsItemKind

private const val ITEM_ROUTE = "passwords/item/{itemId}"
private const val EDIT_ROUTE = "passwords/edit/{itemKind}/{itemId}"
private const val VERSIONS_ROUTE = "passwords/versions/{itemId}"
private const val VERSION_ROUTE = "passwords/version/{itemId}/{versionId}"

/** The routes of Passwords: the first screen (which is whatever Passwords is in), setting up, an item, its editor and earlier versions, recently deleted and the settings. */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.passwordsRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Passwords), deepLinks = Routes.deepLinks(Routes.home(AppSection.Passwords))) { PasswordsSection() }
    composable("passwords/setup", deepLinks = Routes.deepLinks("passwords/setup")) { PasswordsSetUpScreen() }
    composable(ITEM_ROUTE, deepLinks = Routes.deepLinks(ITEM_ROUTE)) { entry -> PasswordsItemScreen(entry.arguments?.getString("itemId").orEmpty()) }
    composable(EDIT_ROUTE, deepLinks = Routes.deepLinks(EDIT_ROUTE)) { entry ->
        val kind = PasswordsItemKind.fromRawValue(entry.arguments?.getString("itemKind").orEmpty()) ?: PasswordsItemKind.Login
        val itemId = entry.arguments?.getString("itemId")?.takeIf { id -> id != "new" }
        PasswordsEditorScreen(kind, itemId)
    }
    composable(VERSIONS_ROUTE, deepLinks = Routes.deepLinks(VERSIONS_ROUTE)) { entry -> PasswordsVersionsScreen(entry.arguments?.getString("itemId").orEmpty()) }
    composable(VERSION_ROUTE, deepLinks = Routes.deepLinks(VERSION_ROUTE)) { entry ->
        PasswordsVersionScreen(entry.arguments?.getString("itemId").orEmpty(), entry.arguments?.getString("versionId").orEmpty())
    }
    composable("passwords/deleted", deepLinks = Routes.deepLinks("passwords/deleted")) { PasswordsDeletedScreen() }
    composable("passwords/settings", deepLinks = Routes.deepLinks("passwords/settings")) { PasswordsSettingsScreen() }
}

/** What Passwords plugs into the shell; listed in `Areas`. */
val passwordsArea = Area(
    section = AppSection.Passwords,
    routes = NavGraphBuilder::passwordsRoutes,
    demoAnswerers = passwordsDemoAnswerers,
)
