package com.teanode.kit

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** The vectors the server's InitialColor and the dashboard's avatarColorIndexOf are checked against, so the three cannot drift apart. */
class AvatarColorTest {
    @Test
    fun matchesTheSharedVectors() {
        assertEquals(10, AvatarColor.index("01JABCDEFGHJKMNPQRSTVWXYZ0"))
        assertEquals(4, AvatarColor.index("01HZZZZZZZZZZZZZZZZZZZZZZZ"))
        assertEquals(0, AvatarColor.index("user-a"))
    }

    @Test
    fun theServersIndexWinsWhenItIsUsable() {
        assertEquals(7, AvatarColor.index(7, "user-a"))
        assertEquals(0, AvatarColor.index(0, "01JABCDEFGHJKMNPQRSTVWXYZ0"))
    }

    @Test
    fun aMissingOrOutOfRangeIndexFallsBackToTheText() {
        assertEquals(10, AvatarColor.index(null, "01JABCDEFGHJKMNPQRSTVWXYZ0"))
        assertEquals(4, AvatarColor.index(12, "01HZZZZZZZZZZZZZZZZZZZZZZZ"))
        assertEquals(0, AvatarColor.index(-1, "user-a"))
    }

    @Test
    fun everyTextLandsOnOneOfTheTwelveColors() {
        for (text in listOf("", "a", "中文", "01JZZZZZZZZZZZZZZZZZZZZZZZ")) {
            assertTrue(AvatarColor.index(text) in 0 until AvatarColor.COLOR_COUNT)
        }
    }
}
