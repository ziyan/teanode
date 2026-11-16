package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class PasswordsLockPolicyTest {
    private val start = 1_800_000_000_000L

    private fun seconds(count: Long) = count * 1000

    @Test fun `never used asks at once`() {
        assertTrue(PasswordsLockPolicy.isAskRequired(15, null, start))
    }

    @Test fun `asks once the lock time has passed`() {
        assertFalse(PasswordsLockPolicy.isAskRequired(15, start, start))
        assertFalse(PasswordsLockPolicy.isAskRequired(15, start, start + seconds(14 * 60 + 59)))
        assertTrue(PasswordsLockPolicy.isAskRequired(15, start, start + seconds(15 * 60)))
        assertTrue(PasswordsLockPolicy.isAskRequired(15, start, start + seconds(86_400)))
    }

    @Test fun `a clock set back asks but a small skew does not`() {
        assertFalse(PasswordsLockPolicy.isAskRequired(15, start, start - seconds(2)))
        assertTrue(PasswordsLockPolicy.isAskRequired(15, start, start - seconds(3600)))
    }

    @Test fun `the lock time is kept within what the server accepts`() {
        assertEquals(1, PasswordsLockPolicy.clamped(0))
        assertEquals(10080, PasswordsLockPolicy.clamped(99_999))
        assertTrue(PasswordsLockPolicy.isAskRequired(0, start, start + seconds(61)))
        assertEquals(start + seconds(1800), PasswordsLockPolicy.lockAtEpochMS(30, start))
    }
}
