package com.teanode.kit.devicesetup

import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeError
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class DeviceSetupTest {
    private val capabilities = DeviceSetupCapabilities(
        canSetUpMail = true, canSetUpCalendar = true, canSetUpReminders = false, canSetUpContacts = true,
        mailboxes = listOf(DeviceSetupMailbox("mb1", "Sam", listOf(DeviceSetupAddress("sam@example.com")))),
    )

    @Test
    fun theSelectionStartsFromWhatIsAvailable() {
        val selection = DeviceSetupSelection.starting("Pixel", capabilities)
        assertFalse(selection.hasMailAccess)
        assertTrue(selection.hasCalendarAccess)
        assertFalse(selection.hasRemindersAccess)
        assertTrue(DeviceSetupSelection.starting("Pixel", capabilities, "mb1").hasMailAccess)
    }

    @Test
    fun aProfileNeedsANameAndAService() {
        assertTrue(DeviceSetupSelection.starting("Pixel", capabilities).canCreateProfile(capabilities))
        assertFalse(DeviceSetupSelection.starting(" ", capabilities).canCreateProfile(capabilities))
        assertFalse(DeviceSetupSelection("Pixel").canCreateProfile(capabilities))
        assertFalse(DeviceSetupSelection("Pixel", hasRemindersAccess = true).canCreateProfile(capabilities))
        assertFalse(DeviceSetupSelection("x".repeat(129), hasCalendarAccess = true).canCreateProfile(capabilities))
    }

    @Test
    fun aServiceThatWentAwayIsDropped() {
        val kept = DeviceSetupSelection("Pixel", "mb1", true, true, false, true).retainingAvailableServices(capabilities.copy(canSetUpMail = false, canSetUpContacts = false))
        assertFalse(kept.hasMailAccess)
        assertEquals(null, kept.mailboxId)
        assertFalse(kept.hasContactsAccess)
        assertTrue(kept.hasCalendarAccess)
    }

    @Test
    fun onlyTheServersOwnUnexpiredProfileAddressIsAccepted() {
        val server = ServerAddress.parse("mail.example.com")
        val path = "/api/v1/mailbox/profile/abc.mobileconfig"
        val good = CreatedDeviceSetupProfile(path, "https://mail.example.com$path", "2999-01-01T00:00:00Z")
        assertEquals(good.downloadLink, good.downloadAddress(server))
        assertThrows(TeaNodeError::class.java) { good.copy(downloadLink = "https://other.example.com$path").downloadAddress(server) }
        assertThrows(TeaNodeError::class.java) { good.copy(downloadLink = "https://mail.example.com$path?x=1").downloadAddress(server) }
        assertThrows(TeaNodeError::class.java) { good.copy(expiresAt = "2000-01-01T00:00:00Z").downloadAddress(server) }
    }
}
