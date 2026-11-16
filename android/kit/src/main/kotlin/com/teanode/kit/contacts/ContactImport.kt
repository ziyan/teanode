package com.teanode.kit.contacts

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

// Bringing the contacts on the phone into the address book: which of them the address book already has, and the cards
// that are sent for the rest. The phone's contacts arrive here as plain values, so that this can be tested without
// the phone's contacts provider.

/** ContactIdentity is what tells one contact from another: the name and the ways to reach them, whether they came from the phone or the address book. */
data class ContactIdentity(val name: String, val emails: List<String>, val phones: List<String>)

/** What the contact is matched by when the phone's contacts are imported. */
val ContactSummary.identity: ContactIdentity get() = ContactIdentity(name ?: "", emails ?: emptyList(), phones ?: emptyList())

/**
 * ContactMatcher decides which contacts are already in the address book, so that importing twice does not make every
 * contact twice.
 *
 * A contact is already there when it shares an email address (compared in lower case) or a phone number (compared by
 * its digits) with one that is. A contact with neither is compared by its whole name instead, since that is all there
 * is to go on.
 */
class ContactMatcher(existing: List<ContactIdentity>) {
    private val emails = HashSet<String>()
    private val wholeNumbers = HashSet<String>()
    private val numberEnds = HashSet<String>()
    private val namesWithoutAddresses = HashSet<String>()

    init {
        for (contact in existing) {
            val contactEmails = contact.emails.map(::normalizedEmail).filter { email -> email.isNotEmpty() }
            val contactNumbers = contact.phones.map(::digits).filter { number -> number.isNotEmpty() }
            emails.addAll(contactEmails)
            for (number in contactNumbers) {
                wholeNumbers.add(number)
                if (number.length >= COMPARED_DIGIT_COUNT) numberEnds.add(number.takeLast(COMPARED_DIGIT_COUNT))
            }
            if (contactEmails.isEmpty() && contactNumbers.isEmpty()) {
                val name = normalizedName(contact.name)
                if (name.isNotEmpty()) namesWithoutAddresses.add(name)
            }
        }
    }

    /** Whether the address book already has this contact. */
    fun isAlreadyPresent(candidate: ContactIdentity): Boolean {
        val candidateEmails = candidate.emails.map(::normalizedEmail).filter { email -> email.isNotEmpty() }
        val candidateNumbers = candidate.phones.map(::digits).filter { number -> number.isNotEmpty() }
        if (candidateEmails.any { email -> emails.contains(email) }) return true
        for (number in candidateNumbers) {
            if (number.length >= COMPARED_DIGIT_COUNT) {
                if (numberEnds.contains(number.takeLast(COMPARED_DIGIT_COUNT))) return true
            } else if (wholeNumbers.contains(number)) {
                return true
            }
        }
        if (candidateEmails.isEmpty() && candidateNumbers.isEmpty()) return namesWithoutAddresses.contains(normalizedName(candidate.name))
        return false
    }

    companion object {
        /**
         * How many digits at the end of a number are compared when both numbers have at least that many: the digits
         * before them are a country code or a trunk prefix, which one place writes and another leaves out.
         */
        const val COMPARED_DIGIT_COUNT = 10

        internal fun normalizedEmail(email: String): String = email.trim().lowercase()

        internal fun digits(phone: String): String = phone.filter { character -> character in '0'..'9' }

        internal fun normalizedName(name: String): String = name.split(Regex("""\s+""")).filter { word -> word.isNotEmpty() }.joinToString(" ").lowercase()
    }
}

/** ContactCard is a vCard on its way to the address book. */
object ContactCard {
    /**
     * The most a card may be, in bytes of UTF-8. The server refuses a card above 1 MiB, and the request that carries it
     * is also held to 1 MiB with the card's line breaks and quotes written out as escapes, so a card is kept to half
     * of that.
     */
    const val MAXIMUM_BYTE_COUNT = 512 * 1024

    /** Whether the server will take a card this large. */
    fun fits(card: String): Boolean = card.toByteArray(Charsets.UTF_8).size <= MAXIMUM_BYTE_COUNT

    /**
     * The card without its picture. A photograph is written into a card as text a third larger than the picture, which
     * would make a card of hundreds of kilobytes for the sake of a thumbnail the address book list does not need.
     */
    fun removingPhoto(card: String): String {
        val kept = mutableListOf<String>()
        var isInPhoto = false
        for (line in card.split("\n")) {
            // A folded line goes on from the one before it.
            if (line.startsWith(" ") || line.startsWith("\t")) {
                if (!isInPhoto) kept.add(line)
                continue
            }
            isInPhoto = isPhotoLine(line)
            if (!isInPhoto) kept.add(line)
        }
        return kept.joinToString("\n")
    }

    /** Whether a line starts the PHOTO property, which may carry a group ("item1.PHOTO") and parameters (";ENCODING=b"). */
    private fun isPhotoLine(line: String): Boolean {
        val end = line.indexOfFirst { character -> character == ';' || character == ':' }
        if (end < 0) return false
        val name = line.substring(0, end)
        return name.substringAfterLast('.').uppercase() == "PHOTO"
    }
}

@Serializable
private class ImportAnswer(@SerialName("SaveContact") val saved: Saved) {
    @Serializable
    class Saved(val id: String)
}

/**
 * Adds a contact from a whole vCard, which is how the phone writes one. The server reads the card itself; a card whose
 * UID the address book already has replaces that contact instead of making another.
 */
suspend fun Connection.importContact(addressBookId: String, card: String) {
    execute<ImportAnswer>(ContactImportDocuments.IMPORT_CONTACT, mapOf("addressBookId" to JsonPrimitive(addressBookId), "card" to JsonPrimitive(card)))
}
