package com.teanode.kit.chats

import com.teanode.kit.ServerAddress
import com.teanode.kit.recordings.RecordingLink
import java.time.LocalDate

/** AppLinkTarget is where a link or a dashboard path inside the app goes (ios AppModel.route and show(path:)). */
sealed class AppLinkTarget {
    /** A web page, which opens in the app's own browser. */
    data class WebPage(val address: String) : AppLinkTarget()

    /** Another app's or the system's: mailto:, tel:, a map, this app's own universal links. */
    data class System(val address: String) : AppLinkTarget()

    data class Recording(val recordingLink: RecordingLink) : AppLinkTarget()

    data class MailItem(val itemId: String) : AppLinkTarget()

    /** A page of the person's own memory. */
    data class MemoryPage(val pagePath: String) : AppLinkTarget()

    /** A page another person shared, named by their username. */
    data class SharedMemoryPage(val ownerUsername: String, val pagePath: String) : AppLinkTarget()

    /** The Location page, at a day when the address names one. */
    data class Location(val day: LocalDate?) : AppLinkTarget()

    /** The Health page: the chart of a kind of data, the settings, or the part alone. */
    data class Health(val healthType: String?, val isSettings: Boolean) : AppLinkTarget()

    /** Knowledge, at a page when the address names one. */
    data class Knowledge(val pagePath: String) : AppLinkTarget()

    /** Mail, at a conversation when the address names one. */
    data class Mailbox(val itemId: String?) : AppLinkTarget()

    /** Finance, at a part ("networth", "savingstargets": its name in lowercase, no dashes) when the address names one. */
    data class Finance(val partName: String?) : AppLinkTarget()

    /** The Journal, at a day (written 2026-03-14) when the address names one; empty for the list. */
    data class Journal(val day: String) : AppLinkTarget()

    data object Passwords : AppLinkTarget()

    data object Shopping : AppLinkTarget()

    /** Settings; [isMailPrograms] opens the mail programs of the mailbox's devices. */
    data class Settings(val isMailPrograms: Boolean) : AppLinkTarget()

    /** The connect page: the phone is the one connected already. */
    data object Connect : AppLinkTarget()

    /** A link the app does not follow, as the dashboard does not follow it. */
    data object Discarded : AppLinkTarget()
}

/**
 * AppLinks decides where a link in an answer, or a path the agent opens, goes. It is the iPhone app's `AppModel.route`
 * (links in an answer) and `AppModel.show(path:)` (a dashboard path), kept here so they are tested without a screen.
 */
object AppLinks {
    private val dayPattern = Regex("""^\d{4}-\d{2}-\d{2}$""")
    private const val MAXIMUM_PATH_LENGTH = 600

    /** Whether a text is a day written 2026-03-14 that exists. */
    fun isDayText(text: String): Boolean = dayPattern.matches(text) && runCatching { LocalDate.parse(text) }.isSuccess

    /**
     * Where a link tapped in an answer goes. Web links open in the app's browser; a recording this server's own link
     * names, a mail item and a memory page open in the app; anything else is not followed.
     */
    fun forLink(address: String, server: ServerAddress?): AppLinkTarget {
        recordingOf(address, server)?.let { link -> return AppLinkTarget.Recording(link) }
        val scheme = address.substringBefore(':', "").lowercase()
        return when (scheme) {
            "http", "https" -> when (LinkOpening.way(address)) {
                LinkOpening.Way.InApp -> AppLinkTarget.WebPage(address)
                LinkOpening.Way.System -> AppLinkTarget.System(address)
            }
            "mail" -> {
                val itemId = address.drop("mail:".length)
                if (itemId.isNotEmpty() && itemId.all { character -> character.isLetterOrDigit() }) AppLinkTarget.MailItem(itemId) else AppLinkTarget.Discarded
            }
            "memory" -> memoryTarget(address)
            else -> AppLinkTarget.Discarded
        }
    }

    /** `memory:PATH#N@username` is a page another person shared, with the username last; without it the page is the person's own. */
    private fun memoryTarget(address: String): AppLinkTarget {
        val target = address.drop("memory:".length).split('@', limit = 2)
        val pagePath = target.first().substringBefore('#')
        val segments = pagePath.split('/').filter { segment -> segment.isNotEmpty() }
        if (segments.isEmpty() || segments.size > 8) return AppLinkTarget.Discarded
        if (!segments.all { segment -> segment.all { character -> character.isLowerCase() || character.isDigit() || character == '-' } }) return AppLinkTarget.Discarded
        if (target.size > 1) {
            val owner = target[1]
            if (owner.isEmpty() || !owner.all { character -> character.isLetterOrDigit() || character in "._~-" }) return AppLinkTarget.Discarded
            return AppLinkTarget.SharedMemoryPage(owner, pagePath)
        }
        return AppLinkTarget.MemoryPage(pagePath)
    }

    /** The recording a link names, when it is this server's own, or a bare path. */
    fun recordingOf(address: String, server: ServerAddress?): RecordingLink? {
        if (address.length > 4096) return null
        if (address.contains("://")) {
            val origin = ServerAddress.parseOrNull(address) ?: return null
            if (origin != server) return null
            val rest = address.substringAfter("://").let { afterScheme -> if (afterScheme.contains('/')) "/" + afterScheme.substringAfter('/') else "" }
            return RecordingLink.parse(rest.substringBefore('#'))
        }
        return if (address.startsWith("/")) RecordingLink.parse(address.substringBefore('#')) else null
    }

    /** The Location page's address, `/location`, with the day it may name (`?day=2030-01-15`); null for any other address. */
    fun locationOf(written: String): AppLinkTarget.Location? {
        val pathText = written.substringBefore('?')
        if (pathText != "/location" && pathText != "/location/") return null
        if (!written.contains('?')) return AppLinkTarget.Location(null)
        for (pair in written.substringAfter('?').split('&')) {
            if (pair.substringBefore('=') != "day" || !pair.contains('=')) continue
            val text = pair.substringAfter('=')
            if (isDayText(text)) return AppLinkTarget.Location(LocalDate.parse(text))
        }
        return AppLinkTarget.Location(null)
    }

    /** The Health page's address, `/health`, with the kind of data it may name (`?type=stepCount`) or the settings (`?tab=settings`); null for any other address. */
    fun healthOf(written: String): AppLinkTarget.Health? {
        val pathText = written.substringBefore('?')
        if (pathText != "/health" && pathText != "/health/") return null
        var link = AppLinkTarget.Health(healthType = null, isSettings = false)
        if (!written.contains('?')) return link
        for (pair in written.substringAfter('?').split('&')) {
            if (!pair.contains('=')) continue
            val key = pair.substringBefore('=')
            val text = pair.substringAfter('=')
            if (key == "tab" && text == "settings") link = AppLinkTarget.Health(null, isSettings = true)
            // A kind of data is its name: letters and digits.
            if (key == "type" && text.isNotEmpty() && text.length <= 96 && text.all { character -> character.isLetterOrDigit() }) link = AppLinkTarget.Health(text, isSettings = false)
        }
        return link
    }

    /**
     * Where a dashboard path goes, checked as the dashboard checks it: the person's own pages, in plain segments.
     * `/recordings/<id>` of this server and the Location and Health pages are read first, as they carry a query.
     */
    fun forPath(written: String, server: ServerAddress?): AppLinkTarget {
        recordingOf(written, server)?.let { link -> return AppLinkTarget.Recording(link) }
        locationOf(written)?.let { target -> return target }
        healthOf(written)?.let { target -> return target }
        val path = if (written.length > 1 && written.endsWith("/")) written.dropLast(1) else written
        if (!path.startsWith("/") || path.length > MAXIMUM_PATH_LENGTH) return AppLinkTarget.Discarded
        val segments = path.drop(1).split('/')
        val isPlain = segments.all { segment ->
            segment.isNotEmpty() && segment != "." && segment != ".." && segment.all { character -> character.isLetterOrDigit() || character in "-._~" }
        }
        if (!isPlain) return AppLinkTarget.Discarded
        return when (segments.first()) {
            "knowledge" -> AppLinkTarget.Knowledge(segments.drop(1).joinToString("/"))
            "mailbox" -> if (segments.size >= 3 && segments[1] != "compose" && segments[1] != "subscriptions") AppLinkTarget.Mailbox(segments[2]) else AppLinkTarget.Mailbox(null)
            "finance" -> {
                if (path == "/finance/link") return AppLinkTarget.Discarded
                // The dashboard writes the parts with dashes: net-worth.
                AppLinkTarget.Finance(segments.getOrNull(1)?.replace("-", "")?.lowercase())
            }
            // /journal/YYYY-MM-DD is a day; the pages of a week, a month and a year open the list.
            "journal" -> AppLinkTarget.Journal(if (segments.size == 2 && isDayText(segments[1])) segments[1] else "")
            // Only the part: an item's address would say which item, and an address is no reason to open one.
            "passwords" -> AppLinkTarget.Passwords
            "shopping" -> AppLinkTarget.Shopping
            "settings" -> AppLinkTarget.Settings(isMailPrograms = segments.size >= 3 && segments[1] == "mailbox" && segments[2] == "devices")
            "connect" -> AppLinkTarget.Connect
            else -> AppLinkTarget.Discarded
        }
    }
}
