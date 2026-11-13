package com.teanode.kit.contacts

/** One contact of the phone, as the phone keeps it: the parts of a card the address book takes. */
data class PhoneContactFields(
    val identifier: String,
    val givenName: String = "",
    val familyName: String = "",
    val fullName: String = "",
    val organization: String = "",
    val emails: List<String> = emptyList(),
    val phones: List<String> = emptyList(),
    val note: String = "",
)

/**
 * ContactCardWriter writes a vCard 3.0 for a contact of the phone, the way the iPhone's serializer does: the server reads
 * the card itself. The picture is left out, as [ContactCard.removingPhoto] does on the iPhone.
 */
object ContactCardWriter {
    fun card(contact: PhoneContactFields): String {
        val lines = mutableListOf("BEGIN:VCARD", "VERSION:3.0")
        lines.add("N:${escaped(contact.familyName)};${escaped(contact.givenName)};;;")
        lines.add("FN:${escaped(contact.fullName.ifEmpty { contact.organization })}")
        if (contact.organization.isNotEmpty()) lines.add("ORG:${escaped(contact.organization)};")
        for (email in contact.emails) lines.add("EMAIL;TYPE=INTERNET:${escaped(email)}")
        for (phone in contact.phones) lines.add("TEL:${escaped(phone)}")
        if (contact.note.isNotEmpty()) lines.add("NOTE:${escaped(contact.note)}")
        // The phone's own identifier makes importing the same contact again replace it rather than make another.
        lines.add("UID:${escaped(contact.identifier)}")
        lines.add("END:VCARD")
        return lines.joinToString("\r\n") + "\r\n"
    }

    /** A value of a card with what the format reserves written as escapes. */
    private fun escaped(text: String): String =
        text.replace("\\", "\\\\").replace("\r\n", "\\n").replace("\n", "\\n").replace(",", "\\,").replace(";", "\\;")
}
