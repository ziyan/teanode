package com.teanode.kit.chats

import com.teanode.kit.ServerAddress
import com.teanode.kit.recordings.RecordingLink
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import java.time.LocalDate

class AppLinksTest {
    private val server = ServerAddress.parse("https://mail.example.com")

    @Test
    fun aWebPageOpensInTheBrowserAndAnAppLinkIsTheSystems() {
        assertEquals(AppLinkTarget.WebPage("https://www.example.org/a"), AppLinks.forLink("https://www.example.org/a", server))
        assertEquals(AppLinkTarget.System("https://teanode.com/app/connect"), AppLinks.forLink("https://teanode.com/app/connect", server))
    }

    @Test
    fun aRecordingOfThisServerOpensItsPassage() {
        val target = AppLinks.forLink("https://mail.example.com/recordings/abc123?version=run1&startMS=1000&endMS=2000", server)
        assertEquals(AppLinkTarget.Recording(RecordingLink("abc123", "run1", 1000, 2000)), target)
        assertEquals(AppLinkTarget.Recording(RecordingLink("abc123", null, null, null)), AppLinks.forLink("/recordings/abc123", server))
        // Another server's recording is only a web page.
        assertEquals(AppLinkTarget.WebPage("https://other.example.net/recordings/abc123"), AppLinks.forLink("https://other.example.net/recordings/abc123", server))
    }

    @Test
    fun aMailLinkNeedsAnIdentifier() {
        assertEquals(AppLinkTarget.MailItem("a1b2"), AppLinks.forLink("mail:a1b2", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forLink("mail:", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forLink("mail:a/b", server))
    }

    @Test
    fun aMemoryLinkIsOwnOrShared() {
        assertEquals(AppLinkTarget.MemoryPage("people/ada"), AppLinks.forLink("memory:people/ada#2", server))
        assertEquals(AppLinkTarget.SharedMemoryPage("sam.example", "projects/tea"), AppLinks.forLink("memory:projects/tea#1@sam.example", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forLink("memory:People/Ada", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forLink("memory:a/b@", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forLink("memory:", server))
    }

    @Test
    fun anythingElseIsNotFollowed() {
        assertEquals(AppLinkTarget.Discarded, AppLinks.forLink("javascript:alert(1)", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forLink("intent://example", server))
    }

    @Test
    fun theLocationPageMayNameADay() {
        assertEquals(AppLinkTarget.Location(null), AppLinks.forPath("/location", server))
        assertEquals(AppLinkTarget.Location(LocalDate.of(2030, 1, 15)), AppLinks.forPath("/location?day=2030-01-15", server))
        // A month that does not exist is not a day.
        assertEquals(AppLinkTarget.Location(null), AppLinks.forPath("/location?day=2030-13-15", server))
    }

    @Test
    fun theHealthPageMayNameADataKindOrTheSettings() {
        assertEquals(AppLinkTarget.Health(null, false), AppLinks.forPath("/health", server))
        assertEquals(AppLinkTarget.Health("stepCount", false), AppLinks.forPath("/health?type=stepCount", server))
        assertEquals(AppLinkTarget.Health(null, true), AppLinks.forPath("/health?tab=settings", server))
        assertEquals(AppLinkTarget.Health(null, false), AppLinks.forPath("/health?type=step-count", server))
        assertNull(AppLinks.healthOf("/healthy"))
    }

    @Test
    fun dashboardPathsGoToTheirPart() {
        assertEquals(AppLinkTarget.Knowledge(""), AppLinks.forPath("/knowledge", server))
        assertEquals(AppLinkTarget.Knowledge("people/ada"), AppLinks.forPath("/knowledge/people/ada", server))
        assertEquals(AppLinkTarget.Mailbox("t9"), AppLinks.forPath("/mailbox/inbox/t9", server))
        assertEquals(AppLinkTarget.Mailbox(null), AppLinks.forPath("/mailbox/compose/new", server))
        assertEquals(AppLinkTarget.Finance("networth"), AppLinks.forPath("/finance/net-worth", server))
        assertEquals(AppLinkTarget.Finance(null), AppLinks.forPath("/finance", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forPath("/finance/link", server))
        assertEquals(AppLinkTarget.Journal("2026-03-14"), AppLinks.forPath("/journal/2026-03-14", server))
        assertEquals(AppLinkTarget.Journal(""), AppLinks.forPath("/journal/2026-W11", server))
        assertEquals(AppLinkTarget.Passwords, AppLinks.forPath("/passwords/item-1", server))
        assertEquals(AppLinkTarget.Shopping, AppLinks.forPath("/shopping", server))
        assertEquals(AppLinkTarget.Settings(isMailPrograms = true), AppLinks.forPath("/settings/mailbox/devices", server))
        assertEquals(AppLinkTarget.Settings(isMailPrograms = false), AppLinks.forPath("/settings/circles", server))
        assertEquals(AppLinkTarget.Connect, AppLinks.forPath("/connect", server))
    }

    @Test
    fun aPathMustBePlain() {
        assertEquals(AppLinkTarget.Discarded, AppLinks.forPath("/knowledge/../secrets", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forPath("knowledge", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forPath("/knowledge/a b", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forPath("/unknown", server))
        assertEquals(AppLinkTarget.Discarded, AppLinks.forPath("/" + "a".repeat(700), server))
    }
}
