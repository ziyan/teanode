import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// ManageMailTests cover the operator's mail delivery pages: reading the
/// server's answers for all mail, the queue, the reports and the domains,
/// what is sent to change them, and what the pages decide from them.
final class ManageMailTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String, statusCode: Int = 200) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (statusCode, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "operator")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentBody(_ request: URLRequest) throws -> [String: JSONValue] {
        try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        guard case .object(let variables) = try sentBody(request)["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private func sentQuery(_ request: URLRequest) throws -> String {
        guard case .string(let query) = try sentBody(request)["query"] else { return "" }
        return query
    }

    // MARK: Who may open the area

    func testReadsWhetherThePersonIsAnOperatorAndWhichPagesTheyMayOpen() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"GetSession":{"manages":true,"permissions":{"everywhere":["report:read","domain:manage-all"],
                  "byDomain":[{"domainId":"domain-1","permissions":["queue:manage"]}]}}}}
                """#)
        let access = try await connection.operatorAccess()
        XCTAssertTrue(access.isOperator)
        XCTAssertTrue(access.canReadReports)
        XCTAssertTrue(access.canManageDomains, "an all-domains permission stands in for the domain permission")
        XCTAssertTrue(access.canManageQueue, "a domain permission held over one domain is enough")
        XCTAssertFalse(access.canAuditMail)
        XCTAssertTrue(access.canOpenMailDelivery)
    }

    func testSomebodyWithoutPermissionsIsNoOperator() async throws {
        let (connection, _) = connection(answering: #"{"data":{"GetSession":{"manages":false,"permissions":null}}}"#)
        let isOperator = try await connection.isOperator()
        XCTAssertFalse(isOperator)
        let access = OperatorAccess(isOperator: false)
        XCTAssertFalse(access.canOpenMailDelivery)
        XCTAssertFalse(OperatorAccess(isOperator: true, everywherePermissions: ["mail:audit-allx"]).canAuditMail)
    }

    // MARK: All mail

    func testAsksForTheNewestMailOfEveryDomainAndReadsTheRows() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListMails":[
                  {"id":"mail-1","domainId":"domain-1","sender":"bounce@example.net","from":"","subject":"Hello",
                   "status":"accepted","kind":"incoming","size":2048,"receivedAt":"2030-01-02T03:04:05Z"},
                  null
                ]}}
                """#)
        let mails = try await connection.allMail()
        XCTAssertEqual(mails.map(\.id), ["mail-1"])
        XCTAssertEqual(mails[0].mailStatus, "accepted")
        XCTAssertEqual(mails[0].mailKind, "incoming")
        XCTAssertEqual(mails[0].byteCount, 2048)
        XCTAssertEqual(mails[0].shownSender, "bounce@example.net", "with no From header the envelope sender stands in")
        XCTAssertEqual(
            try sentVariables(fake.sent[0]), ["domainId": .string(""), "pagination": .object(["first": .number(500)])])
    }

    func testReadsWhetherPicturesWereFetchedByMessage() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListMailOpens":[
                  {"mailId":"mail-1","trackable":true,"opened":true,"openedAt":"2030-01-02T00:00:00Z","openCount":3},
                  {"mailId":"mail-2","trackable":false,"opened":false}
                ]}}
                """#)
        let opens = try await connection.mailOpens(mailIds: ["mail-1", "mail-2"])
        XCTAssertEqual(opens["mail-1"]?.openCount, 3)
        XCTAssertEqual(opens["mail-2"]?.isTrackable, false)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["mailIds": .array([.string("mail-1"), .string("mail-2")])])
        let none = try await connection.mailOpens(mailIds: [])
        XCTAssertTrue(none.isEmpty)
        XCTAssertEqual(fake.sent.count, 1, "nothing is asked about no messages")
    }

    func testReadsOneMessageWithItsChecksContentAndDeliveries() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{
                  "GetSpamTraining":{"label":"spam"},
                  "GetSettings":{"antispam":{"effectiveEngine":"builtin","bayesEnabled":true,"bayesMinimumMessages":200,
                    "bayesLearnedSpam":12,"bayesLearnedHam":30}},
                  "GetMail":{"id":"mail-1","sender":"someone@example.net","from":"Someone <someone@example.net>","subject":"Invoice",
                    "recipients":["sales@example.com"],"status":"accepted","kind":"incoming","size":4096,"receivedAt":"2030-01-02T03:04:05Z",
                    "ip":"192.0.2.10","rdns":"mx.example.net.","hello":"mx.example.net","messageId":"<one@example.net>",
                    "tlsVersion":"TLS 1.3","tlsCipherSuite":"TLS_AES_128_GCM_SHA256","location":{"city":"Springfield","country":"Atlantis"},
                    "authenticationResults":{
                      "senderMx":{"domain":"example.net","mailServers":["mx.example.net"]},
                      "spf":{"domain":"example.net","ip":"192.0.2.10","result":"pass"},
                      "dmarc":{"domain":"example.net","policy":"reject","dkimAlignment":"s","result":"pass"},
                      "dkims":[{"domain":"example.net","selector":"sel1","identifier":"@example.net","result":"pass"}],
                      "arc":{"result":"none","instances":0},
                      "spamFilter":{"score":1.25,"result":"pass","symbols":["RULE_A"],"checks":[{"symbol":"RULE_A","score":1.25,"description":"a rule"}]},
                      "antivirus":{"viruses":[]},
                      "errors":null}},
                  "GetMailContent":{"mailId":"mail-1","available":true,"text":"Hello","html":"","hasRemoteContent":false,"imagesAllowed":false,
                    "size":4096,"rawHeaders":"Subject: Invoice","headers":[{"key":"Subject","value":"Invoice"}],"attachments":[]},
                  "GetMailOpens":{"trackable":false,"opened":false},
                  "ListDeliveriesByMail":[{"id":"delivery-1","recipient":"person@example.org","kind":"forward","status":"failed",
                    "size":4096,"attempts":2,"error":"550 no such user","method":"email","destination":"person@example.org",
                    "attemptedAt":"2030-01-02T04:00:00Z","retryAt":null,
                    "deliveryStatuses":[{"reportingMta":"mx.example.org","recipientStatuses":[
                      {"finalRecipient":"person@example.org","action":"failed","status":"5.1.1","remoteMta":"mx.example.org",
                       "diagnosticCode":"smtp; 550 no such user"}]}]}]
                }}
                """#)
        let mailDetail = try await connection.auditedMail(mailId: "mail-1")
        XCTAssertEqual(mailDetail.trainingLabel, "spam")
        XCTAssertEqual(mailDetail.classifier?.canBeTaught, true)
        XCTAssertEqual(mailDetail.classifier?.bayesMinimumMessageCount, 200)
        XCTAssertEqual(mailDetail.mail.reverseDnsName, "mx.example.net.")
        XCTAssertEqual(ManagedWords.hostName(mailDetail.mail.reverseDnsName), "mx.example.net")
        XCTAssertEqual(mailDetail.mail.location?.placeText, "Springfield, Atlantis")
        XCTAssertEqual(mailDetail.mail.authenticationResults?.spf?.spfResult, "pass")
        XCTAssertEqual(mailDetail.mail.authenticationResults?.dkims?.first?.selector, "sel1")
        XCTAssertEqual(mailDetail.mail.authenticationResults?.arc?.instanceCount, 0)
        XCTAssertEqual(mailDetail.mail.authenticationResults?.spamFilter?.checks?.first?.ruleDescription, "a rule")
        XCTAssertEqual(mailDetail.content?.text, "Hello")
        XCTAssertEqual(mailDetail.opens?.isTrackable, false)
        XCTAssertEqual(mailDetail.deliveries.first?.attemptCount, 2)
        XCTAssertEqual(mailDetail.deliveries.first?.deliveryMethod, "email")
        XCTAssertEqual(mailDetail.deliveries.first?.recipientReports.first?.report.reportStatus, "5.1.1")
        XCTAssertEqual(mailDetail.deliveries.first?.recipientReports.first?.reportingMta, "mx.example.org")
        XCTAssertEqual(MailOutcome.of(mailDetail.mail, deliveries: mailDetail.deliveries), .partlyDelivered(deliveredCount: 0, failedCount: 1, totalCount: 1))
    }

    func testAMessageNeverTaughtHasNoLabel() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"GetSpamTraining":null,"GetSettings":{"antispam":{"effectiveEngine":"spamd","bayesEnabled":false,
                  "bayesMinimumMessages":200,"bayesLearnedSpam":0,"bayesLearnedHam":0}},
                  "GetMail":{"id":"mail-2"},"GetMailContent":null,"GetMailOpens":null,"ListDeliveriesByMail":[]}}
                """#)
        let mailDetail = try await connection.auditedMail(mailId: "mail-2")
        XCTAssertNil(mailDetail.trainingLabel)
        XCTAssertEqual(mailDetail.classifier?.canBeTaught, false, "only the built-in filter with its classifier on learns")
        XCTAssertTrue(mailDetail.deliveries.isEmpty)
    }

    func testTeachesTheFilterOrForgetsWhatItWasTaught() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"MarkMail":{"mailId":"mail-1","label":"ham","learnedSpam":3,"learnedHam":4},
                  "ForgetMail":{"mailId":"mail-1","label":"","learnedSpam":3,"learnedHam":3}}}
                """#)
        let marked = try await connection.teachSpamFilter(mailId: "mail-1", trainingLabel: "ham")
        XCTAssertEqual(marked.trainingLabel, "ham")
        XCTAssertEqual(marked.learnedHamCount, 4)
        let forgotten = try await connection.teachSpamFilter(mailId: "mail-1", trainingLabel: nil)
        XCTAssertEqual(forgotten.trainingLabel, "")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["mailId": .string("mail-1"), "label": .string("ham")])
        XCTAssertTrue(try sentQuery(fake.sent[0]).contains("MarkMail"))
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["mailId": .string("mail-1")])
        XCTAssertTrue(try sentQuery(fake.sent[1]).contains("ForgetMail"))
    }

    func testSaysWhatHappenedToAMessageInOneLine() {
        func rejected(_ results: MailAuthenticationResults) -> AuditedMail {
            AuditedMail(id: "mail-1", mailStatus: "rejected", authenticationResults: results)
        }
        let virus = MailAuthenticationResults(antivirus: .init(viruses: ["Test.Virus"]))
        XCTAssertEqual(MailOutcome.of(rejected(virus), deliveries: []), .rejectedForViruses(["Test.Virus"]))
        let spam = MailAuthenticationResults(spamFilter: .init(score: 11.6, spamResult: "fail", symbols: nil, checks: nil))
        XCTAssertEqual(MailOutcome.of(rejected(spam), deliveries: []), .rejectedAsSpam(score: 11.6))
        let dmarc = MailAuthenticationResults(
            dmarc: .init(domain: "example.net", policy: "reject", subdomainPolicy: nil, dkimAlignment: nil, spfAlignment: nil, dmarcResult: "fail"))
        XCTAssertEqual(MailOutcome.of(rejected(dmarc), deliveries: []), .rejectedByDMARC(policy: "reject"))
        XCTAssertEqual(MailOutcome.of(rejected(.init(errorMessages: ["too large"])), deliveries: []), .rejectedWithErrors(["too large"]))
        XCTAssertEqual(MailOutcome.of(rejected(.init()), deliveries: []), .rejected)

        let accepted = AuditedMail(id: "mail-2", mailStatus: "accepted")
        XCTAssertEqual(MailOutcome.of(accepted, deliveries: []), .noDeliveries)
        let delivered = MailDelivery(id: "delivery-1", deliveryStatus: "delivered")
        let queued = MailDelivery(id: "delivery-2", deliveryStatus: "queued")
        XCTAssertEqual(MailOutcome.of(accepted, deliveries: [delivered, delivered]), .delivered(deliveredCount: 2))
        XCTAssertEqual(MailOutcome.of(accepted, deliveries: [delivered, queued]), .pending(pendingCount: 1))
        let dropped = MailDelivery(id: "delivery-3", deliveryStatus: "dropped")
        XCTAssertEqual(
            MailOutcome.of(accepted, deliveries: [delivered, dropped, queued]), .partlyDelivered(deliveredCount: 1, failedCount: 1, totalCount: 3))
    }

    func testFiltersMailByDomainStatusKindAndWords() {
        let mails = [
            AuditedMail(id: "one", domainId: "domain-1", from: "Robin <robin@example.net>", subject: "Quarterly report", mailStatus: "accepted", mailKind: "incoming"),
            AuditedMail(id: "two", domainId: "domain-2", sender: "bounce@example.org", subject: "Undelivered", mailStatus: "rejected", mailKind: "dsn"),
            AuditedMail(id: "three", domainId: "domain-1", from: "news@example.org", subject: "Weekly news", mailStatus: "rejected", mailKind: "incoming"),
        ]
        XCTAssertFalse(MailListFilter().isFiltering)
        XCTAssertEqual(MailListFilter().applied(to: mails).map(\.id), ["one", "two", "three"])
        XCTAssertEqual(MailListFilter(domainId: "domain-1").applied(to: mails).map(\.id), ["one", "three"])
        XCTAssertEqual(MailListFilter(domainId: "domain-1", mailStatus: "rejected").applied(to: mails).map(\.id), ["three"])
        XCTAssertEqual(MailListFilter(mailKind: "dsn").applied(to: mails).map(\.id), ["two"])
        XCTAssertEqual(MailListFilter(searchText: "BOUNCE").applied(to: mails).map(\.id), ["two"], "the envelope sender is searched too")
        XCTAssertEqual(MailListFilter(searchText: "weekly example.org").applied(to: mails).map(\.id), ["three"])
    }

    func testWritesAScoreTheWayTheDashboardDoes() {
        XCTAssertEqual(ManagedWords.score(11.606000000000002), "11.6")
        XCTAssertEqual(ManagedWords.score(3.0), "3")
        XCTAssertEqual(ManagedWords.score(-1.0), "-1")
        XCTAssertEqual(ManagedWords.score(0.25), "0.3")
    }

    func testGivesStatusesTheDashboardsTones() {
        XCTAssertEqual(ManagedTone.of("Delivered"), .good)
        XCTAssertEqual(ManagedTone.of("hardfail"), .bad)
        XCTAssertEqual(ManagedTone.of("queued"), .warn)
        XCTAssertEqual(ManagedTone.of("expanded"), .plain)
        XCTAssertEqual(ManagedTone.of(nil), .plain)
    }

    // MARK: The queue

    func testReadsTheQueueAndRetriesADelivery() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListPendingDeliveries":[
                  {"id":"delivery-1","mailId":"mail-1","domainId":"domain-1","recipient":"person@example.org","kind":"forward",
                   "status":"queued","attempts":1,"error":"451 try later","retryAt":"2030-01-02T05:00:00Z"}],
                  "RetryDelivery":{"id":"delivery-1","status":"queued","attempts":2}}}
                """#)
        let deliveries = try await connection.pendingDeliveries()
        XCTAssertEqual(deliveries.first?.errorMessage, "451 try later")
        XCTAssertEqual(deliveries.first?.isFailing, true, "a delivery put off to a later attempt has failed once")
        try await connection.retryDelivery(deliveryId: "delivery-1")
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["deliveryId": .string("delivery-1")])
    }

    func testFiltersTheQueue() {
        let deliveries = [
            MailDelivery(id: "one", domainId: "domain-1", recipient: "person@example.org", deliveryStatus: "queued"),
            MailDelivery(id: "two", domainId: "domain-2", recipient: "other@example.net", deliveryStatus: "failed"),
        ]
        XCTAssertEqual(DeliveryQueueFilter(domainId: "domain-2").applied(to: deliveries).map(\.id), ["two"])
        XCTAssertEqual(DeliveryQueueFilter(deliveryStatus: "queued").applied(to: deliveries).map(\.id), ["one"])
        XCTAssertEqual(DeliveryQueueFilter(searchText: "example.net").applied(to: deliveries).map(\.id), ["two"])
        XCTAssertTrue(DeliveryQueueFilter(searchText: "x").isFiltering)
    }

    // MARK: Reports

    func testReadsTheReportsAndTheirAlignment() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListReports":[
                  {"id":"report-1","domainId":"domain-1","beginAt":"2030-01-01T00:00:00Z","endAt":"2030-01-02T00:00:00Z","count":12,
                   "ip":"198.51.100.4","rdns":"sender.example.net.","fromDomain":"example.com","disposition":"none",
                   "dkimAligned":true,"spfAligned":false}]}}
                """#)
        let reports = try await connection.dmarcReports()
        XCTAssertEqual(reports.first?.messageCount, 12)
        XCTAssertEqual(reports.first?.alignment, .one)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["pagination": .object(["first": .number(500)])])
        XCTAssertEqual(DMARCReport(id: "a", isDkimAligned: true, isSpfAligned: true).alignment, .both)
        XCTAssertEqual(DMARCReport(id: "b", isDkimAligned: false, isSpfAligned: false).alignment, .neither)
        XCTAssertFalse(ReportAlignment.neither.isPassed)
    }

    func testReadsOneReportAsItArrived() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"GetReport":{"id":"report-1","count":2,"ip":"198.51.100.4","dkimAligned":false,"spfAligned":false,
                  "location":{"city":"","country":"Atlantis"},
                  "feedback":{"organizationName":"Receiver","email":"reports@example.org","reportId":"made-up-1","begin":1893456000,
                    "end":1893542400,"domain":"example.com","dkimAlignment":"r","spfAlignment":"s","policy":"quarantine","subdomainPolicy":"none",
                    "percent":50,"failureOptions":"1",
                    "records":[{"sourceIp":"198.51.100.4","count":2,"disposition":"quarantine","dkim":"fail","spf":"pass",
                      "reasonType":"forwarded","headerFrom":"example.com",
                      "dkims":[{"domain":"example.com","selector":"sel1","result":"fail","humanResult":"signature did not verify"}],
                      "spfs":[{"domain":"example.net","scope":"mfrom","result":"pass"}]}]}}}}
                """#)
        let report = try await connection.dmarcReport(reportId: "report-1")
        XCTAssertEqual(report?.location?.placeText, "Atlantis")
        XCTAssertEqual(report?.feedback?.policyText, "p=quarantine · sp=none · pct=50 · fo=1")
        XCTAssertEqual(report?.feedback?.records?.first?.isPassed, true)
        XCTAssertEqual(report?.feedback?.records?.first?.dkimResults?.first?.humanResult, "signature did not verify")
        XCTAssertEqual(report?.feedback?.records?.first?.spfResults?.first?.scope, "mfrom")
    }

    func testFiltersReports() {
        let reports = [
            DMARCReport(id: "one", domainId: "domain-1", fromDomain: "example.com", disposition: "none", isDkimAligned: true, isSpfAligned: true),
            DMARCReport(id: "two", domainId: "domain-1", fromDomain: "example.com", disposition: "reject", isDkimAligned: false, isSpfAligned: false),
        ]
        XCTAssertEqual(ReportListFilter(alignment: .neither).applied(to: reports).map(\.id), ["two"])
        XCTAssertEqual(ReportListFilter(disposition: "none").applied(to: reports).map(\.id), ["one"])
        XCTAssertEqual(ReportListFilter(domainId: "domain-2").applied(to: reports).map(\.id), [])
    }

    // MARK: Domains

    func testReadsTheDomainsAndTheirDNSInOneWord() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListDomains":[
                  {"id":"domain-1","domain":"example.com","subdomain":"","comment":"main","aliases":[{"id":"alias-1"}],"credentials":[],
                   "records":{"records":[{"type":"MX","verified":true,"optional":false},{"type":"TXT","verified":false,"optional":false},
                     {"type":"TXT","verified":false,"optional":true}]}},
                  {"id":"domain-2","domain":"example.net","aliases":[],"credentials":[],"records":null}]}}
                """#)
        let domains = try await connection.managedDomains()
        XCTAssertEqual(domains.map(\.domainName), ["example.com", "example.net"])
        XCTAssertEqual(domains[0].aliases?.count, 1)
        XCTAssertEqual(domains[0].dnsSummary, .missing(missingCount: 1), "an optional record is never missing")
        XCTAssertEqual(domains[1].dnsSummary, .notChecked)
        let published = DomainDNSCheck(records: [DNSRecord(recordType: "MX", isVerified: true), DNSRecord(recordType: "AAAA", isOptional: true, isVerified: false)])
        XCTAssertEqual(DomainDNSSummary.of(published), .allPublished(recordCount: 2))
    }

    func testReadsOneDomainWhole() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"GetDomain":{"id":"domain-1","domain":"example.com","subdomain":"","comment":"","spamFilterScoreThreshold":5,
                  "mailServers":["mx1.example.com"],"mailHosts":["mx1.example.com"],"linkHost":"","linkHostname":"link.example.com",
                  "dkimSelector":"sel1","hasDkimKey":true,
                  "logo":{"filename":"mark.svg","title":"Example","url":"/api/v1/domains/domain-1/logo","publicUrl":"https://example.com/mark.svg",
                    "uploadedAt":"2030-01-01T00:00:00Z"},
                  "aliases":[{"id":"alias-1","pattern":"","kind":"mailbox","mailboxId":"mailbox-1","disabled":false},
                    {"id":"alias-2","pattern":"^hello$","kind":"mailServer","disabled":true,"mailServer":{"host":"relay.example.net","port":25}}],
                  "credentials":[{"id":"credential-1","comment":"laptop","disabled":false}],
                  "records":{"checkedAt":"2030-01-01T00:00:00Z","error":"","records":[
                    {"type":"MX","name":"example.com","expected":"mx1.example.com","priority":10,"optional":false,"found":[],"verified":false,
                     "purpose":"where mail arrives"},
                    {"type":"TXT","name":"_bimi.example.com","expected":"","optional":true,"verified":false,"purpose":"logo",
                     "blocked":"the DMARC policy is none"}]}}}}
                """#)
        let domain = try await connection.managedDomain(domainId: "domain-1")
        XCTAssertEqual(domain?.hasDkimKey, true)
        XCTAssertEqual(domain?.logo?.filename, "mark.svg")
        XCTAssertEqual(domain?.aliases?.last?.isDisabled, true)
        XCTAssertEqual(domain?.aliases?.last?.mailServer?.port, 25)
        XCTAssertEqual(domain?.credentials?.first?.comment, "laptop")
        XCTAssertEqual(domain?.dnsCheck?.records?.first?.publishedValue, "10 mx1.example.com", "an MX value is its preference and host")
        XCTAssertEqual(domain?.dnsCheck?.records?.last?.publishedValue, "")
        XCTAssertEqual(domain?.dnsCheck?.records?.last?.blockedReason, "the DMARC policy is none")
    }

    func testCountsADomainsMailAndItsQueue() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"CountMailsBy":[{"value":"accepted","count":40},{"value":"rejected","count":2},{"value":"received","count":1}],
                  "ListMails":[{"id":"mail-9","receivedAt":"2030-01-03T00:00:00Z"}],
                  "ListPendingDeliveries":[{"id":"delivery-1","status":"queued","retryAt":null},
                    {"id":"delivery-2","status":"queued","retryAt":"2030-01-03T01:00:00Z"},{"id":"delivery-3","status":"failed"}]}}
                """#)
        let overview = try await connection.domainOverview(domainId: "domain-1")
        XCTAssertEqual(overview.totalMailCount, 43)
        XCTAssertEqual(overview.acceptedMailCount, 40)
        XCTAssertEqual(overview.rejectedMailCount, 2)
        XCTAssertEqual(overview.newestReceivedAt, "2030-01-03T00:00:00Z")
        XCTAssertEqual(overview.pendingDeliveries.count, 3)
        XCTAssertEqual(overview.failingDeliveryCount, 2)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["domainId": .string("domain-1")])
    }

    func testAddsADomain() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"CreateDomain":{"id":"domain-3","domain":"example.org"}}}"#)
        let domainId = try await connection.createManagedDomain(domainName: "example.org")
        XCTAssertEqual(domainId, "domain-3")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["domain": .string("example.org")])
    }

    func testAddsAnAliasSendingOnlyTheDestinationItsKindNeeds() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"CreateAlias":{"id":"alias-3"},"DeleteAlias":true}}"#)
        try await connection.createDomainAlias(domainId: "domain-1", pattern: "^sales$", aliasKind: "email", destination: "team@example.net", mailboxId: "mailbox-1")
        try await connection.createDomainAlias(domainId: "domain-1", pattern: "", aliasKind: "mailbox", destination: "left over", mailboxId: "mailbox-1")
        try await connection.createDomainAlias(domainId: "domain-1", pattern: "^drop$", aliasKind: "null", destination: "", mailboxId: "")
        try await connection.deleteDomainAlias(aliasId: "alias-3")
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["domainId": .string("domain-1"), "pattern": .string("^sales$"), "comment": .string(""), "kind": .string("email"),
             "email": .string("team@example.net"), "webhook": .null, "mailboxId": .null])
        XCTAssertEqual(try sentVariables(fake.sent[1])["mailboxId"], .string("mailbox-1"))
        XCTAssertEqual(try sentVariables(fake.sent[1])["email"], .null)
        XCTAssertEqual(try sentVariables(fake.sent[2])["webhook"], .null)
        XCTAssertEqual(try sentVariables(fake.sent[3]), ["aliasId": .string("alias-3")])
    }

    func testChangesAnAliasSendingOnlyTheDestinationItsKindNeeds() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"UpdateAlias":{"id":"alias-3"}}}"#)
        try await connection.updateDomainAlias(
            aliasId: "alias-3", pattern: "^sales$", comment: "the sales desk", aliasKind: "webhook", destination: "https://example.net/hook",
            mailboxId: "mailbox-1", isDisabled: true)
        try await connection.updateDomainAlias(
            aliasId: "alias-4", pattern: "", comment: "", aliasKind: "mailServer", destination: "left over", mailboxId: "", isDisabled: false)
        XCTAssertTrue(try sentQuery(fake.sent[0]).contains("UpdateAlias"))
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["aliasId": .string("alias-3"), "pattern": .string("^sales$"), "comment": .string("the sales desk"), "kind": .string("webhook"),
             "email": .null, "webhook": .string("https://example.net/hook"), "mailboxId": .null, "disabled": .bool(true)])
        XCTAssertEqual(try sentVariables(fake.sent[1])["kind"], .string("mailServer"))
        XCTAssertEqual(try sentVariables(fake.sent[1])["email"], .null)
        XCTAssertEqual(try sentVariables(fake.sent[1])["webhook"], .null)
        XCTAssertEqual(try sentVariables(fake.sent[1])["disabled"], .bool(false))
    }

    func testKnowsWhichKindsAndDestinationsAnAliasEditorOffers() throws {
        let relay = try JSONDecoder().decode(
            DomainAlias.self, from: Data(#"{"id":"alias-5","pattern":"^relay$","kind":"mailServer","mailServer":{"host":"mx.example.net","port":25}}"#.utf8))
        let forward = try JSONDecoder().decode(
            DomainAlias.self, from: Data(#"{"id":"alias-6","pattern":"^sales$","kind":"email","email":"team@example.net"}"#.utf8))
        XCTAssertEqual(relay.selectableKinds, ["mailbox", "email", "webhook", "null", "mailServer"])
        XCTAssertEqual(forward.selectableKinds, ["mailbox", "email", "webhook", "null"])
        XCTAssertEqual(forward.editableDestination, "team@example.net")
        XCTAssertEqual(relay.editableDestination, "")
        XCTAssertFalse(DomainAlias.isDestinationFilled(aliasKind: "email", destination: "  ", mailboxId: ""))
        XCTAssertTrue(DomainAlias.isDestinationFilled(aliasKind: "webhook", destination: "https://example.net/hook", mailboxId: ""))
        XCTAssertFalse(DomainAlias.isDestinationFilled(aliasKind: "mailbox", destination: "left over", mailboxId: ""))
        XCTAssertTrue(DomainAlias.isDestinationFilled(aliasKind: "mailbox", destination: "", mailboxId: "mailbox-1"))
        XCTAssertTrue(DomainAlias.isDestinationFilled(aliasKind: "null", destination: "", mailboxId: ""))
        XCTAssertTrue(DomainAlias.isDestinationFilled(aliasKind: "mailServer", destination: "", mailboxId: ""))
        XCTAssertFalse(DomainAlias.isDestinationFilled(aliasKind: "", destination: "team@example.net", mailboxId: ""))
    }

    func testStartsAndSwitchesTheAliasEditorKindWithoutCarryingAnAddressOver() throws {
        let forward = try JSONDecoder().decode(
            DomainAlias.self, from: Data(#"{"id":"alias-6","pattern":"^sales$","kind":"email","email":"team@example.net"}"#.utf8))
        let noKind = try JSONDecoder().decode(DomainAlias.self, from: Data(#"{"id":"alias-7","pattern":"^lost$"}"#.utf8))
        let unknownKind = try JSONDecoder().decode(
            DomainAlias.self, from: Data(#"{"id":"alias-8","pattern":"^odd$","kind":"carrierPigeon"}"#.utf8))
        XCTAssertEqual(forward.editorKind, "email")
        XCTAssertEqual(noKind.editorKind, "")
        XCTAssertEqual(unknownKind.editorKind, "")
        XCTAssertEqual(forward.editableDestination(forKind: "email"), "team@example.net")
        XCTAssertEqual(forward.editableDestination(forKind: "webhook"), "")
        XCTAssertEqual(forward.editableDestination(forKind: "null"), "")
        XCTAssertEqual(noKind.editableDestination(forKind: "email"), "")
    }

    func testReadsTheServersMailboxesForAnAlias() async throws {
        let (connection, _) = connection(
            answering: #"""
                {"data":{"ListAllMailboxes":[{"id":"mailbox-1","name":"Inbox","userId":"user-1","username":"robin","userName":"Robin Example"},
                  {"id":"mailbox-2","name":"Shared","userId":"user-2","username":"sam","userName":"sam"}]}}
                """#)
        let mailboxes = try await connection.serverMailboxes()
        XCTAssertEqual(mailboxes.map(\.pickerText), ["robin (Robin Example) · Inbox", "sam · Shared"])
        XCTAssertEqual(mailboxes[0].rowText, "robin · Inbox")
    }

    func testMakesACredentialShownOnce() async throws {
        let (connection, fake) = connection(
            answering: #"{"data":{"CreateCredential":{"username":"made-up-user","password":"made-up-password","host":"mail.example.com","port":"587"}}}"#)
        let created = try await connection.createDomainCredential(domainId: "domain-1", comment: "printer")
        XCTAssertEqual(created.password, "made-up-password")
        XCTAssertEqual(created.port, "587")
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["domainId": .string("domain-1"), "comment": .string("printer")])
    }

    func testChangesACredentialWithoutTouchingItsPassword() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"UpdateCredential":{"id":"credential-1"}}}"#)
        try await connection.updateDomainCredential(credentialId: "credential-1", comment: "printer", restrictedLocalPart: "scans", isDisabled: true)
        XCTAssertTrue(try sentQuery(fake.sent[0]).contains("UpdateCredential"))
        XCTAssertFalse(try sentQuery(fake.sent[0]).contains("password"))
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["credentialId": .string("credential-1"), "comment": .string("printer"), "alias": .string("scans"), "disabled": .bool(true)])
    }

    func testChecksTheDNSAgainAfterChangingWhatToPublish() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"UpdateDomain":{"id":"domain-1"},"CheckDomain":{"id":"domain-1"},"DeleteDomainLogo":true}}"#)
        try await connection.updateDomainMailServers(domainId: "domain-1", mailServers: ManagedWords.mailServerNames(" mx1.example.com, ,mx2.example.com "))
        XCTAssertEqual(
            try sentVariables(fake.sent[0]),
            ["domainId": .string("domain-1"), "mailServers": .array([.string("mx1.example.com"), .string("mx2.example.com")])])
        XCTAssertTrue(try sentQuery(fake.sent[1]).contains("CheckDomain"))
        try await connection.updateDomainDkimSelector(domainId: "domain-1", selector: "sel2")
        XCTAssertTrue(try sentQuery(fake.sent[3]).contains("CheckDomain"))
        try await connection.updateDomainLinkHost(domainId: "domain-1", linkHost: "")
        XCTAssertEqual(fake.sent.count, 5, "the host for pictures changes no record")
        try await connection.deleteDomainLogo(domainId: "domain-1")
        XCTAssertTrue(try sentQuery(fake.sent[6]).contains("CheckDomain"))
    }

    func testSaysWhyALogoWasRefused() async throws {
        let (connection, fake) = connection(answering: #"{"error":"the mark is not square"}"#, statusCode: 400)
        do {
            try await connection.uploadDomainLogo(domainId: "domain-1", fileName: "mark.svg", svgData: Data("<svg/>".utf8))
            XCTFail("a refused file uploads")
        } catch TeaNodeError.refused(let refusalMessage) {
            XCTAssertEqual(refusalMessage, "the mark is not square")
        }
        XCTAssertEqual(fake.sent[0].url?.path, "/api/v1/domains/domain-1/logo")
        XCTAssertEqual(fake.sent.count, 1, "nothing is checked after a refusal")
    }

    func testNamesDKIMRecordsAndHosts() {
        XCTAssertEqual(ManagedWords.dkimRecordName(selector: "sel1", domainName: "example.com"), "sel1._domainkey.example.com")
        XCTAssertEqual(ManagedWords.dkimRecordName(selector: "", domainName: "example.com"), "example.com")
        XCTAssertEqual(ManagedWords.hostName("mx.example.net."), "mx.example.net")
    }

    // MARK: Templates

    func testReadsTemplatesAndLayoutsAndCountsWhatSitsInEachLayout() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"ListTemplates":[
                  {"id":"template-1","name":"welcome","comment":"","locale":"en","layoutId":"layout-1","modifiedAt":"2030-01-01T00:00:00Z",
                   "variables":["name"],"translations":[{"locale":"ja"}]},
                  {"id":"template-2","name":"reset","locale":"","layoutId":"","variables":[],"translations":[]}],
                  "ListLayouts":[{"id":"layout-1","comment":"with the footer","locale":"en","translations":[]}]}}
                """#)
        let (templates, layouts) = try await connection.domainTemplates(domainId: "domain-1")
        XCTAssertEqual(templates.map(\.name), ["welcome", "reset"])
        XCTAssertEqual(templates[0].locales, ["en", "ja"])
        XCTAssertEqual(templates[1].locales, [])
        XCTAssertEqual(layouts.first?.comment, "with the footer")
        XCTAssertEqual(DomainLayout.templateCountByLayoutId(templates), ["layout-1": 1])
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["domainId": .string("domain-1")])
    }

    func testReadsOneTemplateAndRemovesTemplatesAndLayouts() async throws {
        let (connection, fake) = connection(
            answering: #"""
                {"data":{"GetTemplate":{"id":"template-1","name":"welcome","subject":"Hello","htmlContent":"<p>Hi</p>","textContent":"Hi",
                  "translations":[{"locale":"ja","subject":"Konnichiwa","htmlContent":"","textContent":"Konnichiwa"}]},
                  "DeleteTemplate":true,"DeleteLayout":true}}
                """#)
        let template = try await connection.domainTemplate(templateId: "template-1")
        XCTAssertEqual(template?.translations?.first?.textContent, "Konnichiwa")
        try await connection.deleteDomainTemplate(templateId: "template-1")
        try await connection.deleteDomainLayout(layoutId: "layout-1")
        XCTAssertEqual(try sentVariables(fake.sent[1]), ["templateId": .string("template-1")])
        XCTAssertEqual(try sentVariables(fake.sent[2]), ["layoutId": .string("layout-1")])
    }
}
