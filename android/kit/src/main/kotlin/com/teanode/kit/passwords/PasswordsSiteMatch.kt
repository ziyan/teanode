package com.teanode.kit.passwords

// Deciding which logins belong to a page. Autofill gives a domain or a URL and the person's logins carry the sites they
// typed, with or without a scheme. The rule is the host and nothing looser: `example.com` and `www.example.com` are the
// same site, `login.example.com` is not, and neither is `example.com.evil.test`. A login for a parent domain is offered
// below the exact ones (RelatedHost), never as a first choice, so a page cannot be handed a password just by sitting
// under a domain the person signed in to. Where the page's full address is known (a URL, not a bare domain), the port and
// the scheme count as well ([PasswordsSiteMatch.isOrigin]), so a password saved for a secure site is never offered to
// its plain-text twin or to another service on the same host.

/** How well a login's site fits the requested page. */
enum class PasswordsSiteMatchLevel {
    /** Nothing in common. */
    None,

    /** One host is a subdomain of the other (`login.example.com` and `example.com`). */
    RelatedHost,

    /** The same host, ignoring a leading `www.`, the scheme, the port and the path. */
    ExactHost,
}

/**
 * Compares sites. Every search is over code points, as the iPhone's is over unicode scalars: a combining mark after a "/"
 * or an "@" must not hide it, as it does not for the Go rule that reads bytes.
 */
object PasswordsSiteMatch {
    private fun codePoints(text: String): List<Int> = text.codePoints().toArray().toList()

    private fun text(points: List<Int>): String {
        val builder = StringBuilder()
        for (point in points) builder.appendCodePoint(point)
        return builder.toString()
    }

    private fun isWhitespace(point: Int): Boolean = Character.isWhitespace(point) || Character.isSpaceChar(point)

    private fun trimmed(site: String): List<Int> {
        val points = codePoints(site)
        var start = 0
        var end = points.size
        while (start < end && isWhitespace(points[start])) start++
        while (end > start && isWhitespace(points[end - 1])) end--
        return points.subList(start, end)
    }

    private fun hasPrefix(points: List<Int>, prefix: String): Boolean {
        val expected = codePoints(prefix)
        return points.size >= expected.size && points.subList(0, expected.size) == expected
    }

    private fun hasSuffix(value: String, suffix: String): Boolean {
        val points = codePoints(value)
        val expected = codePoints(suffix)
        return points.size >= expected.size && points.subList(points.size - expected.size, points.size) == expected
    }

    /**
     * The host of a site or a requested page, lower case, without a leading `www.`, a trailing dot, a port, a path or a
     * user name. A site typed without a scheme (`example.com/sign-in`) is read as if it had one. Null when there is no
     * host, the scheme is not http or https, or the address holds a backslash or a control character. It follows
     * `NormalizedHost` in internal/passwords/sitematch.go, and both pass the cases in
     * internal/passwords/testdata/sitematch.json.
     */
    fun normalizedHost(site: String): String? {
        var points = trimmed(site)
        if (points.any { point -> point == '\\'.code || point < 0x20 || point == 0x7f }) return null
        val split = splitScheme(points)
        if (split != null) {
            val scheme = split.first.lowercase()
            if (scheme != "http" && scheme != "https") return null
            points = split.second
        } else if (hasPrefix(points, "//")) {
            points = points.drop(2)
        } else if (isSchemeWithoutSlashes(points)) {
            // "javascript:", "mailto:", "about:blank": not a host and a port.
            return null
        }
        val scalars = authority(points)
        if (scalars.firstOrNull() == '['.code) {
            // An IPv6 address: the brackets and everything between them.
            val closing = scalars.indexOf(']'.code)
            if (closing < 0) return null
            return text(scalars.subList(0, closing + 1)).lowercase()
        }
        var hostPoints = scalars
        val colon = scalars.lastIndexOf(':'.code)
        if (colon >= 0) {
            if (!scalars.subList(colon + 1, scalars.size).all { point -> point in 48..57 }) return null
            hostPoints = scalars.subList(0, colon)
        }
        var host = codePoints(text(hostPoints).lowercase())
        while (host.lastOrNull() == '.'.code) host = host.dropLast(1)
        if (hasPrefix(host, "www.") && host.drop(4).contains('.'.code)) host = host.drop(4)
        if (host.isEmpty() || host.any { point -> point == 0x20 || point == 0x09 || point == 0x0D || point == 0x0A }) return null
        return text(host)
    }

    /** The part of an address after its scheme that holds a host: up to the first "/", "?" or "#", and after the last "@". */
    private fun authority(points: List<Int>): List<Int> {
        var result = points
        val end = result.indexOfFirst { point -> point == '/'.code || point == '?'.code || point == '#'.code }
        if (end >= 0) result = result.subList(0, end)
        val atSign = result.lastIndexOf('@'.code)
        if (atSign >= 0) result = result.subList(atSign + 1, result.size)
        return result
    }

    /** Whether two pieces of text are the same code points. Canonically equivalent text is not the same, as for the Go rule. */
    private fun isSameText(first: String, second: String): Boolean = codePoints(first) == codePoints(second)

    /** Splits `scheme://rest` when the text starts with a scheme followed by two slashes. A `://` further on is not a scheme. */
    private fun splitScheme(points: List<Int>): Pair<String, List<Int>>? {
        val separator = codePoints("://")
        if (points.size < separator.size) return null
        for (schemeEnd in 0..points.size - separator.size) {
            if (points.subList(schemeEnd, schemeEnd + separator.size) != separator) continue
            // Only the first "://" counts, and a scheme cannot be empty.
            if (schemeEnd == 0) return null
            val scheme = text(points.subList(0, schemeEnd))
            if (!isSchemeToken(scheme)) return null
            return scheme to points.subList(schemeEnd + separator.size, points.size)
        }
        return null
    }

    /** A letter followed by letters, digits, `+`, `-` and `.`. */
    private fun isSchemeToken(value: String): Boolean {
        if (value.isEmpty()) return false
        value.toByteArray(Charsets.UTF_8).forEachIndexed { index, byte ->
            val code = byte.toInt() and 0xFF
            val isLetter = code in 65..90 || code in 97..122
            val isOther = code in 48..57 || code == 43 || code == 45 || code == 46
            if (!isLetter && (index == 0 || !isOther)) return false
        }
        return true
    }

    /** A scheme and a colon that is not a host and a port: `mailto:ada@example.com` is, `localhost:8080/sign-in` is not. */
    private fun isSchemeWithoutSlashes(points: List<Int>): Boolean {
        val colon = points.indexOf(':'.code)
        if (colon <= 0 || !isSchemeToken(text(points.subList(0, colon)))) return false
        val rest = text(points.subList(colon + 1, points.size)).toByteArray(Charsets.UTF_8)
        var digitsEnd = 0
        while (digitsEnd < rest.size && rest[digitsEnd] in 48..57) digitsEnd++
        if (digitsEnd > 0 && (digitsEnd == rest.size || listOf('/'.code, '?'.code, '#'.code).contains(rest[digitsEnd].toInt()))) return false
        return true
    }

    /** How well one site fits the requested page. */
    fun level(site: String, requested: String): PasswordsSiteMatchLevel {
        val siteHost = normalizedHost(site) ?: return PasswordsSiteMatchLevel.None
        val requestedHost = normalizedHost(requested) ?: return PasswordsSiteMatchLevel.None
        if (isSameText(siteHost, requestedHost)) return PasswordsSiteMatchLevel.ExactHost
        // A dot-less name (localhost, an intranet host) has no parent domain to share.
        if (siteHost.contains('.') && requestedHost.contains('.') &&
            (hasSuffix(requestedHost, ".$siteHost") || hasSuffix(siteHost, ".$requestedHost"))
        ) {
            return PasswordsSiteMatchLevel.RelatedHost
        }
        return PasswordsSiteMatchLevel.None
    }

    /** The best level any of the sites reaches. */
    fun level(sites: List<String>, requested: String): PasswordsSiteMatchLevel =
        sites.map { site -> level(site, requested) }.maxOrNull() ?: PasswordsSiteMatchLevel.None

    /**
     * How well one site fits a requested page. With `isPageAddress` (the request is a full address, not a bare domain)
     * the same host is an exact host only when the origin is the same too; the same host on another port, or a secure site
     * asked for by a plain-text page, is none. A related host is the same either way.
     */
    fun level(site: String, requested: String, isPageAddress: Boolean): PasswordsSiteMatchLevel {
        val hostLevel = level(site, requested)
        if (!isPageAddress || hostLevel != PasswordsSiteMatchLevel.ExactHost) return hostLevel
        return if (isOrigin(listOf(site), requested)) PasswordsSiteMatchLevel.ExactHost else PasswordsSiteMatchLevel.None
    }

    /** The best level any of the sites reaches for a requested page. */
    fun level(sites: List<String>, requested: String, isPageAddress: Boolean): PasswordsSiteMatchLevel =
        sites.map { site -> level(site, requested, isPageAddress) }.maxOrNull() ?: PasswordsSiteMatchLevel.None

    /**
     * Whether the page is at the origin of one of the sites, which is stricter than a host match and is what a full
     * address needs before a password is offered for it: the host is the same, and so is the port. A site that names a
     * port matches only a page on that port; one that names none matches only a page on the default port of the site's
     * scheme (443 for a site saved without a scheme or as https). A page on http matches only a site saved as http. It
     * follows `IsOriginOfSites` in internal/passwords/sitematch.go.
     */
    fun isOrigin(sites: List<String>, requested: String): Boolean {
        val requestedHost = normalizedHost(requested) ?: return false
        val requestedScheme = scheme(requested)
        val requestedPort = effectivePort(requested)
        for (site in sites) {
            val siteHost = normalizedHost(site) ?: continue
            if (!isSameText(siteHost, requestedHost)) continue
            val siteScheme = scheme(site)
            if (requestedScheme == "http" && siteScheme != "http") continue
            val sitePort = explicitPort(site) ?: defaultPort(siteScheme)
            if (sitePort == requestedPort) return true
        }
        return false
    }

    /** The port a page is on: the one its address names, or the default of its scheme. Zero when the address names a port that is not 1 to 65535. */
    internal fun effectivePort(page: String): Int = explicitPort(page) ?: defaultPort(scheme(page))

    /** "http" when the address says so and "https" otherwise, which is what a site typed without a scheme means. */
    internal fun scheme(site: String): String {
        val split = splitScheme(trimmed(site))
        return if (split != null && split.first.lowercase() == "http") "http" else "https"
    }

    internal fun defaultPort(scheme: String): Int = if (scheme == "http") 80 else 443

    /** The port an address names; null when it names none. A port that is not a number from 1 to 65535 is named, and is zero. */
    internal fun explicitPort(site: String): Int? {
        var points = trimmed(site)
        val split = splitScheme(points)
        if (split != null) points = split.second else if (hasPrefix(points, "//")) points = points.drop(2)
        var scalars = authority(points)
        if (scalars.firstOrNull() == '['.code) {
            val closing = scalars.indexOf(']'.code)
            if (closing < 0 || closing + 1 >= scalars.size || scalars[closing + 1] != ':'.code) return null
            scalars = scalars.subList(closing + 1, scalars.size)
        }
        val colon = scalars.lastIndexOf(':'.code)
        if (colon < 0) return null
        val digits = scalars.subList(colon + 1, scalars.size)
        if (digits.isEmpty()) return null
        var port = 0
        for (point in digits) {
            if (point !in 48..57 || port > 65535) return 0
            port = port * 10 + (point - 48)
        }
        return if (port in 1..65535) port else 0
    }

    /** Whether a login is for the requested page: one of its sites has the same host. */
    fun isMatch(sites: List<String>, requested: String): Boolean = level(sites, requested) == PasswordsSiteMatchLevel.ExactHost

    /** Whether a login is for the requested page, or for a domain above or below it, for the "more logins" list under the exact ones. */
    fun isRelatedOrBetter(sites: List<String>, requested: String): Boolean = level(sites, requested) >= PasswordsSiteMatchLevel.RelatedHost

    /**
     * Logins sorted for the requested page: the exact host first (the exact origin when `isPageAddress`), then the related
     * hosts, then (when `isKeepingOthers`) the rest. Within a level they keep the order they came in.
     */
    fun <Login> sorted(
        logins: List<Login>,
        requested: String,
        isPageAddress: Boolean = false,
        isKeepingOthers: Boolean = false,
        sitesOf: (Login) -> List<String>,
    ): List<Login> {
        val ranked = logins.mapIndexed { index, login -> Triple(index, login, level(sitesOf(login), requested, isPageAddress)) }
        return ranked.filter { entry -> isKeepingOthers || entry.third != PasswordsSiteMatchLevel.None }
            .sortedWith(compareByDescending<Triple<Int, Login, PasswordsSiteMatchLevel>> { entry -> entry.third }.thenBy { entry -> entry.first })
            .map { entry -> entry.second }
    }
}

/** What the system's credential store keeps about one login on one site, so it can be suggested. No secret. */
data class PasswordsPasswordIdentity(
    /** The item's id, handed back to the autofill service when the person picks it. */
    val itemId: String,
    /** The site's host (a domain identifier). */
    val serviceDomain: String,
    val userName: String,
)

/** What the credential store keeps about one passkey. */
data class PasswordsPasskeyIdentity(
    val itemId: String,
    val relyingPartyId: String,
    val userName: String,
    val credentialId: ByteArray,
    val userHandle: ByteArray,
) {
    override fun equals(other: Any?): Boolean =
        other is PasswordsPasskeyIdentity && itemId == other.itemId && relyingPartyId == other.relyingPartyId && userName == other.userName &&
            credentialId.contentEquals(other.credentialId) && userHandle.contentEquals(other.userHandle)

    override fun hashCode(): Int = itemId.hashCode() * 31 + credentialId.contentHashCode()
}

/** Everything one login adds to the credential store. */
data class PasswordsIdentityRecords(
    val passwordIdentities: List<PasswordsPasswordIdentity> = emptyList(),
    val passkeyIdentities: List<PasswordsPasskeyIdentity> = emptyList(),
) {
    companion object {
        /**
         * The records of one login: a password identity for each different host among its sites (only when it has a
         * password), and an identity for each passkey that reads back. The record id is the item's id.
         */
        fun records(itemId: String, login: PasswordsLoginFields): PasswordsIdentityRecords {
            val passwordIdentities = mutableListOf<PasswordsPasswordIdentity>()
            if (login.password.isNotEmpty()) {
                val seenHosts = mutableSetOf<String>()
                for (site in login.sites) {
                    val host = PasswordsSiteMatch.normalizedHost(site) ?: continue
                    if (seenHosts.add(host)) passwordIdentities.add(PasswordsPasswordIdentity(itemId, host, login.userName))
                }
            }
            val passkeyIdentities = mutableListOf<PasswordsPasskeyIdentity>()
            for (passkey in login.passkeys) {
                if (passkey.relyingPartyId.isEmpty()) continue
                val credentialId = PasswordsBase64Url.decode(passkey.credentialId)
                val userHandle = PasswordsBase64Url.decode(passkey.userHandle)
                if (credentialId == null || credentialId.isEmpty() || userHandle == null) continue
                passkeyIdentities.add(
                    PasswordsPasskeyIdentity(itemId, passkey.relyingPartyId, passkey.userName.ifEmpty { login.userName }, credentialId, userHandle),
                )
            }
            return PasswordsIdentityRecords(passwordIdentities, passkeyIdentities)
        }

        /** The records of every login among opened items. Deleted items and notes and cards add nothing. */
        fun records(items: List<PasswordItem>): PasswordsIdentityRecords {
            val passwordIdentities = mutableListOf<PasswordsPasswordIdentity>()
            val passkeyIdentities = mutableListOf<PasswordsPasskeyIdentity>()
            for (item in items) {
                if (item.deletedAtEpochMS != null) continue
                val login = (item.fields as? PasswordsItemFields.Login)?.fields ?: continue
                val own = records(item.id, login)
                passwordIdentities += own.passwordIdentities
                passkeyIdentities += own.passkeyIdentities
            }
            return PasswordsIdentityRecords(passwordIdentities, passkeyIdentities)
        }
    }
}

/** Reads the unpadded base64url a website sends credential ids and user handles in. */
internal object PasswordsBase64Url {
    fun decode(text: String): ByteArray? {
        var standard = text.replace('-', '+').replace('_', '/')
        while (standard.length % 4 != 0) standard += "="
        return runCatching { java.util.Base64.getDecoder().decode(standard) }.getOrNull()
    }

    fun encode(bytes: ByteArray): String = java.util.Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
}
