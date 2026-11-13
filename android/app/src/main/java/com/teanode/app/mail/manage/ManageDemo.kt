package com.teanode.app.mail.manage

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import com.teanode.kit.mail.ServerTime
import java.time.Instant
import java.time.temporal.ChronoUnit

/** The invented operator's pages of demo mode: the mail a server handled, its queue, a DMARC report and two domains. Everything is made up. */
object ManageDemoData {
    private val now: Instant = Instant.now()
    private fun hoursAgo(hours: Long): String = ServerTime.text(now.minus(hours, ChronoUnit.HOURS))

    private val mails: String = """
        [{"id":"mail-1","domainId":"domain-1","sender":"sam@example.org","from":"Sam Fixture <sam@example.org>","subject":"Lunch on Friday?","status":"accepted","kind":"incoming","size":4200,"receivedAt":"${hoursAgo(1)}"},
         {"id":"mail-2","domainId":"domain-1","sender":"orders@example.org","from":"Example Books <orders@example.org>","subject":"Your receipt for order 1042","status":"accepted","kind":"incoming","size":48213,"receivedAt":"${hoursAgo(3)}"},
         {"id":"mail-3","domainId":"domain-1","sender":"support@examp1e-login.test","from":"Account Team <support@examp1e-login.test>","subject":"Verify your account now","status":"rejected","kind":"incoming","size":2100,"receivedAt":"${hoursAgo(5)}"},
         {"id":"mail-4","domainId":"domain-2","sender":"alex@example.com","from":"Alex Example <alex@example.com>","subject":"Re: Lunch on Friday?","status":"delivered","kind":"outgoing","size":3100,"receivedAt":"${hoursAgo(20)}"}]
    """.trimIndent()

    private val mailDetail: String = """
        {"id":"mail-3","sender":"support@examp1e-login.test","from":"Account Team <support@examp1e-login.test>","subject":"Verify your account now","recipients":["alex@example.com"],"status":"rejected","kind":"incoming","size":2100,"receivedAt":"${hoursAgo(5)}",
         "ip":"203.0.113.7","rdns":"mail.examp1e-login.test.","hello":"mail.examp1e-login.test","messageId":"<phish-1@examp1e-login.test>","envelopeId":"env-1","tlsVersion":"TLSv1.3","tlsCipherSuite":"TLS_AES_256_GCM_SHA384","location":{"city":"Sampleton","country":"Exampleland"},
         "authenticationResults":{"senderMx":null,"fromMx":null,"spf":{"domain":"examp1e-login.test","ip":"203.0.113.7","result":"fail"},
           "dmarc":{"domain":"examp1e-login.test","policy":"reject","subdomainPolicy":"reject","dkimAlignment":"r","spfAlignment":"r","result":"fail","passedAtForwarderDomain":null},
           "dkims":[{"domain":"examp1e-login.test","selector":"mail","identifier":"@examp1e-login.test","result":"fail"}],"arc":null,
           "spamFilter":{"score":9.1,"result":"fail","symbols":["PHISHING"],"checks":[{"symbol":"PHISHING","score":6.0,"description":"Looks like a phishing page"}]},"antivirus":{"viruses":[]},"contentFilter":null,"errors":[]}}
    """.trimIndent()

    private val deliveries: String = """
        [{"id":"delivery-1","mailId":"mail-4","domainId":"domain-2","recipient":"sam@example.org","kind":"outgoing","status":"failed","size":3100,"attempts":3,"error":"451 try again later","method":"smtp","destination":"mx.example.org","attemptedAt":"${hoursAgo(1)}","retryAt":"${ServerTime.text(now.plus(1, ChronoUnit.HOURS))}","deliveryStatuses":[{"reportingMta":"mx.example.org","recipientStatuses":[{"finalRecipient":"sam@example.org","action":"delayed","status":"4.2.2","remoteMta":"mx.example.org","diagnosticCode":"mailbox full","lastAttemptDate":null}]}]},
         {"id":"delivery-2","mailId":"mail-1","domainId":"domain-1","recipient":"alex@example.com","kind":"incoming","status":"queued","size":4200,"attempts":0,"error":"","method":"email","destination":"","attemptedAt":null,"retryAt":null,"deliveryStatuses":[]}]
    """.trimIndent()

    private val reports: String = """
        [{"id":"report-1","domainId":"domain-1","beginAt":"${hoursAgo(30)}","endAt":"${hoursAgo(6)}","count":12,"ip":"198.51.100.9","rdns":"relay.example.net.","fromDomain":"example.com","senderDomain":"example.net","disposition":"none","dkimAligned":true,"spfAligned":true},
         {"id":"report-2","domainId":"domain-1","beginAt":"${hoursAgo(30)}","endAt":"${hoursAgo(6)}","count":3,"ip":"203.0.113.7","rdns":"","fromDomain":"example.com","senderDomain":"","disposition":"reject","dkimAligned":false,"spfAligned":false}]
    """.trimIndent()

    private val report: String = """
        {"id":"report-2","domainId":"domain-1","beginAt":"${hoursAgo(30)}","endAt":"${hoursAgo(6)}","count":3,"ip":"203.0.113.7","rdns":"","fromDomain":"example.com","senderDomain":"","disposition":"reject","dkimAligned":false,"spfAligned":false,"location":{"city":"Sampleton","country":"Exampleland"},
         "feedback":{"organizationName":"Example Receiver","email":"reports@example.net","extraContactInfo":"","reportId":"receiver-2030-01","begin":1893456000,"end":1893542400,"errors":[],"domain":"example.com","dkimAlignment":"r","spfAlignment":"s","policy":"reject","subdomainPolicy":"reject","percent":100,"failureOptions":"0",
           "records":[{"sourceIp":"203.0.113.7","count":3,"disposition":"reject","dkim":"fail","spf":"fail","reasonType":"","reasonComment":"","headerFrom":"example.com","envelopeFrom":"examp1e-login.test","envelopeTo":"example.org","dkims":[],"spfs":[]}]}}
    """.trimIndent()

    private val domains: String = """
        [{"id":"domain-1","domain":"example.com","subdomain":"","comment":"Main domain","spamFilterScoreThreshold":5.0,"aliases":[],"credentials":[],
          "records":{"checkedAt":"${hoursAgo(2)}","error":"","records":[
            {"type":"MX","name":"example.com","expected":"mail.example.com","priority":10,"optional":false,"found":["10 mail.example.com"],"verified":true,"purpose":"Receives the domain's mail","blocked":""},
            {"type":"TXT","name":"example.com","expected":"v=spf1 mx -all","priority":0,"optional":false,"found":["v=spf1 mx -all"],"verified":true,"purpose":"Says which servers may send as the domain","blocked":""},
            {"type":"TXT","name":"mail._domainkey.example.com","expected":"v=DKIM1; k=rsa; p=EXAMPLEKEY","priority":0,"optional":false,"found":[],"verified":false,"purpose":"Signs outgoing mail","blocked":""},
            {"type":"TXT","name":"_dmarc.example.com","expected":"v=DMARC1; p=none","priority":0,"optional":true,"found":[],"verified":false,"purpose":"Asks receivers for reports","blocked":""}]},
          "mailServers":["mail.example.com"],"mailHosts":["mail.example.com"],"linkHost":"links.example.com","linkHostname":"links.example.com","dkimSelector":"mail","hasDkimKey":true,"logo":null},
         {"id":"domain-2","domain":"example.net","subdomain":"","comment":"","spamFilterScoreThreshold":5.0,"aliases":[],"credentials":[],"records":null,"mailServers":[],"mailHosts":[],"linkHost":"","linkHostname":"","dkimSelector":"","hasDkimKey":false,"logo":null}]
    """.trimIndent()

    private val domainDetail: String = """
        {"id":"domain-1","domain":"example.com","subdomain":"","comment":"Main domain","spamFilterScoreThreshold":5.0,
         "aliases":[{"id":"alias-1","pattern":"alex","comment":"Alex","kind":"mailbox","email":"","webhook":"","mailboxId":"mailbox-1","disabled":false,"mailServer":null},
                    {"id":"alias-2","pattern":"*","comment":"Catch all","kind":"email","email":"alex@example.com","webhook":"","mailboxId":"","disabled":true,"mailServer":null}],
         "credentials":[{"id":"credential-1","comment":"Printer","alias":"scanner","disabled":false}],
         "records":{"checkedAt":"${hoursAgo(2)}","error":"","records":[
            {"type":"MX","name":"example.com","expected":"mail.example.com","priority":10,"optional":false,"found":["10 mail.example.com"],"verified":true,"purpose":"Receives the domain's mail","blocked":""},
            {"type":"TXT","name":"mail._domainkey.example.com","expected":"v=DKIM1; k=rsa; p=EXAMPLEKEY","priority":0,"optional":false,"found":[],"verified":false,"purpose":"Signs outgoing mail","blocked":""},
            {"type":"TXT","name":"_dmarc.example.com","expected":"v=DMARC1; p=none","priority":0,"optional":true,"found":[],"verified":false,"purpose":"Asks receivers for reports","blocked":""}]},
         "mailServers":["mail.example.com"],"mailHosts":["mail.example.com"],"linkHost":"links.example.com","linkHostname":"links.example.com","dkimSelector":"mail","hasDkimKey":true,"logo":null}
    """.trimIndent()

    val answerers: List<DemoAnswerer> = listOf(
        // Only the operator's question about the session: the foundation answers who is signed in.
        DemoAnswerer { request ->
            if (request.document.contains("manages")) DemoResponse("""{"data":{"GetSession":{"manages":true,"permissions":{"everywhere":["mail:audit","queue:manage","report:read","domain:manage"],"byDomain":[]}}}}""") else null
        },
        graphQLField("ListMailOpens") { _ -> """[{"mailId":"mail-4","trackable":true,"opened":true,"openedAt":"${hoursAgo(10)}","lastOpenedAt":"${hoursAgo(9)}","openCount":2,"ip":null},{"mailId":"mail-2","trackable":true,"opened":false,"openedAt":null,"lastOpenedAt":null,"openCount":0,"ip":null}]""" },
        graphQLField("ListMails") { variables -> if (variables.containsKey("pagination")) mails else """[{"receivedAt":"${hoursAgo(1)}"}]""" },
        // The page of one message asks for its training, the filter settings, the message, its content, pictures and deliveries together.
        DemoAnswerer { request ->
            if (request.document.contains("GetSpamTraining")) {
                DemoResponse("""{"data":{"GetSpamTraining":{"label":""},"GetSettings":{"antispam":{"effectiveEngine":"builtin","bayesEnabled":true,"bayesMinimumMessages":200,"bayesLearnedSpam":12,"bayesLearnedHam":30}},"GetMail":$mailDetail,"GetMailContent":{"mailId":"mail-3","available":true,"text":"Please verify your account at the address below.","html":"","hasRemoteContent":false,"imagesAllowed":false,"size":2100,"rawHeaders":"Received: from mail.examp1e-login.test\nSubject: Verify your account now","headers":[],"attachments":[]},"GetMailOpens":{"trackable":true,"opened":false,"openedAt":null,"lastOpenedAt":null,"openCount":0,"ip":null},"ListDeliveriesByMail":[]}}""")
            } else {
                null
            }
        },
        // The first page of a domain asks for its mail counts, its newest message and its waiting deliveries together.
        DemoAnswerer { request ->
            if (request.document.contains("CountMailsBy")) {
                DemoResponse("""{"data":{"CountMailsBy":[{"value":"accepted","count":120},{"value":"rejected","count":14}],"ListMails":[{"id":"mail-1","receivedAt":"${hoursAgo(1)}"}],"ListPendingDeliveries":$deliveries}}""")
            } else {
                null
            }
        },
        DemoAnswerer { request ->
            if (request.document.contains("ListTemplates")) {
                DemoResponse("""{"data":{"ListTemplates":[{"id":"template-1","domainId":"domain-1","layoutId":"layout-1","name":"Welcome","comment":"","locale":"en","subject":"Welcome","variables":[],"modifiedAt":null,"translations":[{"locale":"ja"}]}],"ListLayouts":[{"id":"layout-1","domainId":"domain-1","comment":"Plain layout","locale":"en","modifiedAt":null,"translations":[]}]}}""")
            } else {
                null
            }
        },
        graphQLField("ListPendingDeliveries") { _ -> deliveries },
        graphQLField("ListReports") { _ -> reports },
        graphQLField("GetReport") { _ -> report },
        graphQLField("ListDomains") { _ -> domains },
        graphQLField("GetDomain") { _ -> domainDetail },
        graphQLField("ListAllMailboxes") { _ -> """[{"id":"mailbox-1","name":"Alex Example","userId":"user-1","username":"alex","userName":"Alex Example"}]""" },
        graphQLField("RetryDelivery", "true"),
        graphQLField("MarkMail") { _ -> """{"label":"spam","learnedSpam":10,"learnedHam":20}""" },
        graphQLField("ForgetMail") { _ -> """{"label":"","learnedSpam":9,"learnedHam":20}""" },
        graphQLField("CheckDomain", """{"id":"domain-1"}"""),
        graphQLField("CreateDomain", """{"id":"domain-3"}"""),
        graphQLField("CreateAlias", "true"),
        graphQLField("UpdateAlias", "true"),
        graphQLField("DeleteAlias", "true"),
        graphQLField("CreateCredential", """{"username":"scanner@example.com","password":"demo-secret-0000","host":"mail.example.com","port":"587"}"""),
        graphQLField("DeleteCredential", "true"),
        graphQLField("UpdateDomain", """{"id":"domain-1"}"""),
        graphQLField("RegenerateDomainKey", """{"id":"domain-1"}"""),
    )
}
