package com.teanode.kit

import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/**
 * ConnectLink is what a connect code holds: the link the dashboard, the command line or the agent shows as a QR code
 * to connect a phone, `https://teanode.com/app/connect#server=<address>&code=<secret>`.
 *
 * Scanned by the phone's camera it opens the app, which claims the address. The server and the one-time secret ride
 * after the "#", which a browser never sends, so neither reaches the project's web server.
 */
data class ConnectLink(
    /** The server the link connects to. */
    val server: ServerAddress,
    /**
     * The one-time secret the app exchanges for its sign-in, when the link carries one. A link with only a server
     * leaves signing in to the person.
     */
    val connectCode: String? = null,
) {
    /** The link, as the server writes it. */
    val url: String
        get() {
            val fragment = StringBuilder("server=").append(QueryString.encode(server.toString()))
            if (connectCode != null) fragment.append("&code=").append(QueryString.encode(connectCode))
            return "https://$HOST$PATH#$fragment"
        }

    companion object {
        /** The host and path the link is served from. The app claims them, so Android opens the app for them. */
        const val HOST = "teanode.com"
        const val PATH = "/app/connect"

        /** The connect link a text is, or null when it is not one. */
        fun parse(link: String): ConnectLink? {
            val parsed = link.trim().toHttpUrlOrNull() ?: return null
            if (parsed.scheme != "https" || parsed.host != HOST || parsed.encodedPath != PATH) return null
            val fragment = parsed.encodedFragment ?: return null
            val items = QueryString.parse(fragment)
            val written = items["server"] ?: return null
            val server = ServerAddress.parseOrNull(written) ?: return null
            val code = items["code"]
            return ConnectLink(server, if (code.isNullOrEmpty()) null else code)
        }
    }
}
