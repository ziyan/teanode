package com.teanode.kit.contacts

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ContactImportTest {
    private fun person(name: String, emails: List<String> = emptyList(), phones: List<String> = emptyList()) = ContactIdentity(name, emails, phones)

    @Test
    fun aSharedEmailMatchesWhateverTheCaseAndSpacing() {
        val matcher = ContactMatcher(listOf(person("Ada Example", emails = listOf("Ada@Example.com"))))
        assertTrue(matcher.isAlreadyPresent(person("A. Example", emails = listOf(" ada@example.COM "))))
        assertFalse(matcher.isAlreadyPresent(person("Ada Example", emails = listOf("ada@example.org"))))
    }

    @Test
    fun anyOneOfSeveralEmailsMatches() {
        val matcher = ContactMatcher(listOf(person("Ada Example", emails = listOf("ada@example.com", "ada@example.org"))))
        assertTrue(matcher.isAlreadyPresent(person("Ada", emails = listOf("other@example.net", "ADA@EXAMPLE.ORG"))))
    }

    @Test
    fun phonesMatchByDigitsWhateverTheWriting() {
        val matcher = ContactMatcher(listOf(person("Ben Sample", phones = listOf("(555) 010-1234"))))
        assertTrue(matcher.isAlreadyPresent(person("Ben", phones = listOf("555.010.1234"))))
        assertFalse(matcher.isAlreadyPresent(person("Ben", phones = listOf("555 010 1235"))))
    }

    @Test
    fun aPhoneMatchesWithOrWithoutACountryCode() {
        val matcher = ContactMatcher(listOf(person("Ben Sample", phones = listOf("555 010 1234"))))
        assertTrue(matcher.isAlreadyPresent(person("Ben", phones = listOf("+1 555 010 1234"))))
        val withCode = ContactMatcher(listOf(person("Ben Sample", phones = listOf("+1 555 010 1234"))))
        assertTrue(withCode.isAlreadyPresent(person("Ben", phones = listOf("(555) 010-1234"))))
    }

    @Test
    fun shortNumbersAreComparedWhole() {
        val matcher = ContactMatcher(listOf(person("Front desk", phones = listOf("0101234"))))
        assertTrue(matcher.isAlreadyPresent(person("Desk", phones = listOf("010-1234"))))
        // The last digits alone are not enough when a number is short.
        assertFalse(matcher.isAlreadyPresent(person("Desk", phones = listOf("555 010 1234"))))
    }

    @Test
    fun textWithoutDigitsIsNotANumber() {
        val matcher = ContactMatcher(listOf(person("Ben Sample", phones = listOf("none"))))
        assertFalse(matcher.isAlreadyPresent(person("Cy Sample", phones = listOf("n/a"))))
    }

    @Test
    fun theNameMatchesOnlyWhenNeitherSideHasAddresses() {
        val matcher = ContactMatcher(listOf(person("Cy Sample")))
        assertTrue(matcher.isAlreadyPresent(person("  cy   SAMPLE ")))
        assertFalse(matcher.isAlreadyPresent(person("Cy Sample", emails = listOf("cy@example.com"))))
    }

    @Test
    fun theSameNameWithAnotherAddressIsAnotherPerson() {
        val matcher = ContactMatcher(listOf(person("Cy Sample", emails = listOf("cy@example.com"))))
        assertFalse(matcher.isAlreadyPresent(person("Cy Sample")))
        assertFalse(matcher.isAlreadyPresent(person("Cy Sample", emails = listOf("cy@example.org"))))
    }

    @Test
    fun noNameMatchesNoName() {
        assertFalse(ContactMatcher(listOf(person(""))).isAlreadyPresent(person("  ")))
    }

    @Test
    fun aSummaryIsReadAsAnIdentity() {
        val summary = TeaNodeJson.decodeFromString(ContactSummary.serializer(), """{"id":"c1","name":"Dee Sample","emails":["dee@example.com"],"phones":["555 010 9999"]}""")
        assertEquals(person("Dee Sample", listOf("dee@example.com"), listOf("555 010 9999")), summary.identity)
        assertEquals(person(""), TeaNodeJson.decodeFromString(ContactSummary.serializer(), """{"id":"c2"}""").identity)
    }

    @Test
    fun thePhotoIsRemovedFromACardWithTheLinesFoldedUnderIt() {
        val card = listOf(
            "BEGIN:VCARD", "VERSION:3.0", "N:Sample;Ada;;;", "FN:Ada Sample", "PHOTO;ENCODING=b;TYPE=JPEG:/9j/4AAQSkZJRgABAQAAAQABAAD",
            " AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "\tAAAA", "EMAIL;TYPE=INTERNET:ada@example.com", "END:VCARD", "",
        ).joinToString("\r\n")
        assertEquals(
            listOf("BEGIN:VCARD", "VERSION:3.0", "N:Sample;Ada;;;", "FN:Ada Sample", "EMAIL;TYPE=INTERNET:ada@example.com", "END:VCARD", "").joinToString("\r\n"),
            ContactCard.removingPhoto(card),
        )
    }

    @Test
    fun aGroupedPhotoGoesAndAFoldedNoteStays() {
        val card = listOf("BEGIN:VCARD", "item1.photo:AAAA", "NOTE:Met at the", " fair", "PHOTOGRAPHER:not a photo", "END:VCARD").joinToString("\n")
        assertEquals(listOf("BEGIN:VCARD", "NOTE:Met at the", " fair", "PHOTOGRAPHER:not a photo", "END:VCARD").joinToString("\n"), ContactCard.removingPhoto(card))
    }

    @Test
    fun aCardWithoutAPhotoIsUnchanged() {
        val card = "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ada Sample\r\nEND:VCARD\r\n"
        assertEquals(card, ContactCard.removingPhoto(card))
    }

    @Test
    fun aCardTooLargeForTheServerDoesNotFit() {
        assertTrue(ContactCard.fits("BEGIN:VCARD\r\nEND:VCARD\r\n"))
        assertFalse(ContactCard.fits("x".repeat(ContactCard.MAXIMUM_BYTE_COUNT + 1)))
    }

    @Test
    fun theImportDocumentSendsTheWholeCard() {
        assertTrue(ContactImportDocuments.IMPORT_CONTACT.contains("\$card: String!"))
        assertTrue(ContactImportDocuments.IMPORT_CONTACT.contains("SaveContact(addressBookId: \$addressBookId, card: \$card)"))
    }
}
