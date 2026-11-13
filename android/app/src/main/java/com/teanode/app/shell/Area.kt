package com.teanode.app.shell

import androidx.annotation.StringRes
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.navigation.NavGraphBuilder
import com.teanode.app.demo.DemoAnswerer
import com.teanode.kit.AppSection

/**
 * Area is everything one part of the app plugs into the shell: its routes, the places Search can offer by name, and
 * the answers demo mode gives. Each part has one, in its own package (`chatsArea` in `com.teanode.app.chats`), listed
 * once in [Areas]. A part is built by filling its package; nobody edits the shell.
 */
class Area(
    /** The part these places belong to; null for a screen outside any part, such as Search. */
    val section: AppSection?,
    /** Registers the part's screens; the first is `Routes.home(section)`. */
    val routes: NavGraphBuilder.(navigator: AppNavigator) -> Unit,
    /** The places inside the part that Search offers by name (a mail folder, a Settings page). */
    val places: () -> List<AppPlace> = { emptyList() },
    /** What demo mode answers for this part's GraphQL documents, most specific first. */
    val demoAnswerers: List<DemoAnswerer> = emptyList(),
)

/** AppPlace is a place of the app the Search screen offers by name, as the dashboard's search box offers its pages. */
class AppPlace(
    val id: String,
    @param:StringRes val titleResource: Int,
    /** The route to open. */
    val route: String,
    val icon: ImageVector,
    /** The places it is inside, outermost first, by string resource. */
    @param:StringRes val parentTitleResources: List<Int> = emptyList(),
)
