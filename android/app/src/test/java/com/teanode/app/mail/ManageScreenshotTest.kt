package com.teanode.app.mail

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import com.teanode.app.demo.DemoHost
import com.teanode.app.mail.manage.AllMailScreen
import com.teanode.app.mail.manage.AuditedMailScreen
import com.teanode.app.mail.manage.DeliveryQueueScreen
import com.teanode.app.mail.manage.DomainScreen
import com.teanode.app.mail.manage.DomainsScreen
import com.teanode.app.mail.manage.ManageOverviewScreen
import com.teanode.app.mail.manage.ReportDetailScreen
import com.teanode.app.mail.manage.ReportsScreen
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of the operator's mail pages in demo mode: all mail, one message, the queue, the reports and the domains. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ManageScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun show(look: Look, fontScale: Float = 1f, waitsFor: String, name: String, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { content() } } }
        composeRule.waitUntil(10_000) { composeRule.onAllNodesWithText(waitsFor, substring = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.waitForIdle()
        composeRule.photograph("mail-manage-$name-${if (look == Look.Light) "light" else "dark"}${if (fontScale > 1f) "-largest-text" else ""}")
    }

    @Test fun `overview`() = show(Look.Light, waitsFor = "Queue", name = "overview") { ManageOverviewScreen({}, {}, {}, {}, {}) }
    @Test fun `overview dark`() = show(Look.Dark, waitsFor = "Queue", name = "overview") { ManageOverviewScreen({}, {}, {}, {}, {}) }
    @Test fun `all mail`() = show(Look.Light, waitsFor = "Lunch on Friday?", name = "all-mail") { AllMailScreen({}, {}) }
    @Test fun `all mail dark`() = show(Look.Dark, waitsFor = "Lunch on Friday?", name = "all-mail") { AllMailScreen({}, {}) }
    @Test fun `all mail largest text`() = show(Look.Light, 2f, "Lunch on Friday?", "all-mail") { AllMailScreen({}, {}) }
    @Test fun `one message`() = show(Look.Light, waitsFor = "Authentication", name = "message") { AuditedMailScreen("mail-3", {}) }
    @Test fun `one message dark`() = show(Look.Dark, waitsFor = "Authentication", name = "message") { AuditedMailScreen("mail-3", {}) }
    @Test fun `queue`() = show(Look.Light, waitsFor = "sam@example.org", name = "queue") { DeliveryQueueScreen({}, {}) }
    @Test fun `queue dark`() = show(Look.Dark, waitsFor = "sam@example.org", name = "queue") { DeliveryQueueScreen({}, {}) }
    @Test fun `reports`() = show(Look.Light, waitsFor = "198.51.100.9", name = "reports") { ReportsScreen({}, {}) }
    @Test fun `report`() = show(Look.Light, waitsFor = "Example Receiver", name = "report") { ReportDetailScreen("report-2", {}) }
    @Test fun `domains`() = show(Look.Light, waitsFor = "example.net", name = "domains") { DomainsScreen({}, {}) }
    @Test fun `domains dark`() = show(Look.Dark, waitsFor = "example.net", name = "domains") { DomainsScreen({}, {}) }
    @Test fun `domain`() = show(Look.Light, waitsFor = "Aliases", name = "domain") { DomainScreen("domain-1", {}) }
    @Test fun `domain dark`() = show(Look.Dark, waitsFor = "Aliases", name = "domain") { DomainScreen("domain-1", {}) }
}
