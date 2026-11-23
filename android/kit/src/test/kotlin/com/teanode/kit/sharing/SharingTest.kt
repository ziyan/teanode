package com.teanode.kit.sharing

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class SharingTest {
    private fun made(kind: String, scopes: List<String> = emptyList(), circle: String? = null) =
        SharingMade(kind, "s1", granteeUsername = "sam", granteeName = "", granteeCircleId = circle, granteeCircleName = "Family", shareScopes = scopes)

    @Test
    fun passwordsAreNeverStoppedFromTheList() {
        assertFalse(made("passwords").canStop)
        assertTrue(made("calendar").canStop)
        assertFalse(made("something_new").canStop)
    }

    @Test
    fun theGranteeIsTheCircleOrThePerson() {
        assertEquals("sam", made("calendar").shownGrantee)
        assertEquals("Family", made("calendar", circle = "c1").shownGrantee)
    }

    @Test
    fun allFinanceIsAFinanceShareWithTheAllAccountsScope() {
        assertTrue(made("finance", listOf("allAccounts")).isAllFinance)
        assertFalse(made("calendar", listOf("allAccounts")).isAllFinance)
    }

    @Test
    fun aReceivedShareNamesItsOwner() {
        val received = SharingReceived("calendar", "s", ownerUserId = "u", ownerUsername = "alex", isDirect = true, canDecline = true)
        assertEquals("alex", received.shownOwner)
        assertEquals("alex", received.copy(ownerName = "").shownOwner)
        assertEquals("Alex", received.copy(ownerName = "Alex").shownOwner)
    }
}
