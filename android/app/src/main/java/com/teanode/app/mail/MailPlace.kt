package com.teanode.app.mail

import android.net.Uri

/** MailPlace is a list the mail part can show: a folder, the two views that gather from every folder, or the mailing lists. */
sealed class MailPlace {
    data class Folder(val folderId: String) : MailPlace()
    data object Starred : MailPlace()
    data object Priority : MailPlace()
    data object Subscriptions : MailPlace()

    /** The text a route carries it as. */
    val routeText: String
        get() = when (this) {
            is Folder -> "folder:$folderId"
            Starred -> "starred"
            Priority -> "priority"
            Subscriptions -> "subscriptions"
        }

    companion object {
        fun fromRouteText(text: String?): MailPlace = when {
            text == null -> Subscriptions
            text.startsWith("folder:") -> Folder(text.removePrefix("folder:"))
            text == "starred" -> Starred
            text == "priority" -> Priority
            else -> Subscriptions
        }
    }
}

/** The routes of Mail, built in one place so a screen and the graph agree. */
object MailRoutes {
    const val HOME = "mail/home"
    const val PLACE = "mail/place/{place}"
    const val SEARCH = "mail/search?words={words}"
    const val THREAD = "mail/thread/{itemId}?folderId={folderId}"
    const val COMPOSE = "mail/compose/{kind}?itemId={itemId}&to={to}"
    const val SETTINGS = "mail/settings"
    const val SETTINGS_GENERAL = "mail/settings/general"
    const val SETTINGS_AUTO_REPLY = "mail/settings/auto-reply"
    const val SETTINGS_FOLDERS = "mail/settings/folders"
    const val SETTINGS_PROGRAMS = "mail/settings/programs"
    const val RULES = "mail/rules"
    const val SUBSCRIPTION = "mail/subscription/{key}"
    const val MANAGE = "mail/manage"
    const val MANAGE_ALL_MAIL = "mail/manage/all-mail"
    const val MANAGE_MAIL = "mail/manage/mail/{mailId}"
    const val MANAGE_QUEUE = "mail/manage/queue"
    const val MANAGE_REPORTS = "mail/manage/reports"
    const val MANAGE_REPORT = "mail/manage/report/{reportId}"
    const val MANAGE_DOMAINS = "mail/manage/domains"
    const val MANAGE_DOMAIN = "mail/manage/domain/{domainId}"

    fun place(place: MailPlace): String = "mail/place/${Uri.encode(place.routeText)}"
    fun search(words: String): String = "mail/search?words=${Uri.encode(words)}"
    fun thread(itemId: String, folderId: String?): String = "mail/thread/${Uri.encode(itemId)}" + (folderId?.let { id -> "?folderId=${Uri.encode(id)}" } ?: "")
    fun compose(kind: ComposeKind, itemId: String? = null, to: String? = null): String =
        "mail/compose/${kind.routeName}" + listOfNotNull(itemId?.let { id -> "itemId=${Uri.encode(id)}" }, to?.let { address -> "to=${Uri.encode(address)}" })
            .joinToString("&").let { query -> if (query.isEmpty()) "" else "?$query" }
    fun subscription(key: String): String = "mail/subscription/${Uri.encode(key)}"
    fun manageMail(mailId: String): String = "mail/manage/mail/${Uri.encode(mailId)}"
    fun manageReport(reportId: String): String = "mail/manage/report/${Uri.encode(reportId)}"
    fun manageDomain(domainId: String): String = "mail/manage/domain/${Uri.encode(domainId)}"
}

/** What a message being written starts from. */
enum class ComposeKind(val routeName: String) {
    New("new"), Reply("reply"), ReplyAll("replyAll"), Forward("forward"), Draft("draft");

    companion object {
        fun fromRouteName(name: String?): ComposeKind = entries.firstOrNull { kind -> kind.routeName == name } ?: New
    }
}
