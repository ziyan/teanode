package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant

class DevicePasswordsTest {
    private data class Password(val id: String, override val passwordCreatedAt: String?, override val passwordLastUsedAt: String? = null) : DevicePassword

    @Test fun `newest first orders by creation time and puts unreadable last`() {
        val passwords = listOf(
            Password("old", "2030-01-01T00:00:00Z"),
            Password("unknown", null),
            Password("new", "2030-03-01T00:00:00Z"),
        )
        assertEquals(listOf("new", "old", "unknown"), passwords.newestFirst().map { password -> password.id })
    }

    @Test fun `a password never used after an hour or idle for ninety days may be safe to delete`() {
        val now = Instant.parse("2030-06-01T12:00:00Z")
        assertFalse(Password("p", "2030-06-01T11:30:00Z").isMaybeSafeToDelete(now))
        assertTrue(Password("p", "2030-06-01T10:30:00Z").isMaybeSafeToDelete(now))
        assertFalse(Password("p", "2029-01-01T00:00:00Z", "2030-05-01T00:00:00Z").isMaybeSafeToDelete(now))
        assertTrue(Password("p", "2029-01-01T00:00:00Z", "2030-01-01T00:00:00Z").isMaybeSafeToDelete(now))
    }

    @Test fun `a time with fractional seconds reads`() {
        val now = Instant.parse("2030-06-01T12:00:00Z")
        assertTrue(Password("p", "2030-02-01T00:00:00.123456789Z").isMaybeSafeToDelete(now))
    }
}
