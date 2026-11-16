package com.teanode.kit.agent

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.LocalDate

class AgentFollowUpsTest {
    @Test
    fun followUpsComeNearestDayFirstAndThoseWithNoDayLast() {
        val ordered = AgentFollowUp.ordered(
            listOf(AgentFollowUp("a"), AgentFollowUp("b", dueOn = "2030-03-02"), AgentFollowUp("c", dueOn = "2030-03-01"), AgentFollowUp("d", dueOn = "2030-03-02")),
        )
        assertEquals(listOf("c", "b", "d", "a"), ordered.map { followUp -> followUp.id })
    }

    @Test
    fun aBillNeedsATitleADayAndAnAmount() {
        val bill = NewAgentFollowUp(followUpKind = "bill", followUpTitle = "Water", dueOn = "2030-03-01", amount = "20")
        assertTrue(bill.canAdd)
        assertFalse(bill.copy(amount = " ").canAdd)
        assertFalse(bill.copy(dueOn = "").canAdd)
        assertFalse(bill.copy(followUpTitle = "  ").canAdd)
    }

    @Test
    fun aParcelNeedsNoDay() {
        assertTrue(NewAgentFollowUp(followUpKind = "parcel", followUpTitle = "Books").canAdd)
        assertFalse(NewAgentFollowUp(followUpKind = "deadline", followUpTitle = "Form").canAdd)
    }

    @Test
    fun onlyTheFieldsOfTheKindAreSent() {
        val sent = NewAgentFollowUp(followUpKind = "parcel", followUpTitle = " Books ", dueOn = "", payee = "Left over", carrier = "Post", trackingNumber = "X1", place = "Nowhere").variables
        assertEquals(setOf("followUpKind", "followUpTitle", "carrier", "trackingNumber"), sent.keys)
        assertEquals("\"Books\"", sent["followUpTitle"].toString())
    }

    @Test
    fun aDayReadsAsThatDayAndNotAsOneElse() {
        assertEquals(LocalDate.of(2030, 3, 1), AgentFollowUp("a", dueOn = "2030-03-01").dueDate)
        assertNull(AgentFollowUp("a", dueOn = "soon").dueDate)
        assertNull(AgentFollowUp("a").dueDate)
    }

    @Test
    fun onlyWebPagesAreLinked() {
        assertEquals("example.com", AgentFollowUp("a", sourceUrl = "https://example.com/events").sourcePageHost)
        assertEquals("https://example.com/events", AgentFollowUp("a", sourceUrl = "https://example.com/events").sourcePageUrl)
        assertNull(AgentFollowUp("a", sourceUrl = "javascript:alert(1)").sourcePageUrl)
        assertNull(AgentFollowUp("a", sourceUrl = "file:///etc/hosts").sourcePageUrl)
        assertNull(AgentFollowUp("a").sourcePageUrl)
    }

    @Test
    fun theAnswerReadsAsTheServerWritesIt() {
        val followUp = TeaNodeJson.decodeFromString<AgentFollowUp>("""{"id":"f","followUpKind":"bill","followUpTitle":"Water","followUpState":"overdue"}""")
        assertTrue(followUp.isOverdue)
        assertFalse(followUp.isStalled)
    }
}
