package com.teanode.app.knowledge

import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.Area
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** The places of Knowledge. A page's path is passed in the `path` query argument, encoded. */
object KnowledgeRoutes {
    val HOME: String = Routes.home(AppSection.Knowledge)
    val PAGE: String = Routes.child(AppSection.Knowledge, "page?path={path}")

    /** The page of memory at a path, such as `people/ada`. */
    fun page(path: String): String = Routes.child(AppSection.Knowledge, "page?path=${android.net.Uri.encode(path)}")
}

/** The routes of Knowledge. */
@Suppress("UNUSED_PARAMETER")
fun NavGraphBuilder.knowledgeRoutes(navigator: AppNavigator) {
    composable(KnowledgeRoutes.HOME, deepLinks = Routes.deepLinks(KnowledgeRoutes.HOME)) { KnowledgeSection() }
}

/** What Knowledge plugs into the shell; listed in `Areas`. */
val knowledgeArea = Area(
    section = AppSection.Knowledge,
    routes = NavGraphBuilder::knowledgeRoutes,
    demoAnswerers = knowledgeDemoAnswerers,
)
