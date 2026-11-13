package com.teanode.kit.contacts

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** ContactAccount is an account on the server, as a contact is tied to it and as the card names somebody. */
@Serializable
data class ContactAccount(
    val userId: String,
    val username: String = "",
    val name: String? = null,
    val avatarUrl: String? = null,
    val avatarColorIndex: Int? = null,
) {
    val displayName: String get() = if (name.isNullOrEmpty()) username else name
}

/**
 * AddressBook is an address book the person can use: their own, or one a circle they are in keeps for itself, which
 * everyone in the circle reads and edits. The fields after `contacts` come only from the list that has the circles'
 * books; an older answer has none, and the book is the person's own.
 */
@Serializable
data class AddressBook(
    val id: String,
    val name: String,
    val contacts: Int? = null,
    /** What the book says about itself, kept when it is renamed. */
    val description: String? = null,
    /** The circle a circle's book belongs to. */
    val circleId: String? = null,
    val circleName: String? = null,
    val isCircleBook: Boolean? = null,
    /** Whether the person may rename it and, for a circle's, delete it: its owner, or an admin of its circle. */
    val canManage: Boolean? = null,
) {
    /** Whether this is a circle's book. */
    val isOfACircle: Boolean get() = isCircleBook == true

    /** What a list calls the book: "Contacts" for the person's own, and for a circle's its name, with the circle's when the two differ. */
    val shownName: String
        get() = if (!isOfACircle || circleName.isNullOrEmpty() || circleName == name) name else "$name ($circleName)"
}

/** ContactSummary is a contact as the list shows it. */
@Serializable
data class ContactSummary(
    val id: String,
    val name: String? = null,
    val organization: String? = null,
    val emails: List<String>? = null,
    val phones: List<String>? = null,
    val hasPhoto: Boolean? = null,
    val addresses: List<Written>? = null,
    /** The account in the person's circles this contact is, when they said so. */
    val accountUserId: String? = null,
) {
    @Serializable
    data class Written(val written: String? = null)

    /** What the list calls them: their name, or else their first address. */
    val shownName: String get() = if (!name.isNullOrEmpty()) name else emails?.firstOrNull() ?: ""

    val photoPath: String get() = pictureAddress(id, null)

    /** Where a contact in a circle's book has its picture: the book is named, since the server looks in the person's own books first. */
    fun pictureAddress(inAddressBook: String?): String = pictureAddress(id, inAddressBook)

    companion object {
        /** The same for a view that knows only the contact's id. */
        fun pictureAddress(contactId: String, inAddressBook: String?): String {
            val path = "/api/v1/contacts/$contactId/photo"
            return if (inAddressBook.isNullOrEmpty()) path else "$path?addressBookId=$inAddressBook"
        }
    }
}

/** ContactAddress is one postal address of a contact. */
@Serializable
data class ContactAddress(
    val street: String? = null,
    val locality: String? = null,
    val region: String? = null,
    val postalCode: String? = null,
    val country: String? = null,
)

/** ContactDetail is a contact whole, read before it is changed: every field is sent back, since an empty one clears what is there. */
@Serializable
data class ContactDetail(
    val id: String,
    /** The address book it is in; null where it was not asked for. */
    val addressBookId: String? = null,
    val name: String? = null,
    val organization: String? = null,
    val emails: List<String>? = null,
    val phones: List<String>? = null,
    val note: String? = null,
    val hasPhoto: Boolean? = null,
    val addresses: List<ContactAddress>? = null,
    /**
     * The account on this server the contact is, when the person said so and still shares a circle with it; null
     * otherwise. Private to the person.
     */
    val accountUserId: String? = null,
    val account: ContactAccount? = null,
) {
    /** Whether the contact is linked to somebody in the person's circles. */
    val isLinkedToAccount: Boolean get() = !accountUserId.isNullOrEmpty() && account != null
}

/** ContactAccountLink is what linking a contact to an account (or clearing the link) answers: the contact and the account it is now. */
@Serializable
data class ContactAccountLink(val id: String, val accountUserId: String? = null, val account: ContactAccount? = null)

/**
 * ContactAccountSuggestion is an unlinked contact that looks like somebody in the person's circles, offered for the
 * person to confirm. Nothing is linked until they do, and the server does not say which address matched.
 */
@Serializable
data class ContactAccountSuggestion(val addressBookId: String, val contact: ContactSummary, val account: ContactAccount) {
    val id: String get() = contact.id
}

@Serializable
private class AddressBooksAnswer(@SerialName("ListAddressBooks") val books: List<AddressBook>)

@Serializable
private class ReachableAddressBooksAnswer(@SerialName("ListReachableAddressBooks") val books: List<AddressBook>)

@Serializable
private class CreatedCircleBookAnswer(@SerialName("CreateCircleAddressBook") val book: AddressBook)

@Serializable
private class SavedAddressBookAnswer(@SerialName("SaveAddressBook") val book: AddressBook)

@Serializable
private class ContactsAnswer(@SerialName("ListContacts") val contacts: List<ContactSummary>)

@Serializable
private class ContactAnswer(@SerialName("GetContact") val contact: ContactDetail)

@Serializable
private class MyContactAnswer(@SerialName("AgentGraphPage") val page: Page? = null) {
    @Serializable
    class Page(val contact: PageContact? = null)

    @Serializable
    class PageContact(val id: String)
}

@Serializable
private class SetContactAccountAnswer(@SerialName("SetContactAccount") val link: ContactAccountLink)

@Serializable
private class SuggestionsAnswer(@SerialName("ListContactAccountSuggestions") val suggestions: List<ContactAccountSuggestion>)

/** The id of a contact and, when it is known, the book it is in. */
fun contactVariables(contactId: String, addressBookId: String?): Map<String, JsonElement> {
    val variables = linkedMapOf<String, JsonElement>("id" to JsonPrimitive(contactId))
    if (!addressBookId.isNullOrEmpty()) variables["addressBookId"] = JsonPrimitive(addressBookId)
    return variables
}

suspend fun Connection.addressBooks(): List<AddressBook> = execute<AddressBooksAnswer>(ContactDocuments.ADDRESS_BOOKS).books

/**
 * The person's own address books and then the books of the circles they are in. `addressBooks` stays their own alone,
 * which is what the photos, the recordings and the person's card tie to.
 */
suspend fun Connection.reachableAddressBooks(): List<AddressBook> =
    execute<ReachableAddressBooksAnswer>(ContactDocuments.REACHABLE_ADDRESS_BOOKS).books

/** Makes the address book a circle keeps for itself; any member of the circle may. A circle has one. */
suspend fun Connection.createCircleAddressBook(circleId: String, name: String? = null): AddressBook {
    val variables = linkedMapOf<String, JsonElement>("circleId" to JsonPrimitive(circleId))
    if (!name.isNullOrEmpty()) variables["name"] = JsonPrimitive(name)
    return execute<CreatedCircleBookAnswer>(ContactDocuments.CREATE_CIRCLE_ADDRESS_BOOK, variables).book
}

/** Renames an address book, sending back what it says about itself; a circle's only by an admin of the circle. */
suspend fun Connection.renameAddressBook(book: AddressBook, name: String): AddressBook = execute<SavedAddressBookAnswer>(
    ContactDocuments.SAVE_ADDRESS_BOOK,
    mapOf("id" to JsonPrimitive(book.id), "name" to JsonPrimitive(name), "description" to JsonPrimitive(book.description ?: "")),
).book

/** Deletes a circle's address book with every contact in it; only an admin of the circle may. */
suspend fun Connection.deleteCircleAddressBook(addressBookId: String) {
    execute<JsonElement>(ContactDocuments.DELETE_CIRCLE_ADDRESS_BOOK, mapOf("addressBookId" to JsonPrimitive(addressBookId)))
}

suspend fun Connection.contacts(addressBookId: String, matching: String? = null, first: Int? = null): List<ContactSummary> {
    val variables = linkedMapOf<String, JsonElement>("addressBookId" to JsonPrimitive(addressBookId))
    if (!matching.isNullOrEmpty()) variables["query"] = JsonPrimitive(matching)
    if (first != null) variables["first"] = JsonPrimitive(first)
    return execute<ContactsAnswer>(ContactDocuments.CONTACTS, variables).contacts
}

/**
 * One contact, whole. A contact is named only within its book, so a contact in a circle's book is read by naming the
 * book; left out, the server looks in the person's own books first.
 */
suspend fun Connection.contact(contactId: String, addressBookId: String? = null): ContactDetail =
    execute<ContactAnswer>(ContactDocuments.CONTACT, contactVariables(contactId, addressBookId)).contact

/** Saves a contact, every field sent: an empty one clears what is there. */
suspend fun Connection.saveContact(
    addressBookId: String,
    contactId: String?,
    name: String,
    organization: String,
    emails: List<String>,
    phones: List<String>,
    note: String,
    addresses: List<ContactAddress>,
) {
    val variables = linkedMapOf<String, JsonElement>(
        "addressBookId" to JsonPrimitive(addressBookId), "name" to JsonPrimitive(name), "organization" to JsonPrimitive(organization),
        "emails" to JsonArray(emails.map { email -> JsonPrimitive(email) }), "phones" to JsonArray(phones.map { phone -> JsonPrimitive(phone) }),
        "note" to JsonPrimitive(note),
    )
    if (contactId != null) variables["id"] = JsonPrimitive(contactId)
    variables["addresses"] = JsonArray(
        addresses.map { address ->
            JsonObject(
                listOf("street" to address.street, "locality" to address.locality, "region" to address.region, "postalCode" to address.postalCode, "country" to address.country)
                    .filter { entry -> entry.second != null }.associate { entry -> entry.first to JsonPrimitive(entry.second) },
            )
        },
    )
    execute<JsonElement>(ContactDocuments.SAVE_CONTACT, variables)
}

/** Deletes a contact, in the book named as [contact] does. */
suspend fun Connection.deleteContact(contactId: String, addressBookId: String? = null) {
    execute<JsonElement>(ContactDocuments.DELETE_CONTACT, contactVariables(contactId, addressBookId))
}

/** Takes the picture off a contact and leaves the rest of the card, in the book named as [contact] does. */
suspend fun Connection.removeContactPicture(contactId: String, addressBookId: String? = null) {
    execute<JsonElement>(ContactDocuments.REMOVE_CONTACT_PICTURE, contactVariables(contactId, addressBookId))
}

/** The contact that is the person themselves, if one is marked. */
suspend fun Connection.myContactId(): String? = execute<MyContactAnswer>(ContactDocuments.MY_CONTACT).page?.contact?.id

suspend fun Connection.setMyContact(contactId: String?) {
    execute<JsonElement>(ContactDocuments.SET_MY_CONTACT, mapOf("contactId" to (contactId?.let { id -> JsonPrimitive(id) } ?: JsonNull)))
}

/**
 * Says which account on this server a contact is, chosen from the people the person shares a circle with. An empty or
 * null `accountUserId` clears the link. Private to the person: the account is never told.
 */
suspend fun Connection.setContactAccount(addressBookId: String, contactId: String, accountUserId: String?): ContactAccountLink =
    execute<SetContactAccountAnswer>(
        ContactDocuments.SET_CONTACT_ACCOUNT,
        mapOf(
            "addressBookId" to JsonPrimitive(addressBookId), "contactId" to JsonPrimitive(contactId),
            "accountUserId" to (accountUserId?.let { id -> JsonPrimitive(id) } ?: JsonNull),
        ),
    ).link

/** The contacts that look like people in the person's circles, each to be confirmed or left alone. */
suspend fun Connection.contactAccountSuggestions(): List<ContactAccountSuggestion> =
    execute<SuggestionsAnswer>(ContactDocuments.CONTACT_ACCOUNT_SUGGESTIONS).suggestions
