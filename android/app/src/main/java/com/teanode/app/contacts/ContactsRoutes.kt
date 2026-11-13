package com.teanode.app.contacts

import android.net.Uri
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes
import com.teanode.kit.AppSection
import com.teanode.kit.contacts.PersonCardTarget
import com.teanode.kit.contacts.openDirectChat

/** The routes of Contacts, built in one place so a screen and the graph agree. */
object ContactsRoutes {
    const val CONTACT = "contacts/contact/{contactId}"
    const val EDITOR = "contacts/editor?contactId={contactId}"
    const val IMPORT = "contacts/import"
    const val PERSON_ACCOUNT = "contacts/person/account/{userId}"
    const val PERSON_CONTACT = "contacts/person/contact/{addressBookId}/{contactId}"
    const val VISITS = "contacts/visits/{userId}"

    fun contact(contactId: String): String = "contacts/contact/${Uri.encode(contactId)}"
    fun editor(contactId: String?): String = "contacts/editor" + (contactId?.let { id -> "?contactId=${Uri.encode(id)}" } ?: "")
    fun visits(userId: String): String = "contacts/visits/${Uri.encode(userId)}"

    /** Where a person's card is, for a screen of another part that shows a person: an account in the circles, or a contact. */
    fun person(target: PersonCardTarget): String = when (target) {
        is PersonCardTarget.Account -> "contacts/person/account/${Uri.encode(target.userId)}"
        is PersonCardTarget.Contact -> "contacts/person/contact/${Uri.encode(target.addressBookId)}/${Uri.encode(target.contactId)}"
    }

    val home: String get() = Routes.home(AppSection.Contacts)
}

/**
 * Opens the direct chat with somebody and goes to it. The chat is made when there is none; the conversation's own screen
 * belongs to Chats, so when this app has no route for it yet the Chats list opens instead.
 */
suspend fun openDirectChat(model: ContactsModel, navigator: AppNavigator, userId: String) {
    val conversationId = model.connection.openDirectChat(userId)
    val isOpened = runCatching { navigator.navigate("agent/conversation/${Uri.encode(conversationId)}") }.isSuccess
    if (!isOpened) navigator.openSection(AppSection.Agent)
}
