package com.teanode.app.notifications

import com.teanode.app.coding.codingSessionRoute
import com.teanode.app.journal.JournalRoutes
import com.teanode.app.settings.SettingsRoutes
import com.teanode.app.shell.Routes
import com.teanode.kit.notifications.NotificationDestination
import com.teanode.kit.AppSection
import android.net.Uri
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * NotificationOpening is where the app goes when a notification is pressed. The press is decided in the kit; this holds
 * the place until the shell has gone to its part, and the parts read the detail (which conversation, which person,
 * which store or list, which session, which day) from it as the iPhone app reads `AppModel.openedConversationId` and
 * its siblings.
 */
object NotificationOpening {
    private val pendingDestination = MutableStateFlow<NotificationDestination?>(null)

    /** The place to open, until the part that shows it has taken it with [consume]. */
    val destination: StateFlow<NotificationDestination?> = pendingDestination.asStateFlow()

    private val pendingLocationUserId = MutableStateFlow<String?>(null)

    /** Who to select on the Location map when it shows, until it has taken them with [consumeLocationUserId]. */
    val locationUserId: StateFlow<String?> = pendingLocationUserId.asStateFlow()

    fun consumeLocationUserId() {
        pendingLocationUserId.value = null
    }

    private val pendingNotice = MutableStateFlow<String?>(null)

    /** What to tell the person instead, when the notification cannot be opened (its account is gone). */
    val notice: StateFlow<String?> = pendingNotice.asStateFlow()

    fun open(destination: NotificationDestination) {
        pendingLocationUserId.value = (destination as? NotificationDestination.Location)?.userId?.takeIf { userId -> userId.isNotEmpty() }
        pendingDestination.value = destination
    }

    fun tell(text: String) {
        pendingNotice.value = text
    }

    fun consume() {
        pendingDestination.value = null
    }

    fun consumeNotice() {
        pendingNotice.value = null
    }

    /** The part of the app a place is in. */
    fun sectionOf(destination: NotificationDestination): AppSection = when (destination) {
        NotificationDestination.Circles -> AppSection.Settings
        is NotificationDestination.Location -> AppSection.Location
        is NotificationDestination.Shopping -> AppSection.Shopping
        is NotificationDestination.Coding, NotificationDestination.CodingHome -> AppSection.Coding
        is NotificationDestination.JournalDay -> AppSection.Journal
        NotificationDestination.Feed -> AppSection.Feed
        is NotificationDestination.Conversation, NotificationDestination.Main -> AppSection.Agent
    }

    /**
     * The screens to open for a place, in order: the part first, then the item inside it, so back goes to the part.
     * Mirrors the iPhone app's `AppModel.perform` for a pressed notification.
     */
    fun routesOf(destination: NotificationDestination): List<String> {
        val partRoute = Routes.home(sectionOf(destination))
        return when (destination) {
            is NotificationDestination.Conversation -> listOf(partRoute, Routes.child(AppSection.Agent, "conversation/${Uri.encode(destination.conversationId)}"))
            NotificationDestination.Circles -> listOf(partRoute, SettingsRoutes.CIRCLES)
            is NotificationDestination.Shopping -> when {
                !destination.storeName.isNullOrEmpty() -> listOf(partRoute, Routes.child(AppSection.Shopping, "store/${Uri.encode(destination.storeName)}"))
                !destination.listId.isNullOrEmpty() -> listOf(partRoute, Routes.child(AppSection.Shopping, "list/${Uri.encode(destination.listId)}"))
                else -> listOf(partRoute)
            }
            is NotificationDestination.Coding -> listOf(partRoute, codingSessionRoute(destination.computer, destination.paneId))
            is NotificationDestination.JournalDay -> if (destination.day.isEmpty()) listOf(partRoute) else listOf(partRoute, JournalRoutes.day(destination.day))
            // Location opens on the map with the person selected: the screen reads [LocationOpening].
            is NotificationDestination.Location, NotificationDestination.CodingHome, NotificationDestination.Main, NotificationDestination.Feed -> listOf(partRoute)
        }
    }
}
