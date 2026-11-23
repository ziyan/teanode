package com.teanode.kit.account

import com.teanode.kit.circles.PersonPronoun
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AccountProfileTest {
    private val profile = AccountProfile("user-1", "robin", "Robin", "robin@example.com", "Europe/Berlin", "fixed")

    @Test
    fun anUntouchedProfileHasNothingToSave() {
        val draft = ProfileDraft(profile)
        assertFalse(draft.hasChanges)
        assertFalse(draft.canSave)
    }

    @Test
    fun spacesAroundTheWordsAreNoChange() {
        assertFalse(ProfileDraft(profile, name = "  Robin ", username = "robin ").hasChanges)
    }

    @Test
    fun aNewUsernameIsARename() {
        val draft = ProfileDraft(profile, username = "robin2")
        assertTrue(draft.isRenaming)
        assertTrue(draft.canSave)
        assertFalse(draft.copy(username = "  ").canSave)
    }

    @Test
    fun followingTheBrowserSendsNoZone() {
        val draft = ProfileDraft(profile, email = " someone@example.com ").choosing(TimezoneMode.Auto, "Asia/Tokyo")
        assertEquals(JsonPrimitive(""), draft.variables["timezone"])
        assertEquals(JsonPrimitive("auto"), draft.variables["timezoneMode"])
        assertEquals(JsonPrimitive("someone@example.com"), draft.variables["email"])
    }

    @Test
    fun aFixedZoneStartsFromTheAccountsOrThePhones() {
        val withZone = ProfileDraft(AccountProfile("u", "robin", timezone = "Europe/Berlin", timezoneMode = "auto"), timezone = "")
        assertEquals("Europe/Berlin", withZone.choosing(TimezoneMode.Fixed, "Asia/Tokyo").timezone)
        assertEquals("Asia/Tokyo", ProfileDraft(AccountProfile("u", "robin")).choosing(TimezoneMode.Fixed, "Asia/Tokyo").timezone)
        assertEquals("America/Denver", ProfileDraft(AccountProfile("u", "robin"), timezone = "America/Denver").choosing(TimezoneMode.Fixed, "Asia/Tokyo").timezone)
    }

    @Test
    fun theProfileKnowsHowThePersonIsDescribed() {
        assertEquals(PersonPronoun.She, AccountProfile("u", "robin", pronoun = "she").chosenPronoun)
        assertEquals(PersonPronoun.NotSet, AccountProfile("u", "robin", pronoun = "xyz").chosenPronoun)
        assertEquals(MeasurementSystem.Us, AccountProfile("u", "robin", measurementSystem = "us").chosenMeasurementSystem)
    }

    @Test
    fun aPasswordChangeNeedsTheSameNewPasswordTwice() {
        assertFalse(PasswordChangeDraft("old", "new", "other").canChange)
        assertTrue(PasswordChangeDraft("old", "new", "new").canChange)
        assertTrue(PasswordChangeDraft("old", "new", "x").doesConfirmationDiffer)
    }

    @Test
    fun aPhoneSavesOnlyWithANameAndAChange() {
        val phone = PairedPhone("p", "Pixel", "model", notificationKinds = listOf("mail", "chat"), canBeNotified = true)
        assertFalse(PairedPhoneDraft(phone).canSave)
        assertTrue(PairedPhoneDraft(phone, notificationKinds = setOf("mail")).canSave)
        assertFalse(PairedPhoneDraft(phone, phoneName = " ").canSave)
        assertEquals(listOf("mail", "chat"), PairedPhoneDraft(phone, notificationKinds = setOf("chat", "mail")).orderedNotificationKinds(listOf("mail", "calendar", "chat")))
        assertEquals(PhoneNotificationState.Notified, phone.notificationState)
        assertEquals(PhoneNotificationState.LastFailed, phone.copy(lastDeliveryError = "gone").notificationState)
    }
}
