package com.teanode.kit.access

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class AccessManagementTest {
    @Test
    fun theSessionHoldsOnlyWhatItWasGiven() {
        val session = AccessSession("u", AccessSessionPermissions(listOf("user:manage", "photos:use")))
        assertTrue(session.canManageUsers)
        assertTrue(session.canUsePhotos)
        assertFalse(session.canReadAudit)
        assertFalse(AccessSession().canManageUsers)
    }

    @Test
    fun aSearchFindsByUsernameNameOrAddress() {
        val user = AccessUser("1", "robin", "Robin Example", "robin@example.com")
        assertTrue(user.matches(" ROB "))
        assertTrue(user.matches("example.com"))
        assertTrue(user.matches(""))
        assertFalse(user.matches("zzz"))
        assertEquals("robin", user.copy(name = null).displayName)
    }

    @Test
    fun aNewPersonWithoutAPasswordSendsNull() {
        val draft = AccessUserDraft(username = " sam ")
        assertEquals(JsonPrimitive("sam"), draft.createVariables["username"])
        assertEquals(kotlinx.serialization.json.JsonNull, draft.createVariables["password"])
        assertFalse(AccessUserDraft().canSubmit)
    }

    @Test
    fun aRoleKeepsTheServersOrderAndDropsWhatItDoesNotList() {
        val catalog = listOf(AccessPermission("a", "server"), AccessPermission("b", "server"), AccessPermission("c", "domain"))
        val role = AccessRole("r", "Reader", permissions = listOf("c", "a", "gone"))
        assertEquals(listOf("a", "c"), role.permissionsSetting("b", false, catalog))
        assertEquals(listOf("a", "b", "c"), role.permissionsSetting("b", true, catalog))
        assertEquals(listOf("a"), role.permissionsSetting("c", false, catalog))
    }

    @Test
    fun anAuditEventShowsOnlyTheFieldsThatDiffer() {
        val event = AuditEvent(
            "e", "2030-01-01T00:00:00Z", "agent", null, null, null, null, "user", "u", null, null, "update",
            JsonObject(mapOf("name" to JsonPrimitive("A"), "email" to JsonPrimitive("x"))), JsonObject(mapOf("name" to JsonPrimitive("B"), "email" to JsonPrimitive("x"))),
        )
        assertEquals(listOf("name"), event.changedFields)
        assertEquals("agent", event.actorName)
        assertTrue(event.isByAgent)
        assertEquals("A", AuditEvent.describe(event.beforeValue("name")))
        assertNull(AuditEvent.describe(JsonPrimitive("")))
        assertNull(AuditEvent.describe(null))
    }
}
