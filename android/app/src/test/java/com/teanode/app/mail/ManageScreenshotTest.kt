package com.teanode.app.mail

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import com.teanode.app.demo.DemoHost
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import com.teanode.app.mail.manage.AuditSubPage
import com.teanode.app.mail.manage.AuditSubPageScreen
import com.teanode.app.mail.manage.AuthenticationRows
import com.teanode.app.mail.manage.DomainSubPage
import com.teanode.app.mail.manage.DomainSubPageScreen
import com.teanode.app.mail.manage.FeedbackRecordPage
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.mail.DMARCReport
import com.teanode.kit.mail.DNSRecord
import com.teanode.kit.mail.DomainCredential
import com.teanode.kit.mail.MailAuthenticationResults
import com.teanode.kit.mail.MailContent
import com.teanode.kit.mail.MailDelivery
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

    private val authenticationJson = """
        {"senderMx":{"domain":"examp1e-login.test","mailServers":["mx1.examp1e-login.test","mx2.examp1e-login.test"]},
         "spf":{"domain":"examp1e-login.test","ip":"203.0.113.7","result":"softfail"},
         "dmarc":{"domain":"examp1e-login.test","policy":"reject","subdomainPolicy":"reject","dkimAlignment":"r","spfAlignment":"s","result":"fail"},
         "dkims":[{"domain":"examp1e-login.test","selector":"mail","identifier":"@examp1e-login.test","result":"fail"}],"arc":{"result":"pass","instances":2},
         "spamFilter":{"score":9.1,"result":"fail","symbols":["PHISHING"],"checks":[{"symbol":"PHISHING","score":6.0,"description":"Looks like a phishing page"},{"symbol":"FORGED_SENDER","score":3.1,"description":"The sender does not match the signing domain"},{"symbol":"HAS_LIST_UNSUBSCRIBE","score":-0.1}]},
         "antivirus":{"viruses":["Example.Test.Virus"]},"contentFilter":{"unsafeExtensions":["exe"]},"errors":["the DNS lookup timed out"]}
    """.trimIndent()
    private val authentication = TeaNodeJson.decodeFromString(MailAuthenticationResults.serializer(), authenticationJson)
    private val delivery = TeaNodeJson.decodeFromString(
        MailDelivery.serializer(),
        """{"id":"delivery-9","mailId":"mail-4","recipient":"sam@example.org","kind":"outgoing","status":"failed","size":3100,"attempts":3,"error":"451 try again later","method":"mailServer","destination":"mx.example.org","attemptedAt":"2030-01-02T10:00:00Z","retryAt":"2030-01-02T11:00:00Z","deliveryStatuses":[{"reportingMta":"mx.example.org","recipientStatuses":[{"finalRecipient":"sam@example.org","action":"delayed","status":"4.2.2","remoteMta":"mx.example.org","diagnosticCode":"smtp; 452 mailbox full"}]}]}""",
    )
    private val record = TeaNodeJson.decodeFromString(
        DNSRecord.serializer(), """{"type":"TXT","name":"mail._domainkey.example.com","expected":"v=DKIM1; k=rsa; p=EXAMPLEKEY","priority":0,"optional":false,"found":["v=DKIM1; k=rsa; p=OLDKEY"],"verified":false,"purpose":"Signs outgoing mail","blocked":"the selector is not published yet"}""",
    )

    @Test fun `authentication checks`() = show(Look.Light, waitsFor = "Spam filter rules", name = "authentication") { Column(androidx.compose.ui.Modifier.verticalScroll(rememberScrollState())) { AuthenticationRows(authentication) } }
    @Test fun `authentication checks dark`() = show(Look.Dark, waitsFor = "Spam filter rules", name = "authentication") { Column(androidx.compose.ui.Modifier.verticalScroll(rememberScrollState())) { AuthenticationRows(authentication) } }
    @Test fun `spam rules`() = show(Look.Light, waitsFor = "FORGED_SENDER", name = "spam-rules") { AuditSubPageScreen(AuditSubPage.SpamRules(authentication.spamFilter?.checks.orEmpty()), {}) }
    @Test fun `delivery detail`() = show(Look.Light, waitsFor = "mailbox full", name = "delivery") { AuditSubPageScreen(AuditSubPage.DeliveryDetail(delivery), {}) }
    @Test fun `delivery detail dark`() = show(Look.Dark, waitsFor = "mailbox full", name = "delivery") { AuditSubPageScreen(AuditSubPage.DeliveryDetail(delivery), {}) }
    @Test fun `headers`() = show(Look.Light, waitsFor = "Received", name = "headers") { AuditSubPageScreen(AuditSubPage.Headers(listOf(MailContent.Header("Received", "from mx.example.net by mail.example.com; Tue, 1 Jan 2030 10:00:00 +0000"), MailContent.Header("Subject", "Verify your account now"))), {}) }
    @Test fun `source text`() = show(Look.Light, waitsFor = "Please verify", name = "source") { AuditSubPageScreen(AuditSubPage.Source("Text", "Please verify your account at once.", false), {}) }
    @Test fun `report record`() = show(Look.Light, waitsFor = "Evaluated as", name = "report-record") {
        val feedbackRecord = TeaNodeJson.decodeFromString(
            DMARCReport.FeedbackRecord.serializer(),
            """{"sourceIp":"203.0.113.7","count":3,"disposition":"reject","dkim":"fail","spf":"fail","reasonType":"forwarded","reasonComment":"a list","headerFrom":"example.com","envelopeFrom":"examp1e-login.test","envelopeTo":"example.org","dkims":[{"domain":"example.com","selector":"mail","result":"fail","humanResult":"signature did not verify"}],"spfs":[{"domain":"examp1e-login.test","scope":"mfrom","result":"fail"}]}""",
        )
        FeedbackRecordPage(feedbackRecord, {})
    }
    @Test fun `dns record`() = show(Look.Light, waitsFor = "Required value", name = "dns-record") { DomainSubPageScreen(DomainSubPage.Record(record), "example.com", {}, {}) }
    @Test fun `dns record dark`() = show(Look.Dark, waitsFor = "Required value", name = "dns-record") { DomainSubPageScreen(DomainSubPage.Record(record), "example.com", {}, {}) }
    @Test fun `edit credential`() = show(Look.Light, waitsFor = "Edit credential", name = "credential") { DomainSubPageScreen(DomainSubPage.Credential(DomainCredential("credential-1", "Printer", "scanner", true)), "example.com", {}, {}) }
    @Test fun `edit credential dark`() = show(Look.Dark, waitsFor = "Edit credential", name = "credential") { DomainSubPageScreen(DomainSubPage.Credential(DomainCredential("credential-1", "Printer", "scanner", false)), "example.com", {}, {}) }
    @Test fun `template`() = show(Look.Light, waitsFor = "Sent when somebody joins", name = "template") { DomainSubPageScreen(DomainSubPage.Template("template-1", "Plain layout"), "example.com", {}, {}) }
    @Test fun `layout`() = show(Look.Light, waitsFor = "What it is for", name = "layout") { DomainSubPageScreen(DomainSubPage.Layout("layout-1"), "example.com", {}, {}) }
}
