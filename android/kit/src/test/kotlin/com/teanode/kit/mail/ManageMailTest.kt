package com.teanode.kit.mail

import com.teanode.kit.FakeServer
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.connectionFor
import com.teanode.kit.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** The operator's mail pages: what the server's answers read as, what is sent to change them, and what the pages decide from them. */
class ManageMailTest {
    @Test
    fun readsWhetherThePersonIsAnOperatorAndWhichPagesTheyMayOpen() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"GetSession":{"manages":true,"permissions":{"everywhere":["report:read","domain:manage-all"],"byDomain":[{"domainId":"domain-1","permissions":["queue:manage"]}]}}}}"""
        }
        val access = connectionFor(server).operatorAccess()
        assertTrue(access.isOperator)
        assertTrue(access.canReadReports)
        // An all-domains permission stands in for the domain permission, and one held over one domain is enough.
        assertTrue(access.canManageDomains)
        assertTrue(access.canManageQueue)
        assertFalse(access.canAuditMail)
        assertTrue(access.canOpenMailDelivery)
    }

    @Test
    fun somebodyWithoutPermissionsIsNoOperator() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"GetSession":{"manages":false,"permissions":null}}}""" }
        assertFalse(connectionFor(server).isOperator())
        assertFalse(OperatorAccess(isOperator = false).canOpenMailDelivery)
        assertFalse(OperatorAccess(isOperator = true, everywherePermissions = listOf("mail:audit-allx")).canAuditMail)
    }

    @Test
    fun asksForTheNewestMailOfEveryDomainAndReadsTheRows() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"ListMails":[{"id":"mail-1","domainId":"domain-1","sender":"bounce@example.net","from":"","subject":"Hello","status":"accepted","kind":"incoming","size":2048,"receivedAt":"2030-01-02T03:04:05Z"},null]}}"""
        }
        val mails = connectionFor(server).allMail()
        assertEquals(listOf("mail-1"), mails.map { mail -> mail.id })
        assertEquals("accepted", mails[0].mailStatus)
        assertEquals(2048, mails[0].byteCount)
        // With no From header the envelope sender stands in.
        assertEquals("bounce@example.net", mails[0].shownSender)
        val variables = sentVariables(server.sent[0])
        assertEquals(JsonPrimitive(""), variables["domainId"])
        assertEquals(JsonObject(mapOf("first" to JsonPrimitive(500))), variables["pagination"])
    }

    @Test
    fun readsOneMessageWithItsChecksAndDeliveries() = runBlocking {
        val server = FakeServer {
            200 to """
                {"data":{"GetSpamTraining":{"label":"spam"},
                  "GetSettings":{"antispam":{"effectiveEngine":"builtin","bayesEnabled":true,"bayesMinimumMessages":200,"bayesLearnedSpam":12,"bayesLearnedHam":30}},
                  "GetMail":{"id":"mail-1","sender":"someone@example.net","from":"Someone <someone@example.net>","subject":"Invoice","recipients":["sales@example.com"],"status":"accepted","kind":"incoming","size":4096,
                    "receivedAt":"2030-01-02T03:04:05Z","ip":"192.0.2.10","rdns":"mx.example.net.","location":{"city":"Springfield","country":"Atlantis"},
                    "authenticationResults":{"spf":{"domain":"example.net","ip":"192.0.2.10","result":"pass"},"dmarc":{"domain":"example.net","policy":"reject","result":"pass"},
                      "dkims":[{"domain":"example.net","selector":"sel1","result":"pass"}],"spamFilter":{"score":1.5,"result":"pass","symbols":["A"],"checks":[{"symbol":"A","score":1.5,"description":"Looks fine"}]},"errors":null}},
                  "GetMailContent":{"mailId":"mail-1","available":true,"text":"Hello","html":"","attachments":[]},
                  "GetMailOpens":{"trackable":true,"opened":false},
                  "ListDeliveriesByMail":[{"id":"delivery-1","recipient":"sales@example.com","status":"delivered","kind":"incoming","size":4096,"attempts":1}]}}
            """
        }
        val detail = connectionFor(server).auditedMail("mail-1")
        assertEquals("spam", detail.trainingLabel)
        assertTrue(detail.classifier?.canBeTaught == true)
        assertEquals("Atlantis", detail.mail.location?.country)
        assertEquals("Springfield, Atlantis", detail.mail.location?.placeText)
        assertEquals("pass", detail.mail.authenticationResults?.spf?.spfResult)
        assertEquals(1.5, detail.mail.authenticationResults?.spamFilter?.checks?.first()?.score)
        assertEquals(listOf("delivery-1"), detail.deliveries.map { delivery -> delivery.id })
        assertEquals(MailOutcome.Delivered(1), MailOutcome.of(detail.mail, detail.deliveries))
    }

    @Test
    fun aMessageNeverTaughtHasNoLabel() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"GetSpamTraining":{"label":""},"GetMail":{"id":"mail-1"},"GetMailContent":null,"GetMailOpens":null,"ListDeliveriesByMail":null}}""" }
        val detail = connectionFor(server).auditedMail("mail-1")
        assertEquals(null, detail.trainingLabel)
        assertTrue(detail.deliveries.isEmpty())
    }

    @Test
    fun teachesTheFilterOrForgetsWhatItWasTaught() = runBlocking {
        val server = FakeServer { request ->
            if (request.body!!.toString(Charsets.UTF_8).contains("ForgetMail")) 200 to """{"data":{"ForgetMail":{"label":"","learnedSpam":11,"learnedHam":30}}}"""
            else 200 to """{"data":{"MarkMail":{"label":"spam","learnedSpam":12,"learnedHam":30}}}"""
        }
        val connection = connectionFor(server)
        assertEquals("spam", connection.teachSpamFilter("mail-1", "spam").trainingLabel)
        assertEquals("spam", (sentVariables(server.sent[0])["label"] as JsonPrimitive).content)
        assertEquals("", connection.teachSpamFilter("mail-1", null).trainingLabel)
        assertEquals(null, sentVariables(server.sent[1])["label"])
    }

    private fun rejected(results: MailAuthenticationResults) = AuditedMail(id = "mail-1", mailStatus = "rejected", authenticationResults = results)

    @Test
    fun saysWhatHappenedToAMessageInOneLine() {
        assertEquals(MailOutcome.RejectedForViruses(listOf("Test.Virus")), MailOutcome.of(rejected(MailAuthenticationResults(antivirus = MailAuthenticationResults.AntivirusCheck(listOf("Test.Virus")))), emptyList()))
        assertEquals(MailOutcome.RejectedAsSpam(11.6), MailOutcome.of(rejected(MailAuthenticationResults(spamFilter = MailAuthenticationResults.SpamFilterCheck(11.6, "fail"))), emptyList()))
        assertEquals(
            MailOutcome.RejectedByDMARC("reject"),
            MailOutcome.of(rejected(MailAuthenticationResults(dmarc = MailAuthenticationResults.DMARCCheck(domain = "example.net", policy = "reject", dmarcResult = "fail"))), emptyList()),
        )
        assertEquals(MailOutcome.RejectedWithErrors(listOf("too large")), MailOutcome.of(rejected(MailAuthenticationResults(errorMessages = listOf("too large"))), emptyList()))
        assertEquals(MailOutcome.Rejected, MailOutcome.of(rejected(MailAuthenticationResults()), emptyList()))
        val accepted = AuditedMail(id = "mail-2", mailStatus = "accepted")
        assertEquals(MailOutcome.NoDeliveries, MailOutcome.of(accepted, emptyList()))
        val delivered = MailDelivery("delivery-1", deliveryStatus = "delivered")
        val queued = MailDelivery("delivery-2", deliveryStatus = "queued")
        val dropped = MailDelivery("delivery-3", deliveryStatus = "dropped")
        assertEquals(MailOutcome.Delivered(2), MailOutcome.of(accepted, listOf(delivered, delivered)))
        assertEquals(MailOutcome.Pending(1), MailOutcome.of(accepted, listOf(delivered, queued)))
        assertEquals(MailOutcome.PartlyDelivered(1, 1, 3), MailOutcome.of(accepted, listOf(delivered, dropped, queued)))
    }

    @Test
    fun filtersMailByDomainStatusKindAndWords() {
        val mails = listOf(
            AuditedMail("one", domainId = "domain-1", from = "Robin <robin@example.net>", subject = "Quarterly report", mailStatus = "accepted", mailKind = "incoming"),
            AuditedMail("two", domainId = "domain-2", sender = "bounce@example.org", subject = "Undelivered", mailStatus = "rejected", mailKind = "dsn"),
            AuditedMail("three", domainId = "domain-1", from = "news@example.org", subject = "Weekly news", mailStatus = "rejected", mailKind = "incoming"),
        )
        fun ids(filter: MailListFilter) = filter.applied(mails).map { mail -> mail.id }
        assertFalse(MailListFilter().isFiltering)
        assertEquals(listOf("one", "two", "three"), ids(MailListFilter()))
        assertEquals(listOf("one", "three"), ids(MailListFilter(domainId = "domain-1")))
        assertEquals(listOf("three"), ids(MailListFilter(domainId = "domain-1", mailStatus = "rejected")))
        assertEquals(listOf("two"), ids(MailListFilter(mailKind = "dsn")))
        assertEquals(listOf("two"), ids(MailListFilter(searchText = "BOUNCE")))
        assertEquals(listOf("three"), ids(MailListFilter(searchText = "weekly example.org")))
    }

    @Test
    fun writesAScoreTheWayTheDashboardDoes() {
        assertEquals("11.6", ManagedWords.score(11.606000000000002))
        assertEquals("3", ManagedWords.score(3.0))
        assertEquals("-1", ManagedWords.score(-1.0))
        assertEquals("0.3", ManagedWords.score(0.25))
    }

    @Test
    fun givesStatusesTheDashboardsTones() {
        assertEquals(ManagedTone.Good, ManagedTone.of("Delivered"))
        assertEquals(ManagedTone.Bad, ManagedTone.of("hardfail"))
        assertEquals(ManagedTone.Warn, ManagedTone.of("queued"))
        assertEquals(ManagedTone.Plain, ManagedTone.of("expanded"))
        assertEquals(ManagedTone.Plain, ManagedTone.of(null))
    }

    @Test
    fun filtersTheQueue() {
        val deliveries = listOf(
            MailDelivery("one", domainId = "domain-1", recipient = "person@example.org", deliveryStatus = "queued"),
            MailDelivery("two", domainId = "domain-2", recipient = "other@example.net", deliveryStatus = "failed"),
        )
        assertEquals(listOf("two"), DeliveryQueueFilter(domainId = "domain-2").applied(deliveries).map { delivery -> delivery.id })
        assertEquals(listOf("one"), DeliveryQueueFilter(deliveryStatus = "queued").applied(deliveries).map { delivery -> delivery.id })
        assertEquals(listOf("two"), DeliveryQueueFilter(searchText = "example.net").applied(deliveries).map { delivery -> delivery.id })
        assertTrue(DeliveryQueueFilter(searchText = "x").isFiltering)
        assertTrue(deliveries[1].isFailing)
        assertFalse(deliveries[0].isFailing)
        assertTrue(MailDelivery("three", retryAt = "2030-01-01T00:00:00Z").isFailing)
    }

    @Test
    fun readsTheReportsAndTheirAlignment() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"ListReports":[{"id":"report-1","domainId":"domain-1","beginAt":"2030-01-01T00:00:00Z","endAt":"2030-01-02T00:00:00Z","count":12,"ip":"198.51.100.4","rdns":"sender.example.net.","fromDomain":"example.com","disposition":"none","dkimAligned":true,"spfAligned":false}]}}"""
        }
        val reports = connectionFor(server).dmarcReports()
        assertEquals(12, reports.first().messageCount)
        assertEquals(ReportAlignment.One, reports.first().alignment)
        assertEquals(JsonObject(mapOf("first" to JsonPrimitive(500))), sentVariables(server.sent[0])["pagination"])
        assertEquals(ReportAlignment.Both, DMARCReport("a", isDkimAligned = true, isSpfAligned = true).alignment)
        assertEquals(ReportAlignment.Neither, DMARCReport("b", isDkimAligned = false, isSpfAligned = false).alignment)
        assertFalse(ReportAlignment.Neither.isPassed)
    }

    @Test
    fun readsOneReportAsItArrived() {
        val report = TeaNodeJson.decodeFromString(
            DMARCReport.serializer(),
            """{"id":"report-1","feedback":{"organizationName":"Example Receiver","begin":1893456000,"percent":50,"policy":"reject","subdomainPolicy":"none","failureOptions":"1",
              "records":[{"sourceIp":"198.51.100.4","count":3,"disposition":"none","dkim":"pass","spf":"fail","dkims":[{"domain":"example.com","selector":"sel1","result":"pass"}],"spfs":[{"domain":"example.com","result":"fail"}]}]}}""",
        )
        assertEquals("p=reject · sp=none · pct=50 · fo=1", report.feedback?.policyText)
        assertTrue(report.feedback!!.records!!.first().isPassed)
        assertEquals("sel1", report.feedback!!.records!!.first().dkimResults!!.first().selector)
    }

    @Test
    fun filtersReports() {
        val reports = listOf(
            DMARCReport("one", domainId = "domain-1", fromDomain = "example.com", disposition = "none", isDkimAligned = true, isSpfAligned = true),
            DMARCReport("two", domainId = "domain-1", fromDomain = "example.net", disposition = "reject", isDkimAligned = false, isSpfAligned = false),
        )
        assertEquals(listOf("two"), ReportListFilter(alignment = ReportAlignment.Neither).applied(reports).map { report -> report.id })
        assertEquals(listOf("two"), ReportListFilter(disposition = "reject").applied(reports).map { report -> report.id })
        assertEquals(listOf("one"), ReportListFilter(searchText = "example.com").applied(reports).map { report -> report.id })
    }

    @Test
    fun readsTheDomainsAndTheirDNSInOneWord() {
        assertEquals(DomainDNSSummary.NotChecked, DomainDNSSummary.of(null))
        val published = DomainDNSCheck(records = listOf(DNSRecord("MX", isVerified = true), DNSRecord("TXT", isVerified = false, isOptional = true)))
        assertEquals(DomainDNSSummary.AllPublished(2), DomainDNSSummary.of(published))
        val missing = DomainDNSCheck(records = listOf(DNSRecord("MX", isVerified = true), DNSRecord("TXT", isVerified = false)))
        assertEquals(DomainDNSSummary.Missing(1), DomainDNSSummary.of(missing))
        assertEquals("10 mx.example.net", DNSRecord("MX", expectedValue = "mx.example.net", priority = 10).publishedValue)
    }

    @Test
    fun addsAnAliasSendingOnlyTheDestinationItsKindNeeds() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"CreateAlias":{"id":"alias-3"},"DeleteAlias":true}}""" }
        val connection = connectionFor(server)
        connection.createDomainAlias("domain-1", "^sales$", aliasKind = "email", destination = "team@example.net", mailboxId = "mailbox-1")
        connection.createDomainAlias("domain-1", "", aliasKind = "mailbox", destination = "left over", mailboxId = "mailbox-1")
        connection.createDomainAlias("domain-1", "^drop$", aliasKind = "null", destination = "", mailboxId = "")
        connection.deleteDomainAlias("alias-3")
        val first = sentVariables(server.sent[0])
        assertEquals(JsonPrimitive("team@example.net"), first["email"])
        assertEquals(JsonNull, first["webhook"])
        assertEquals(JsonNull, first["mailboxId"])
        assertEquals(JsonPrimitive("mailbox-1"), sentVariables(server.sent[1])["mailboxId"])
        assertEquals(JsonNull, sentVariables(server.sent[1])["email"])
        assertEquals(JsonNull, sentVariables(server.sent[2])["webhook"])
        assertEquals(JsonPrimitive("alias-3"), sentVariables(server.sent[3])["aliasId"])
    }

    @Test
    fun knowsWhichKindsAndDestinationsAnAliasEditorOffers() {
        val relay = TeaNodeJson.decodeFromString(DomainAlias.serializer(), """{"id":"alias-5","pattern":"^relay$","kind":"mailServer","mailServer":{"host":"mx.example.net","port":25}}""")
        val forward = TeaNodeJson.decodeFromString(DomainAlias.serializer(), """{"id":"alias-6","pattern":"^sales$","kind":"email","email":"team@example.net"}""")
        assertEquals(listOf("mailbox", "email", "webhook", "null", "mailServer"), relay.selectableKinds)
        assertEquals(listOf("mailbox", "email", "webhook", "null"), forward.selectableKinds)
        assertEquals("team@example.net", forward.editableDestination)
        assertEquals("", relay.editableDestination)
        assertEquals("email", forward.editorKind)
        assertEquals("", TeaNodeJson.decodeFromString(DomainAlias.serializer(), """{"id":"alias-8","kind":"carrierPigeon"}""").editorKind)
        assertEquals("", forward.editableDestination("webhook"))
        assertFalse(DomainAlias.isDestinationFilled("email", "  ", ""))
        assertTrue(DomainAlias.isDestinationFilled("webhook", "https://example.net/hook", ""))
        assertFalse(DomainAlias.isDestinationFilled("mailbox", "left over", ""))
        assertTrue(DomainAlias.isDestinationFilled("mailbox", "", "mailbox-1"))
        assertTrue(DomainAlias.isDestinationFilled("null", "", ""))
        assertFalse(DomainAlias.isDestinationFilled("", "team@example.net", ""))
    }

    @Test
    fun countsADomainsMailAndItsQueue() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"CountMailsBy":[{"value":"accepted","count":5},{"value":"rejected","count":2},{"value":"accepted","count":1}],"ListMails":[{"receivedAt":"2030-01-02T03:04:05Z"}],"ListPendingDeliveries":[{"id":"d1","status":"failed"},{"id":"d2","status":"queued"}]}}"""
        }
        val overview = connectionFor(server).domainOverview("domain-1")
        assertEquals(8, overview.totalMailCount)
        assertEquals(6, overview.acceptedMailCount)
        assertEquals(2, overview.rejectedMailCount)
        assertEquals("2030-01-02T03:04:05Z", overview.newestReceivedAt)
        assertEquals(1, overview.failingDeliveryCount)
    }

    @Test
    fun namesDKIMRecordsAndHosts() {
        assertEquals("sel1._domainkey.example.com", ManagedWords.dkimRecordName("sel1", "example.com"))
        assertEquals("example.com", ManagedWords.dkimRecordName("", "example.com"))
        assertEquals("mx.example.net", ManagedWords.hostName("mx.example.net."))
        assertEquals(listOf("a.example.net", "b.example.net"), ManagedWords.mailServerNames(" a.example.net, b.example.net ,, "))
    }

    @Test
    fun countsWhatSitsInEachLayout() {
        val counts = DomainLayout.templateCountByLayoutId(listOf(DomainTemplate("a", name = "A", layoutId = "layout-1"), DomainTemplate("b", name = "B", layoutId = "layout-1"), DomainTemplate("c", name = "C")))
        assertEquals(mapOf("layout-1" to 2), counts)
        assertEquals(listOf("en", "ja"), DomainTemplate("a", name = "A", locale = "en", translations = listOf(DomainTemplate.Translation("ja"))).locales)
    }
}
