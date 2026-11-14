package com.teanode.app.shell

import androidx.navigation.NavDeepLink
import androidx.navigation.navDeepLink
import com.teanode.kit.AppSection

/**
 * Routes names every place of the app. A part's places all start with its name: `agent/home` is the Chats list,
 * `mail/thread/{threadId}` a mail thread. [home] is the part's first screen and the one the bottom bar and the drawer
 * open; everything else is a [child] of it. A route is also its deep link: `teanode://app/<route>`.
 */
object Routes {
    /** The graph holding all of one part's places. */
    fun graph(section: AppSection): String = section.routeName

    /** The part's first screen. */
    fun home(section: AppSection): String = "${section.routeName}/home"

    /** A place inside a part, such as `child(AppSection.Mail, "thread/{threadId}")`. */
    fun child(section: AppSection, path: String): String = "${section.routeName}/$path"

    /** The search screen, which is not a part but sits in the bottom bar. */
    const val SEARCH = "search/home"

    /** The deep links of a route: `teanode://app/<route>`, with its arguments in the path. */
    fun deepLinks(route: String): List<NavDeepLink> = listOf(navDeepLink { uriPattern = "$DEEP_LINK_PREFIX/$route" })

    /** The address that opens a route, with its arguments filled in. */
    fun deepLink(route: String): String = "$DEEP_LINK_PREFIX/$route"

    /** Whether a route is a part's first screen, where the bottom bar shows. */
    fun isHome(route: String?): Boolean = route != null && (route == SEARCH || route.endsWith("/home") && route.count { character -> character == '/' } == 1)

    /** The part a route belongs to, when it is in one. */
    fun sectionOf(route: String?): AppSection? = route?.substringBefore('/')?.let { name -> AppSection.fromRouteName(name) }

    const val DEEP_LINK_PREFIX = "teanode://app"
}
