package com.teanode.app.feed

import android.net.Uri
import androidx.navigation.NavGraphBuilder
import androidx.navigation.compose.composable
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection

/** The routes of Feed. */
object FeedRoutes {
    const val STACK = "feed/stack/{feedStackKey}"

    /** One stack's cards; the key holds colons and is written as the address needs it. */
    fun stack(feedStackKey: String): String = "feed/stack/${Uri.encode(feedStackKey)}"

}

/** The routes of Feed: the feed and one stack. */
fun NavGraphBuilder.feedRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Feed), deepLinks = Routes.deepLinks(Routes.home(AppSection.Feed))) {
        FeedSection()
    }
    composable(FeedRoutes.STACK, deepLinks = Routes.deepLinks(FeedRoutes.STACK)) { entry ->
        FeedStackScreen(feedStackKey = entry.arguments?.getString("feedStackKey").orEmpty(), onBack = { navigator.back() })
    }
}

/** What Feed plugs into the shell; listed first in `Areas`. */
val feedArea = Area(
    section = AppSection.Feed,
    routes = NavGraphBuilder::feedRoutes,
    demoAnswerers = feedDemoAnswerers,
)
