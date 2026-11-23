package com.teanode.kit.mail

import com.teanode.kit.FakeServer
import com.teanode.kit.HTTPRequest
import com.teanode.kit.TeaNodeError
import com.teanode.kit.connectionFor
import com.teanode.kit.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** The operator's calls that change something, and the reads of the queue, one domain, the mailboxes and the templates. */
class ManageMailCallsTest {
    private fun queryOf(request: HTTPRequest): String = request.body!!.toString(Charsets.UTF_8)

    @Test
    fun readsWhetherPicturesWereFetchedByMessage() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"ListMailOpens":[{"mailId":"mail-1","trackable":true,"opened":true,"openedAt":"2030-01-02T00:00:00Z","openCount":3},{"mailId":"mail-2","trackable":false,"opened":false}]}}"""
        }
        val connection = connectionFor(server)
        val opens = connection.mailOpens(listOf("mail-1", "mail-2"))
        assertEquals(3, opens["mail-1"]?.openCount)
        assertEquals(false, opens["mail-2"]?.isTrackable)
        assertEquals(JsonArray(listOf(JsonPrimitive("mail-1"), JsonPrimitive("mail-2"))), sentVariables(server.sent[0])["mailIds"])
        assertTrue(connection.mailOpens(emptyList()).isEmpty())
        assertEquals(1, server.sent.size, "nothing is asked about no messages")
    }

    @Test
    fun readsTheQueueAndRetriesADelivery() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"ListPendingDeliveries":[{"id":"delivery-1","mailId":"mail-1","domainId":"domain-1","recipient":"person@example.org","kind":"forward","status":"queued","attempts":1,"error":"451 try later","retryAt":"2030-01-02T05:00:00Z"}],"RetryDelivery":{"id":"delivery-1","status":"queued","attempts":2}}}"""
        }
        val connection = connectionFor(server)
        val deliveries = connection.pendingDeliveries()
        assertEquals("451 try later", deliveries.first().errorMessage)
        assertTrue(deliveries.first().isFailing, "a delivery put off to a later attempt has failed once")
        connection.retryDelivery("delivery-1")
        assertEquals(JsonPrimitive("delivery-1"), sentVariables(server.sent[1])["deliveryId"])
    }

    @Test
    fun readsOneDomainWhole() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"GetDomain":{"id":"domain-1","domain":"example.com","subdomain":"","comment":"","spamFilterScoreThreshold":5,
              "mailServers":["mx1.example.com"],"mailHosts":["mx1.example.com"],"linkHost":"","linkHostname":"link.example.com","dkimSelector":"sel1","hasDkimKey":true,
              "logo":{"filename":"mark.svg","title":"Example","url":"/api/v1/domains/domain-1/logo","publicUrl":"https://example.com/mark.svg","uploadedAt":"2030-01-01T00:00:00Z"},
              "aliases":[{"id":"alias-1","pattern":"","kind":"mailbox","mailboxId":"mailbox-1","disabled":false},{"id":"alias-2","pattern":"^hello$","kind":"mailServer","disabled":true,"mailServer":{"host":"relay.example.net","port":25}}],
              "credentials":[{"id":"credential-1","comment":"laptop","disabled":false}],
              "records":{"checkedAt":"2030-01-01T00:00:00Z","error":"","records":[
                {"type":"MX","name":"example.com","expected":"mx1.example.com","priority":10,"optional":false,"found":[],"verified":false,"purpose":"where mail arrives"},
                {"type":"TXT","name":"_bimi.example.com","expected":"","optional":true,"verified":false,"purpose":"logo","blocked":"the DMARC policy is none"}]}}}}"""
        }
        val domain = connectionFor(server).managedDomain("domain-1")
        assertEquals(true, domain?.hasDkimKey)
        assertEquals("mark.svg", domain?.logo?.filename)
        assertEquals(true, domain?.aliases?.last()?.isDisabled)
        assertEquals(25, domain?.aliases?.last()?.mailServer?.port)
        assertEquals("laptop", domain?.credentials?.first()?.comment)
        assertEquals("10 mx1.example.com", domain?.dnsCheck?.records?.first()?.publishedValue, "an MX value is its preference and host")
        assertEquals("", domain?.dnsCheck?.records?.last()?.publishedValue)
        assertEquals("the DMARC policy is none", domain?.dnsCheck?.records?.last()?.blockedReason)
    }

    @Test
    fun addsADomain() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"CreateDomain":{"id":"domain-3","domain":"example.org"}}}""" }
        assertEquals("domain-3", connectionFor(server).createManagedDomain("example.org"))
        assertEquals(JsonPrimitive("example.org"), sentVariables(server.sent[0])["domain"])
    }

    @Test
    fun changesAnAliasSendingOnlyTheDestinationItsKindNeeds() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"UpdateAlias":{"id":"alias-3"}}}""" }
        val connection = connectionFor(server)
        connection.updateDomainAlias("alias-3", "^sales$", "the sales desk", "webhook", "https://example.net/hook", "mailbox-1", true)
        connection.updateDomainAlias("alias-4", "", "", "mailServer", "left over", "", false)
        assertTrue(queryOf(server.sent[0]).contains("UpdateAlias"))
        val first = sentVariables(server.sent[0])
        assertEquals(JsonPrimitive("webhook"), first["kind"])
        assertEquals(JsonNull, first["email"])
        assertEquals(JsonPrimitive("https://example.net/hook"), first["webhook"])
        assertEquals(JsonNull, first["mailboxId"])
        assertEquals(JsonPrimitive(true), first["disabled"])
        val second = sentVariables(server.sent[1])
        assertEquals(JsonPrimitive("mailServer"), second["kind"])
        assertEquals(JsonNull, second["email"])
        assertEquals(JsonNull, second["webhook"])
        assertEquals(JsonPrimitive(false), second["disabled"])
    }

    @Test
    fun readsTheServersMailboxesForAnAlias() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"ListAllMailboxes":[{"id":"mailbox-1","name":"Inbox","userId":"user-1","username":"robin","userName":"Robin Example"},{"id":"mailbox-2","name":"Shared","userId":"user-2","username":"sam","userName":"sam"}]}}"""
        }
        val mailboxes = connectionFor(server).serverMailboxes()
        assertEquals(listOf("robin (Robin Example) · Inbox", "sam · Shared"), mailboxes.map { mailbox -> mailbox.pickerText })
        assertEquals("robin · Inbox", mailboxes[0].rowText)
    }

    @Test
    fun makesACredentialShownOnce() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"CreateCredential":{"username":"made-up-user","password":"made-up-password","host":"mail.example.com","port":"587"}}}""" }
        val created = connectionFor(server).createDomainCredential("domain-1", "printer")
        assertEquals("made-up-password", created.password)
        assertEquals("587", created.port)
        assertEquals(JsonPrimitive("domain-1"), sentVariables(server.sent[0])["domainId"])
        assertEquals(JsonPrimitive("printer"), sentVariables(server.sent[0])["comment"])
    }

    @Test
    fun changesACredentialWithoutTouchingItsPassword() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"UpdateCredential":{"id":"credential-1"}}}""" }
        connectionFor(server).updateDomainCredential("credential-1", "printer", "scans", true)
        assertTrue(queryOf(server.sent[0]).contains("UpdateCredential"))
        assertFalse(queryOf(server.sent[0]).contains("password"))
        val variables = sentVariables(server.sent[0])
        assertEquals(JsonPrimitive("credential-1"), variables["credentialId"])
        assertEquals(JsonPrimitive("printer"), variables["comment"])
        assertEquals(JsonPrimitive("scans"), variables["alias"])
        assertEquals(JsonPrimitive(true), variables["disabled"])
    }

    @Test
    fun checksTheDnsAgainAfterChangingWhatToPublish() = runBlocking {
        val server = FakeServer { 200 to """{"data":{"UpdateDomain":{"id":"domain-1"},"CheckDomain":{"id":"domain-1"},"DeleteDomainLogo":true}}""" }
        val connection = connectionFor(server)
        connection.updateDomainMailServers("domain-1", ManagedWords.mailServerNames(" mx1.example.com, ,mx2.example.com "))
        assertEquals(JsonArray(listOf(JsonPrimitive("mx1.example.com"), JsonPrimitive("mx2.example.com"))), sentVariables(server.sent[0])["mailServers"])
        assertTrue(queryOf(server.sent[1]).contains("CheckDomain"))
        connection.updateDomainDkimSelector("domain-1", "sel2")
        assertTrue(queryOf(server.sent[3]).contains("CheckDomain"))
        connection.updateDomainLinkHost("domain-1", "")
        assertEquals(5, server.sent.size, "the host for pictures changes no record")
        connection.deleteDomainLogo("domain-1")
        assertTrue(queryOf(server.sent[6]).contains("CheckDomain"))
    }

    @Test
    fun saysWhyALogoWasRefused() = runBlocking {
        val server = FakeServer { 400 to """{"error":"the mark is not square"}""" }
        val refusal = try {
            connectionFor(server).uploadDomainLogo("domain-1", "mark.svg", "<svg/>".toByteArray())
            null
        } catch (refused: TeaNodeError.Refused) {
            refused
        }
        assertEquals("the mark is not square", refusal?.reason)
        assertTrue(server.sent[0].url.endsWith("/api/v1/domains/domain-1/logo"))
        assertEquals(1, server.sent.size, "nothing is checked after a refusal")
    }

    @Test
    fun readsTemplatesAndLayoutsAndCountsWhatSitsInEachLayout() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"ListTemplates":[
              {"id":"template-1","name":"welcome","comment":"","locale":"en","layoutId":"layout-1","modifiedAt":"2030-01-01T00:00:00Z","variables":["name"],"translations":[{"locale":"ja"}]},
              {"id":"template-2","name":"reset","locale":"","layoutId":"","variables":[],"translations":[]}],
              "ListLayouts":[{"id":"layout-1","comment":"with the footer","locale":"en","translations":[]}]}}"""
        }
        val found = connectionFor(server).domainTemplates("domain-1")
        assertEquals(listOf("welcome", "reset"), found.templates.map { template -> template.name })
        assertEquals(listOf("en", "ja"), found.templates[0].locales)
        assertEquals(emptyList<String>(), found.templates[1].locales)
        assertEquals("with the footer", found.layouts.first().comment)
        assertEquals(mapOf("layout-1" to 1), DomainLayout.templateCountByLayoutId(found.templates))
        assertEquals(JsonPrimitive("domain-1"), sentVariables(server.sent[0])["domainId"])
    }

    @Test
    fun readsOneTemplateAndRemovesTemplatesAndLayouts() = runBlocking {
        val server = FakeServer {
            200 to """{"data":{"GetTemplate":{"id":"template-1","name":"welcome","subject":"Hello","htmlContent":"<p>Hi</p>","textContent":"Hi",
              "translations":[{"locale":"ja","subject":"Konnichiwa","htmlContent":"","textContent":"Konnichiwa"}]},"DeleteTemplate":true,"DeleteLayout":true}}"""
        }
        val connection = connectionFor(server)
        val template = connection.domainTemplate("template-1")
        assertEquals("Konnichiwa", template?.translations?.first()?.textContent)
        connection.deleteDomainTemplate("template-1")
        connection.deleteDomainLayout("layout-1")
        assertEquals(JsonPrimitive("template-1"), sentVariables(server.sent[1])["templateId"])
        assertEquals(JsonPrimitive("layout-1"), sentVariables(server.sent[2])["layoutId"])
    }
}
