package com.teanode.kit.photos

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

/** A contact the person can choose to name a face group: what the list of contacts says of them. */
@Serializable
data class PhotoNamingContact(val id: String, val name: String? = null, val emails: List<String>? = null, val addressBookId: String = "") {
    /** What the list calls them: their name, or else their first address. */
    val shownName: String get() = name?.takeIf { text -> text.isNotEmpty() } ?: emails?.firstOrNull().orEmpty()
}

@Serializable private class AddressBookName(val id: String, val name: String = "")

@Serializable private class AddressBooksAnswer(val ListAddressBooks: List<AddressBookName>)

@Serializable private class ContactsAnswer(val ListContacts: List<PhotoNamingContact>)

@Serializable private class SavedContact(val id: String, val name: String? = null)

@Serializable private class SaveContactAnswer(val SaveContact: SavedContact)

/** The contacts of the person's address books that match the words, a few from each book. */
suspend fun Connection.photoNamingContacts(matching: String): List<PhotoNamingContact> {
    val contacts = mutableListOf<PhotoNamingContact>()
    for (book in execute<AddressBooksAnswer>(PhotoDocuments.ADDRESS_BOOKS).ListAddressBooks) {
        val variables = mutableMapOf<String, JsonElement>("addressBookId" to JsonPrimitive(book.id), "first" to JsonPrimitive(PhotoNaming.CONTACTS_PER_BOOK))
        matching.trim().takeIf { words -> words.isNotEmpty() }?.let { words -> variables["query"] = JsonPrimitive(words) }
        contacts += execute<ContactsAnswer>(PhotoDocuments.CONTACTS, variables).ListContacts.map { contact -> contact.copy(addressBookId = book.id) }
    }
    return contacts
}

/** Makes a contact of this name in the person's first address book, so a face group can be named after somebody new. */
suspend fun Connection.createNamedContact(name: String): PhotoNamingContact {
    val book = execute<AddressBooksAnswer>(PhotoDocuments.ADDRESS_BOOKS).ListAddressBooks.firstOrNull() ?: throw PhotoError.NoAddressBook
    val saved = execute<SaveContactAnswer>(
        PhotoDocuments.CREATE_NAMED_CONTACT, mapOf("addressBookId" to JsonPrimitive(book.id), "name" to JsonPrimitive(name)),
    ).SaveContact
    return PhotoNamingContact(saved.id, saved.name, null, book.id)
}
