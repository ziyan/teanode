package com.teanode.kit.chats

import java.net.URI

/**
 * LinkOpening says where a link opened in the app goes: a web page opens in the app's own browser, and everything the
 * system or another app does better opens there.
 */
object LinkOpening {
    enum class Way {
        /** The in-app browser, over whatever is on screen. */
        InApp,

        /** The system: another app, or the browser itself. */
        System,
    }

    /** Hosts whose pages belong to an app of the system. Directions, and a place shared from Maps, open the Maps app, and an app's page opens the store. */
    private val systemHosts = setOf("maps.apple.com", "maps.apple", "apps.apple.com", "maps.google.com", "play.google.com")

    /** Where this app's own links are: teanode.com/app/..., which the system opens in the app when it is installed. */
    private const val APP_PATH_PREFIX = "/app"
    private const val APP_HOST = "teanode.com"

    /**
     * How a link opens. Only an http or https link opens in the app's browser; mailto:, tel:, sms:, teanode:// and any
     * other scheme is the system's, and so are the web pages that something else handles: a map, this app's own links,
     * and a configuration profile.
     */
    fun way(link: String): Way {
        val uri = try {
            URI(link)
        } catch (_: Exception) {
            return Way.System
        }
        val scheme = uri.scheme?.lowercase()
        if (scheme != "http" && scheme != "https") return Way.System
        val host = uri.host?.lowercase()
        if (host.isNullOrEmpty()) return Way.System
        if (host in systemHosts) return Way.System
        val path = uri.path.orEmpty()
        if (host == APP_HOST && (path == APP_PATH_PREFIX || path.startsWith("$APP_PATH_PREFIX/"))) return Way.System
        if (path.lowercase().endsWith(".mobileconfig")) return Way.System
        return Way.InApp
    }
}
