package com.teanode.kit.chats

import java.net.URLDecoder

/**
 * LinkClean shortens a link for people: the tracking parameters taken out of its query string, and the short text
 * shown for a link that has no words of its own.
 *
 * It is the same work as internal/util/linkclean on the server and linkClean.ts in the dashboard, done on the link's
 * text rather than on a parsed URL, because each language's URL parser normalizes differently (host case, percent
 * escapes, dot segments). A change here is a change there.
 */
object LinkClean {
    /** The longest text shown for a link, in characters. */
    const val DISPLAY_LENGTH = 48

    /** The longest cleaned query, question mark included, that is shown with the link. A longer one is left out. */
    const val DISPLAY_QUERY_LENGTH = 32

    /** Query parameters that say where a visitor came from and nothing about what the page is. utm_ is a family, matched by prefix. */
    private val trackingParameters = setOf(
        "gclid", "gclsrc", "gbraid", "wbraid", "dclid", "fbclid", "msclkid", "yclid", "twclid", "ttclid",
        "li_fat_id", "igshid", "mc_cid", "mc_eid", "_gl", "_ga", "_hsenc", "_hsmi", "mkt_tok",
        "oly_anon_id", "oly_enc_id", "vero_id", "rb_clickid", "s_cid", "ref_src",
    )

    /** The parts of a web link, each as written. query and fragment keep their question mark and number sign, and are empty when the link has none. */
    private class Parts(val scheme: String, val authority: String, val path: String, val query: String, val fragment: String)

    private fun partsOf(link: String): Parts? {
        val schemeLength = when {
            link.take(8).lowercase() == "https://" -> 5
            link.take(7).lowercase() == "http://" -> 4
            else -> return null
        }
        val scheme = link.substring(0, schemeLength)
        val afterScheme = link.substring(schemeLength + 3)
        val authorityEnd = afterScheme.indexOfFirst { character -> character == '/' || character == '?' || character == '#' }.let { found -> if (found < 0) afterScheme.length else found }
        val authority = afterScheme.substring(0, authorityEnd)
        val afterAuthority = afterScheme.substring(authorityEnd)
        val pathEnd = afterAuthority.indexOfFirst { character -> character == '?' || character == '#' }.let { found -> if (found < 0) afterAuthority.length else found }
        val path = afterAuthority.substring(0, pathEnd)
        val afterPath = afterAuthority.substring(pathEnd)
        return if (afterPath.startsWith("?")) {
            val queryEnd = afterPath.indexOf('#').let { found -> if (found < 0) afterPath.length else found }
            Parts(scheme, authority, path, afterPath.substring(0, queryEnd), afterPath.substring(queryEnd))
        } else {
            Parts(scheme, authority, path, "", afterPath)
        }
    }

    private fun isTracking(parameter: String): Boolean {
        val name = parameter.substringBefore('=').lowercase()
        return name in trackingParameters || name.startsWith("utm_")
    }

    /**
     * The link without its tracking parameters. Every other parameter stays, as written and in its order. Anything
     * that is not an http or https link, and a link with nothing to remove, comes back exactly as given.
     */
    fun cleaned(link: String): String {
        val parts = partsOf(link) ?: return link
        if (parts.query.isEmpty()) return link
        val parameters = parts.query.drop(1).split("&")
        if (parameters.none { parameter -> isTracking(parameter) }) return link
        // Dropping a tracking parameter rewrites the query, and the empty parameters it leaves (a doubled ampersand) go with it.
        val kept = parameters.filter { parameter -> parameter.isNotEmpty() && !isTracking(parameter) }
        val query = if (kept.isEmpty()) "" else "?" + kept.joinToString("&")
        return parts.scheme + "://" + parts.authority + parts.path + query + parts.fragment
    }

    /**
     * The short text for a link that has no words of its own: the host without "www.", the path, and the cleaned
     * query only when it is short. No scheme, no credentials, no fragment; past `DISPLAY_LENGTH` characters the middle
     * is cut and replaced with an ellipsis.
     */
    fun displayText(link: String): String {
        val parts = partsOf(cleaned(link)) ?: return shortened(link)
        var host = parts.authority.lowercase()
        host.lastIndexOf('@').let { at -> if (at >= 0) host = host.substring(at + 1) }
        if (host.startsWith("www.")) host = host.drop(4)
        // Percent escapes are shown as the characters they stand for; text that is not valid percent-encoding is shown as written.
        val path = try {
            URLDecoder.decode(parts.path.replace("+", "%2B"), Charsets.UTF_8)
        } catch (_: Exception) {
            parts.path
        }
        var query = parts.query
        if (query.codePointCount(0, query.length) > DISPLAY_QUERY_LENGTH) query = ""
        val shownPath = if (path == "/" && query.isEmpty()) "" else path
        return shortened(host + shownPath + query)
    }

    /**
     * The text with the middle cut out when it is longer than `DISPLAY_LENGTH` characters, so that both the start,
     * which says where the link goes, and the end, which says what it is, stay. Characters are Unicode scalars, as on
     * the server.
     */
    private fun shortened(text: String): String {
        val characters = text.codePoints().toArray()
        if (characters.size <= DISPLAY_LENGTH) return text
        val headLength = DISPLAY_LENGTH / 2
        val tailLength = DISPLAY_LENGTH - headLength - 1
        val builder = StringBuilder()
        characters.take(headLength).forEach { codePoint -> builder.appendCodePoint(codePoint) }
        builder.append('…')
        characters.takeLast(tailLength).forEach { codePoint -> builder.appendCodePoint(codePoint) }
        return builder.toString()
    }
}
