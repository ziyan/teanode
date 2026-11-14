package com.teanode.app.notifications

import com.teanode.kit.notifications.NotificationDestination
import com.teanode.kit.AppSection
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

    private val pendingNotice = MutableStateFlow<String?>(null)

    /** What to tell the person instead, when the notification cannot be opened (its account is gone). */
    val notice: StateFlow<String?> = pendingNotice.asStateFlow()

    fun open(destination: NotificationDestination) {
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
        is NotificationDestination.Conversation, NotificationDestination.Main -> AppSection.Agent
    }
}
