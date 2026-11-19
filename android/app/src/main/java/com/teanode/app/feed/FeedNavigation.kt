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
    const val ITEM = "feed/item/{feedItemId}"
    const val STACK = "feed/stack/{feedStackKey}"

    /** One stack's cards; the key holds colons and is written as the address needs it. */
    fun stack(feedStackKey: String): String = "feed/stack/${Uri.encode(feedStackKey)}"

    /** One card, as a pressed notification opens it. */
    fun item(feedItemId: String): String = "feed/item/${Uri.encode(feedItemId)}"
}

/** The routes of Feed: the feed, one stack, and one card by its identifier. */
fun NavGraphBuilder.feedRoutes(navigator: AppNavigator) {
    composable(Routes.home(AppSection.Feed), deepLinks = Routes.deepLinks(Routes.home(AppSection.Feed))) {
        FeedSection()
    }
    composable(FeedRoutes.STACK, deepLinks = Routes.deepLinks(FeedRoutes.STACK)) { entry ->
        FeedStackScreen(feedStackKey = entry.arguments?.getString("feedStackKey").orEmpty(), onBack = { navigator.back() })
    }
    composable(FeedRoutes.ITEM, deepLinks = Routes.deepLinks(FeedRoutes.ITEM)) { entry ->
        FeedItemScreen(feedItemId = entry.arguments?.getString("feedItemId").orEmpty(), onBack = { navigator.back() })
    }
}

/** What Feed plugs into the shell; listed first in `Areas`. */
val feedArea = Area(
    section = AppSection.Feed,
    routes = NavGraphBuilder::feedRoutes,
    demoAnswerers = feedDemoAnswerers,
)
