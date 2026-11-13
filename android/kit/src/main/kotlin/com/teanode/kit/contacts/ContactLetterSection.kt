package com.teanode.kit.contacts

import com.teanode.kit.Monogram
import java.text.Collator
import java.util.Locale

/** ContactLetterSection is the contacts whose names start with one letter, as the list groups them. */
data class ContactLetterSection(val letter: String, val contacts: List<ContactSummary>) {
    companion object {
        /** The contacts sorted the way a person sorts names, grouped by the first letter or digit of their name; those with none go under "#". */
        fun sections(contacts: List<ContactSummary>, locale: Locale = Locale.getDefault()): List<ContactLetterSection> {
            val collator = Collator.getInstance(locale).apply { strength = Collator.PRIMARY }
            val sorted = contacts.sortedWith { left, right -> collator.compare(left.shownName, right.shownName) }
            val sections = mutableListOf<ContactLetterSection>()
            for (contact in sorted) {
                val monogram = Monogram.letter(contact.shownName)
                val letter = if (monogram == "?") "#" else monogram
                val last = sections.lastOrNull()
                if (last != null && last.letter == letter) {
                    sections[sections.size - 1] = ContactLetterSection(letter, last.contacts + contact)
                } else {
                    sections.add(ContactLetterSection(letter, listOf(contact)))
                }
            }
            return sections
        }
    }
}

/** The words a contact's postal address is written in: street, then city, region and postal code, then country. */
object ContactWords {
    fun written(address: ContactAddress): String {
        val cityLine = listOfNotNull(address.locality, address.region, address.postalCode).filter { part -> part.isNotEmpty() }.joinToString(" ")
        return listOfNotNull(address.street, cityLine, address.country).filter { part -> part.isNotEmpty() }.joinToString("\n")
    }
}
