package com.teanode.kit.sharing

import com.teanode.kit.Connection
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import java.time.Instant

// Everything the person shares and everything shared with them, in one list, across calendar, location, Health, memory
// pages, photo albums, recordings, finance accounts, skills, passwords and shopping lists: the same operations the
// dashboard's Sharing page, the command line and the circle tool use. Stopping a share is the kind's own operation.

/** What a share is of. The server names them with these words. */
enum class SharingKind(val serverName: String) {
    Calendar("calendar"), Location("location"), Health("health"), MemoryPage("memory_page"), PhotoAlbum("photo_album"),
    Recording("recording"), Finance("finance"), Skill("skill"), Passwords("passwords"), ShoppingList("shopping_list");

    companion object {
        fun fromServerName(serverName: String): SharingKind? = entries.firstOrNull { kind -> kind.serverName == serverName }
    }
}

/** The words a share's scopes can carry. */
object SharingScope {
    /** The scope of a finance share that covers all of the owner's accounts, now and later, instead of one. */
    const val ALL_ACCOUNTS = "allAccounts"

    /** Whether a share is of all of a person's finance. */
    fun isAllFinance(shareKind: String, shareScopes: List<String>): Boolean = shareKind == SharingKind.Finance.serverName && shareScopes.contains(ALL_ACCOUNTS)
}

/**
 * One share the person made: of what, with whom, at what level. A share of passwords has no id of its own and is
 * counted, never named, because an item's title is sealed.
 */
@Serializable
data class SharingMade(
    val shareKind: String,
    /** Empty for passwords. */
    val shareId: String = "",
    /** The calendar's, album's, recording's or finance account's identifier, the page's path or the skill's name; empty for location, Health and passwords. */
    val subjectId: String = "",
    /** A recording's title or a finance account's name, among the rest. */
    val subjectName: String = "",
    /** The last digits of a finance account's number; empty for every other kind, and absent from a server that does not send it. */
    val subjectMask: String? = null,
    /** True for a share of a memory page that no longer exists, which the owner can still stop; its subjectName is then the path. */
    val isSubjectDeleted: Boolean? = null,
    /** The level of a calendar, location, album, recording or finance share, empty for the rest. */
    val shareLevel: String = "",
    /** What else it gives: the Health categories, the skill's tools, quotes for a page and location for an album. */
    val shareScopes: List<String> = emptyList(),
    val passwordItemCount: Int = 0,
    val granteeUserId: String? = null,
    val granteeUsername: String = "",
    val granteeName: String = "",
    val granteeCircleId: String? = null,
    val granteeCircleName: String = "",
    val createdAt: String = "",
) {
    val identity: String get() = shareKind + "|" + (shareId.ifEmpty { granteeUserId ?: granteeCircleId ?: "" })

    /** The kind, or null for one the app does not know. */
    val knownKind: SharingKind? get() = SharingKind.fromServerName(shareKind)

    /** Whether it is shared with a circle rather than a person. */
    val isCircle: Boolean get() = granteeCircleId != null

    /** Whether it is a share of all of the person's finance, which names no account and is taken back with UnshareFinance. */
    val isAllFinance: Boolean get() = SharingScope.isAllFinance(shareKind, shareScopes)

    /** Who it is shared with: the circle's name, or the person's name, or their username when they have no name. */
    val shownGrantee: String get() = if (isCircle) granteeCircleName else granteeName.ifEmpty { granteeUsername }

    /** Whether a button can stop it: every kind but passwords, which are stopped item by item on the Passwords screen. */
    val canStop: Boolean get() = knownKind?.let { kind -> kind != SharingKind.Passwords } ?: false

    /** The account's last digits, empty when there are none. */
    val shownSubjectMask: String get() = subjectMask ?: ""

    /** Whether the page that was shared is gone. */
    val isPageGone: Boolean get() = isSubjectDeleted == true

    /** When it was made, or null when the server's text cannot be read. */
    val since: Instant? get() = ServerTime.instant(createdAt)
}

/**
 * One share made to the person. A share through a circle names the circle and ends when the person leaves it; one made
 * by name can be declined, except passwords, which their owner removes.
 */
@Serializable
data class SharingReceived(
    val shareKind: String,
    val shareId: String = "",
    val subjectId: String = "",
    val subjectName: String = "",
    val subjectMask: String? = null,
    val shareLevel: String = "",
    val shareScopes: List<String> = emptyList(),
    val passwordItemCount: Int = 0,
    val ownerUserId: String,
    val ownerUsername: String,
    val ownerName: String = "",
    val granteeCircleId: String? = null,
    val granteeCircleName: String = "",
    /** Whether it was made to the person by name. */
    val isDirect: Boolean,
    /** Whether DeclineShare ends it. */
    val canDecline: Boolean,
    val createdAt: String = "",
) {
    val identity: String get() = shareKind + "|" + shareId.ifEmpty { ownerUserId + "|" + (granteeCircleId ?: "") }

    val knownKind: SharingKind? get() = SharingKind.fromServerName(shareKind)

    /** Whether it is a share of all of the owner's finance, which names no account. */
    val isAllFinance: Boolean get() = SharingScope.isAllFinance(shareKind, shareScopes)

    /** Who shared it: their name, or their username when they have none. */
    val shownOwner: String get() = ownerName.ifEmpty { ownerUsername }

    /** The account's last digits, empty when there are none. */
    val shownSubjectMask: String get() = subjectMask ?: ""

    val since: Instant? get() = ServerTime.instant(createdAt)
}

/** What the person shares and what is shared with them. */
@Serializable
data class SharingList(val sharesMade: List<SharingMade> = emptyList(), val sharesReceived: List<SharingReceived> = emptyList()) {
    val isEmpty: Boolean get() = sharesMade.isEmpty() && sharesReceived.isEmpty()
}

/** Why a share could not be stopped from the list. */
sealed class SharingError(message: String) : Exception(message) {
    class UnknownKind(val shareKind: String) : SharingError("unknown share kind $shareKind")
    data object PasswordsAreStoppedOnTheirOwnScreen : SharingError("passwords are stopped item by item on the Passwords screen")
}

@Serializable private class ListSharingAnswer(val ListSharing: SharingList)

/** What the person shares and what is shared with them. */
suspend fun Connection.sharing(): SharingList = execute<ListSharingAnswer>(SharingDocuments.SHARING).ListSharing

/**
 * Takes a share back with the kind's own operation. A share of passwords is stopped item by item on the Passwords
 * screen, so it is refused here. The mutations are the ones the calendar, location, Health, knowledge, photos,
 * recordings, finance, skills and shopping screens send for the same thing.
 */
suspend fun Connection.stopSharing(share: SharingMade) {
    val kind = share.knownKind ?: throw SharingError.UnknownKind(share.shareKind)
    when (kind) {
        SharingKind.Calendar -> stopShare(StopSharingDocuments.UNSHARE_CALENDAR, variablesOf("shareId" to share.shareId))
        SharingKind.Location -> stopShare(StopSharingDocuments.UNSHARE_LOCATION, variablesOf("shareId" to share.shareId))
        SharingKind.Health -> stopShare(
            StopSharingDocuments.SET_HEALTH_SHARE,
            variablesOf("granteeUserId" to share.granteeUserId, "granteeCircleId" to share.granteeCircleId, "healthCategories" to emptyList<String>()),
        )
        SharingKind.MemoryPage -> stopShare(StopSharingDocuments.UNSHARE_KNOWLEDGE_PAGE, variablesOf("shareId" to share.shareId))
        SharingKind.PhotoAlbum -> stopShare(StopSharingDocuments.REMOVE_PHOTO_ALBUM_SHARE, variablesOf("photoAlbumShareId" to share.shareId))
        SharingKind.Recording -> stopShare(StopSharingDocuments.UNSHARE_RECORDING, variablesOf("recordingShareId" to share.shareId))
        SharingKind.Finance ->
            if (share.isAllFinance) {
                stopShare(StopSharingDocuments.UNSHARE_ALL_FINANCE, variablesOf("financeShareId" to share.shareId))
            } else {
                stopShare(StopSharingDocuments.UNSHARE_FINANCE_ACCOUNT, variablesOf("shareId" to share.shareId))
            }
        SharingKind.Skill -> stopShare(StopSharingDocuments.UNSHARE_SKILL, variablesOf("shareId" to share.shareId))
        SharingKind.ShoppingList -> stopShare(StopSharingDocuments.UNSHARE_SHOPPING_LIST, variablesOf("shoppingListShareId" to share.shareId))
        SharingKind.Passwords -> throw SharingError.PasswordsAreStoppedOnTheirOwnScreen
    }
}

private suspend fun Connection.stopShare(document: String, variables: Map<String, JsonElement>) {
    execute<JsonObject>(document, variables)
}

/** Stops receiving a share that was made to the person by name. The owner is not told. */
suspend fun Connection.declineShare(share: SharingReceived) {
    execute<JsonObject>(SharingDocuments.DECLINE_SHARE, variablesOf("shareKind" to share.shareKind, "shareId" to share.shareId))
}

/** The mutations that end a share of each kind, copied from the iPhone app's Documents of that kind. */
object StopSharingDocuments {
    const val UNSHARE_CALENDAR = """mutation (${'$'}shareId: String!) { UnshareCalendar(shareId: ${'$'}shareId) }"""
    const val UNSHARE_LOCATION = """mutation (${'$'}shareId: String!) { UnshareLocation(shareId: ${'$'}shareId) }"""
    const val SET_HEALTH_SHARE = """mutation (${'$'}granteeUserId: String, ${'$'}granteeCircleId: String, ${'$'}healthCategories: [String!]!) {
  SetHealthShare(granteeUserId: ${'$'}granteeUserId, granteeCircleId: ${'$'}granteeCircleId, healthCategories: ${'$'}healthCategories) { id ownerUserId ownerUsername ownerName granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName healthCategories createdAt }
}"""
    const val UNSHARE_KNOWLEDGE_PAGE = """mutation (${'$'}shareId: String!) { UnshareAgentPage(shareId: ${'$'}shareId) }"""
    const val REMOVE_PHOTO_ALBUM_SHARE = """mutation (${'$'}photoAlbumShareId: String!) { RemovePhotoAlbumShare(photoAlbumShareId: ${'$'}photoAlbumShareId) }"""
    const val UNSHARE_RECORDING = """mutation (${'$'}recordingShareId: String!) { UnshareRecording(recordingShareId: ${'$'}recordingShareId) }"""
    const val UNSHARE_ALL_FINANCE = """mutation (${'$'}financeShareId: String!) { UnshareFinance(financeShareId: ${'$'}financeShareId) }"""
    const val UNSHARE_FINANCE_ACCOUNT = """mutation (${'$'}shareId: String!) { UnshareFinanceAccount(shareId: ${'$'}shareId) }"""
    const val UNSHARE_SKILL = """mutation (${'$'}shareId: String!) { UnshareSkill(shareId: ${'$'}shareId) }"""
    const val UNSHARE_SHOPPING_LIST = """mutation (${'$'}shoppingListShareId: String!) { UnshareShoppingList(shoppingListShareId: ${'$'}shoppingListShareId) }"""
}
