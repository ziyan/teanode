package com.teanode.app.notifications

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.shell.AppShell
import com.teanode.kit.notifications.NotificationDestination
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class NotificationOpeningTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test fun `a conversation opens inside the chats`() {
        assertEquals(
            listOf("agent/home", "agent/conversation/c%201"),
            NotificationOpening.routesOf(NotificationDestination.Conversation("c 1")),
        )
    }

    @Test fun `a shopping press opens the store before the list`() {
        assertEquals(
            listOf("shopping/home", "shopping/store/Corner%20Shop"),
            NotificationOpening.routesOf(NotificationDestination.Shopping("Corner Shop", "list-1")),
        )
        assertEquals(listOf("shopping/home", "shopping/list/list-1"), NotificationOpening.routesOf(NotificationDestination.Shopping(null, "list-1")))
        assertEquals(listOf("shopping/home"), NotificationOpening.routesOf(NotificationDestination.Shopping(null, null)))
    }

    @Test fun `a coding question opens its session and a journal question its day`() {
        assertEquals(listOf("coding/home", "coding/session/laptop/pane-1"), NotificationOpening.routesOf(NotificationDestination.Coding("laptop", "pane-1")))
        assertEquals(listOf("coding/home"), NotificationOpening.routesOf(NotificationDestination.CodingHome))
        assertEquals(listOf("journal/home", "journal/day/2026-03-14"), NotificationOpening.routesOf(NotificationDestination.JournalDay("2026-03-14")))
    }

    @Test fun `an invitation opens the circles and a person opens the map`() {
        assertEquals(listOf("settings/home", "settings/circles"), NotificationOpening.routesOf(NotificationDestination.Circles))
        assertEquals(listOf("location/home"), NotificationOpening.routesOf(NotificationDestination.Location("sam-example")))
    }

    @Test fun `a pressed notification is taken by the shell, so the same press opens again`() {
        composeRule.setContent { DemoHost { AppShell() } }
        repeat(2) {
            NotificationOpening.open(NotificationDestination.Circles)
            composeRule.waitUntil(timeoutMillis = 15_000) { NotificationOpening.destination.value == null }
            composeRule.waitUntil(timeoutMillis = 15_000) { composeRule.onAllNodesWithText("Circles", substring = true).fetchSemanticsNodes().isNotEmpty() }
            assertNull(NotificationOpening.destination.value)
        }
    }
}
