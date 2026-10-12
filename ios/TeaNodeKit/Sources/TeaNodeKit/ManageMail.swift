import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

// MARK: - Who may open the management side

/// OperatorAccess is what the signed-in person may open on the management
/// side, as the dashboard reads it from the session: whether they hold any
/// permission that opens it at all, and which of its pages they may open.
public struct OperatorAccess: Hashable, Sendable {
    /// DomainPermissions are the permissions held over one domain.
    public struct DomainPermissions: Codable, Hashable, Sendable {
        public let domainId: String
        public let permissions: [String]
    }

    /// Whether any permission that opens the management side is held: the
    /// session's `manages`.
    public let isOperator: Bool
    /// Server permissions, and domain permissions held over every domain.
    public let everywherePermissions: [String]
    public let domainPermissions: [DomainPermissions]

    public init(isOperator: Bool, everywherePermissions: [String] = [], domainPermissions: [DomainPermissions] = []) {
        self.isOperator = isOperator
        self.everywherePermissions = everywherePermissions
        self.domainPermissions = domainPermissions
    }

    /// Whether a domain permission is held over at least one domain, or
    /// everywhere through the all-domains permission that widens it, as the
    /// dashboard's hasAnywhere decides.
    public func hasAnywhere(_ permission: String) -> Bool {
        let isHeldEverywhere = everywherePermissions.contains { held in
            held == permission || (held.hasSuffix("-all") && String(held.dropLast("-all".count)) == permission)
        }
        return isHeldEverywhere || domainPermissions.contains { $0.permissions.contains(permission) }
    }

    /// The rail's rows: all mail needs mail:audit, the queue queue:manage,
    /// the reports report:read and the domains domain:manage.
    public var canAuditMail: Bool { hasAnywhere("mail:audit") }
    public var canManageQueue: Bool { hasAnywhere("queue:manage") }
    public var canReadReports: Bool { hasAnywhere("report:read") }
    public var canManageDomains: Bool { hasAnywhere("domain:manage") }

    /// Whether any of the mail delivery pages may be opened.
    public var canOpenMailDelivery: Bool { canAuditMail || canManageQueue || canReadReports || canManageDomains }
}

// MARK: - Words the pages share

/// ManagedTone is the color the dashboard gives a status or a verdict:
/// good, bad, a warning, or nothing to say.
public enum ManagedTone: String, Sendable {
    case good, bad, warn, plain

    /// The dashboard's toneFor.
    public static func of(_ statusText: String?) -> ManagedTone {
        switch (statusText ?? "").lowercased() {
        case "pass", "delivered", "accepted", "none": .good
        case "fail", "rejected", "dropped", "hardfail", "permerror": .bad
        case "softfail", "neutral", "temperror", "queued", "attempted", "delayed": .warn
        default: .plain
        }
    }
}

/// ManagedLocation is where an address was placed: a city and a country.
public struct ManagedLocation: Codable, Hashable, Sendable {
    public let city: String?
    public let country: String?

    /// "City, Country", or empty when the country is not known.
    public var placeText: String {
        guard let country, !country.isEmpty else { return "" }
        return [city ?? "", country].filter { !$0.isEmpty }.joined(separator: ", ")
    }
}

/// ManagedWords are the small rules the management pages share.
public enum ManagedWords {
    /// How many rows the mail and report lists ask for, as the dashboard
    /// does; filtering happens over these.
    public static let listLimit = 500
    /// How many rows a list shows at a time, as the dashboard's tables do.
    public static let pageSize = 50

    /// A spam score to one decimal, written the way the dashboard writes it:
    /// 3.0 as 3, -1.0 as -1.
    public static func score(_ score: Double) -> String {
        let rounded = (score * 10).rounded() / 10
        if rounded == rounded.rounded() { return String(Int(rounded)) }
        return String(format: "%.1f", rounded)
    }

    /// A host name without the dot that closes it in DNS.
    public static func hostName(_ written: String?) -> String {
        guard var written else { return "" }
        if written.hasSuffix(".") { written.removeLast() }
        return written
    }

    /// The name a DKIM key is published at: selector._domainkey.domain.
    public static func dkimRecordName(selector: String?, domainName: String?) -> String {
        if let selector, !selector.isEmpty, let domainName, !domainName.isEmpty { return "\(selector)._domainkey.\(domainName)" }
        return domainName ?? ""
    }

    /// Mail server names typed into one field, comma separated.
    public static func mailServerNames(_ typed: String) -> [String] {
        typed.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
    }

    /// Whether every word typed is found, in any case, in one of the texts.
    public static func matches(_ searchText: String, in texts: [String?]) -> Bool {
        let words = searchText.lowercased().split(whereSeparator: \.isWhitespace)
        guard !words.isEmpty else { return true }
        let haystack = texts.compactMap { $0?.lowercased() }.joined(separator: " ")
        return words.allSatisfy { haystack.contains($0) }
    }
}

// MARK: - All mail

/// AuditedMail is a message as the operator's mail pages show it: a row of
/// the list, and with the detail fields when it is opened.
public struct AuditedMail: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let domainId: String?
    public let sender: String?
    public let from: String?
    public let subject: String?
    public let mailStatus: String?
    public let mailKind: String?
    public let byteCount: Int?
    public let receivedAt: String?
    public let recipients: [String]?
    public let ip: String?
    public let reverseDnsName: String?
    public let hello: String?
    public let messageId: String?
    public let envelopeId: String?
    public let tlsVersion: String?
    public let tlsCipherSuite: String?
    public let location: ManagedLocation?
    public let authenticationResults: MailAuthenticationResults?

    enum CodingKeys: String, CodingKey {
        case id, domainId, sender, from, subject, receivedAt, recipients, ip, hello, messageId, envelopeId, tlsVersion
        case tlsCipherSuite, location, authenticationResults
        case mailStatus = "status"
        case mailKind = "kind"
        case byteCount = "size"
        case reverseDnsName = "rdns"
    }

    public init(
        id: String, domainId: String? = nil, sender: String? = nil, from: String? = nil, subject: String? = nil,
        mailStatus: String? = nil, mailKind: String? = nil, byteCount: Int? = nil, receivedAt: String? = nil,
        authenticationResults: MailAuthenticationResults? = nil
    ) {
        self.id = id
        self.domainId = domainId
        self.sender = sender
        self.from = from
        self.subject = subject
        self.mailStatus = mailStatus
        self.mailKind = mailKind
        self.byteCount = byteCount
        self.receivedAt = receivedAt
        self.recipients = nil
        self.ip = nil
        self.reverseDnsName = nil
        self.hello = nil
        self.messageId = nil
        self.envelopeId = nil
        self.tlsVersion = nil
        self.tlsCipherSuite = nil
        self.location = nil
        self.authenticationResults = authenticationResults
    }

    /// Who it is from: the From header, or the envelope sender without one.
    public var shownSender: String {
        if let from, !from.isEmpty { return from }
        return sender ?? ""
    }
}

/// MailAuthenticationResults are the checks a message went through on
/// arrival, each with what it looked at to decide.
public struct MailAuthenticationResults: Codable, Hashable, Sendable {
    /// MXLookup is where a domain the message claims receives its mail.
    public struct MXLookup: Codable, Hashable, Sendable {
        public let domain: String?
        public let mailServers: [String]?
    }

    /// SPFCheck is whether the domain authorizes the address that sent it.
    public struct SPFCheck: Codable, Hashable, Sendable {
        public let domain: String?
        public let ip: String?
        public let spfResult: String?

        enum CodingKeys: String, CodingKey {
            case domain, ip
            case spfResult = "result"
        }
    }

    /// DMARCCheck is whether the domain's own policy is satisfied.
    public struct DMARCCheck: Codable, Hashable, Sendable {
        public let domain: String?
        public let policy: String?
        public let subdomainPolicy: String?
        public let dkimAlignment: String?
        public let spfAlignment: String?
        public let dmarcResult: String?

        enum CodingKeys: String, CodingKey {
            case domain, policy, subdomainPolicy, dkimAlignment, spfAlignment
            case dmarcResult = "result"
        }
    }

    /// DKIMCheck is one signature on the message and whether it holds.
    public struct DKIMCheck: Codable, Hashable, Sendable {
        public let domain: String?
        public let selector: String?
        public let identifier: String?
        public let dkimResult: String?

        enum CodingKeys: String, CodingKey {
            case domain, selector, identifier
            case dkimResult = "result"
        }
    }

    /// ARCCheck is the sealed chain of the servers it passed through.
    public struct ARCCheck: Codable, Hashable, Sendable {
        public let arcResult: String?
        public let instanceCount: Int?

        enum CodingKeys: String, CodingKey {
            case arcResult = "result"
            case instanceCount = "instances"
        }
    }

    /// SpamRule is one rule of the built-in filter that matched, and the
    /// points it added.
    public struct SpamRule: Codable, Hashable, Sendable {
        public let symbol: String
        public let score: Double
        public let ruleDescription: String?

        enum CodingKeys: String, CodingKey {
            case symbol, score
            case ruleDescription = "description"
        }
    }

    /// SpamFilterCheck is how the spam filter scored the message.
    public struct SpamFilterCheck: Codable, Hashable, Sendable {
        public let score: Double
        public let spamResult: String?
        public let symbols: [String]?
        public let checks: [SpamRule]?

        enum CodingKeys: String, CodingKey {
            case score, symbols, checks
            case spamResult = "result"
        }
    }

    /// AntivirusCheck is what the virus scanner found.
    public struct AntivirusCheck: Codable, Hashable, Sendable {
        public let viruses: [String]?
    }

    /// ContentFilterCheck is the attachments refused by their kind.
    public struct ContentFilterCheck: Codable, Hashable, Sendable {
        public let unsafeExtensions: [String]?
    }

    public let senderMx: MXLookup?
    public let fromMx: MXLookup?
    public let spf: SPFCheck?
    public let dmarc: DMARCCheck?
    public let dkims: [DKIMCheck]?
    public let arc: ARCCheck?
    public let spamFilter: SpamFilterCheck?
    public let antivirus: AntivirusCheck?
    public let contentFilter: ContentFilterCheck?
    public let errorMessages: [String]?

    enum CodingKeys: String, CodingKey {
        case senderMx, fromMx, spf, dmarc, dkims, arc, spamFilter, antivirus, contentFilter
        case errorMessages = "errors"
    }

    public init(
        spf: SPFCheck? = nil, dmarc: DMARCCheck? = nil, dkims: [DKIMCheck]? = nil, spamFilter: SpamFilterCheck? = nil,
        antivirus: AntivirusCheck? = nil, errorMessages: [String]? = nil
    ) {
        self.senderMx = nil
        self.fromMx = nil
        self.spf = spf
        self.dmarc = dmarc
        self.dkims = dkims
        self.arc = nil
        self.spamFilter = spamFilter
        self.antivirus = antivirus
        self.contentFilter = nil
        self.errorMessages = errorMessages
    }
}

/// AuditedMailOpens is what is known about a sent message's pictures having
/// been fetched: a floor with false positives in it, never a count of
/// readers.
public struct AuditedMailOpens: Codable, Hashable, Sendable {
    public let mailId: String?
    /// Whether the message carries a picture that could be fetched at all.
    public let isTrackable: Bool
    public let isOpened: Bool
    public let openedAt: String?
    public let lastOpenedAt: String?
    public let openCount: Int?
    public let ip: String?

    enum CodingKeys: String, CodingKey {
        case mailId, openedAt, lastOpenedAt, openCount, ip
        case isTrackable = "trackable"
        case isOpened = "opened"
    }
}

/// MailDelivery is one recipient of a message and what happened about it:
/// a row of the delivery queue, or a delivery on the message's page.
public struct MailDelivery: Codable, Hashable, Sendable, Identifiable {
    /// RecipientReport is what the far end said about one recipient, read
    /// from the bounce it sent.
    public struct RecipientReport: Codable, Hashable, Sendable {
        public let finalRecipient: String?
        public let reportAction: String?
        public let reportStatus: String?
        public let remoteMta: String?
        public let diagnosticCode: String?
        public let lastAttemptDate: String?

        enum CodingKeys: String, CodingKey {
            case finalRecipient, remoteMta, diagnosticCode, lastAttemptDate
            case reportAction = "action"
            case reportStatus = "status"
        }
    }

    /// StatusReport is one bounce: the server that wrote it, and what it
    /// said about each recipient.
    public struct StatusReport: Codable, Hashable, Sendable {
        public let reportingMta: String?
        public let recipientStatuses: [RecipientReport]?
    }

    public let id: String
    public let mailId: String?
    public let domainId: String?
    public let recipient: String?
    public let deliveryKind: String?
    public let deliveryStatus: String?
    public let byteCount: Int?
    public let attemptCount: Int?
    public let errorMessage: String?
    /// email, mailServer, webhook or smtp: how it is handed on.
    public let deliveryMethod: String?
    public let destination: String?
    public let attemptedAt: String?
    public let deliveredAt: String?
    public let droppedAt: String?
    public let notifiedAt: String?
    public let retryAt: String?
    public let statusReports: [StatusReport]?

    enum CodingKeys: String, CodingKey {
        case id, mailId, domainId, recipient, destination, attemptedAt, deliveredAt, droppedAt, notifiedAt, retryAt
        case deliveryKind = "kind"
        case deliveryStatus = "status"
        case byteCount = "size"
        case attemptCount = "attempts"
        case errorMessage = "error"
        case deliveryMethod = "method"
        case statusReports = "deliveryStatuses"
    }

    public init(id: String, domainId: String? = nil, recipient: String? = nil, deliveryStatus: String? = nil, retryAt: String? = nil) {
        self.id = id
        self.mailId = nil
        self.domainId = domainId
        self.recipient = recipient
        self.deliveryKind = nil
        self.deliveryStatus = deliveryStatus
        self.byteCount = nil
        self.attemptCount = nil
        self.errorMessage = nil
        self.deliveryMethod = nil
        self.destination = nil
        self.attemptedAt = nil
        self.deliveredAt = nil
        self.droppedAt = nil
        self.notifiedAt = nil
        self.retryAt = retryAt
        self.statusReports = nil
    }

    /// Every recipient the far end reported on, across its bounces, with the
    /// server that reported it.
    public var recipientReports: [(reportingMta: String?, report: RecipientReport)] {
        (statusReports ?? []).flatMap { statusReport in
            (statusReport.recipientStatuses ?? []).map { (statusReport.reportingMta, $0) }
        }
    }

    /// Whether it has failed at least once: put off to a later attempt, or
    /// failed outright. One still on its first pass is simply in flight.
    public var isFailing: Bool { deliveryStatus == "failed" || !(retryAt ?? "").isEmpty }
}

/// MailSpamTraining is the label a message was taught to the filter with,
/// and how many messages the classifier has learned since.
public struct MailSpamTraining: Codable, Hashable, Sendable {
    /// spam or ham, or empty once forgotten.
    public let trainingLabel: String
    public let learnedSpamCount: Int
    public let learnedHamCount: Int

    enum CodingKeys: String, CodingKey {
        case trainingLabel = "label"
        case learnedSpamCount = "learnedSpam"
        case learnedHamCount = "learnedHam"
    }
}

/// AntispamClassifier is the part of the server's spam settings the message
/// page reads: which filter runs, and how far its classifier has learned.
public struct AntispamClassifier: Codable, Hashable, Sendable {
    public let effectiveEngine: String
    public let isBayesEnabled: Bool
    public let bayesMinimumMessageCount: Int
    public let bayesLearnedSpamCount: Int
    public let bayesLearnedHamCount: Int

    enum CodingKeys: String, CodingKey {
        case effectiveEngine
        case isBayesEnabled = "bayesEnabled"
        case bayesMinimumMessageCount = "bayesMinimumMessages"
        case bayesLearnedSpamCount = "bayesLearnedSpam"
        case bayesLearnedHamCount = "bayesLearnedHam"
    }

    /// Whether a message can be taught to the filter: only the built-in
    /// filter with its classifier on reads what it is taught.
    public var canBeTaught: Bool { effectiveEngine == "builtin" && isBayesEnabled }
}

/// AuditedMailDetail is everything the operator's page for one message
/// shows: the message and its checks, its content, its pictures, its
/// deliveries and what the filter was taught about it.
public struct AuditedMailDetail: Hashable, Sendable {
    public let mail: AuditedMail
    public let content: MailContent?
    public let opens: AuditedMailOpens?
    public let deliveries: [MailDelivery]
    /// spam, ham, or nil when it was never taught.
    public let trainingLabel: String?
    public let classifier: AntispamClassifier?
}

/// MailOutcome is what happened to a message, in one line, as the top of
/// the dashboard's message page says it.
public enum MailOutcome: Hashable, Sendable {
    case rejectedForViruses([String])
    case rejectedAsSpam(score: Double)
    case rejectedByDMARC(policy: String)
    case rejectedWithErrors([String])
    case rejected
    case noDeliveries
    case partlyDelivered(deliveredCount: Int, failedCount: Int, totalCount: Int)
    case delivered(deliveredCount: Int)
    case pending(pendingCount: Int)

    public static func of(_ mail: AuditedMail, deliveries: [MailDelivery]) -> MailOutcome {
        if mail.mailStatus == "rejected" {
            let results = mail.authenticationResults
            if let viruses = results?.antivirus?.viruses, !viruses.isEmpty { return .rejectedForViruses(viruses) }
            if let spamFilter = results?.spamFilter, spamFilter.spamResult == "fail" { return .rejectedAsSpam(score: spamFilter.score) }
            if let dmarc = results?.dmarc, let dmarcResult = dmarc.dmarcResult, !dmarcResult.isEmpty, ManagedTone.of(dmarcResult) == .bad {
                return .rejectedByDMARC(policy: dmarc.policy ?? "none")
            }
            if let errorMessages = results?.errorMessages, !errorMessages.isEmpty { return .rejectedWithErrors(errorMessages) }
            return .rejected
        }
        if deliveries.isEmpty { return .noDeliveries }
        let deliveredCount = deliveries.filter { $0.deliveryStatus == "delivered" }.count
        let failedCount = deliveries.filter { $0.deliveryStatus == "failed" || $0.deliveryStatus == "dropped" }.count
        if failedCount > 0 { return .partlyDelivered(deliveredCount: deliveredCount, failedCount: failedCount, totalCount: deliveries.count) }
        if deliveredCount == deliveries.count { return .delivered(deliveredCount: deliveredCount) }
        return .pending(pendingCount: deliveries.count - deliveredCount)
    }
}

/// MailListFilter narrows the operator's list of mail the way the
/// dashboard's column filters do: by domain, status and kind, and by words
/// in the sender or the subject.
public struct MailListFilter: Hashable, Sendable {
    public var domainId: String?
    public var mailStatus: String?
    public var mailKind: String?
    public var searchText = ""

    public init(domainId: String? = nil, mailStatus: String? = nil, mailKind: String? = nil, searchText: String = "") {
        self.domainId = domainId
        self.mailStatus = mailStatus
        self.mailKind = mailKind
        self.searchText = searchText
    }

    public var isFiltering: Bool { domainId != nil || mailStatus != nil || mailKind != nil || !searchText.isEmpty }

    public func matches(_ mail: AuditedMail) -> Bool {
        if let domainId, mail.domainId != domainId { return false }
        if let mailStatus, mail.mailStatus != mailStatus { return false }
        if let mailKind, mail.mailKind != mailKind { return false }
        return ManagedWords.matches(searchText, in: [mail.from, mail.sender, mail.subject])
    }

    public func applied(to mails: [AuditedMail]) -> [AuditedMail] { mails.filter(matches) }
}

/// DeliveryQueueFilter narrows the delivery queue by domain, status and
/// kind, and by words in the recipient or the last error.
public struct DeliveryQueueFilter: Hashable, Sendable {
    public var domainId: String?
    public var deliveryStatus: String?
    public var deliveryKind: String?
    public var searchText = ""

    public init(domainId: String? = nil, deliveryStatus: String? = nil, deliveryKind: String? = nil, searchText: String = "") {
        self.domainId = domainId
        self.deliveryStatus = deliveryStatus
        self.deliveryKind = deliveryKind
        self.searchText = searchText
    }

    public var isFiltering: Bool { domainId != nil || deliveryStatus != nil || deliveryKind != nil || !searchText.isEmpty }

    public func applied(to deliveries: [MailDelivery]) -> [MailDelivery] {
        deliveries.filter { delivery in
            if let domainId, delivery.domainId != domainId { return false }
            if let deliveryStatus, delivery.deliveryStatus != deliveryStatus { return false }
            if let deliveryKind, delivery.deliveryKind != deliveryKind { return false }
            return ManagedWords.matches(searchText, in: [delivery.recipient, delivery.errorMessage])
        }
    }
}

// MARK: - Reports

/// ReportAlignment is the three states a DMARC aggregate report's row can
/// be in: aligned on both counts, on one (which is a pass), or on neither.
public enum ReportAlignment: String, CaseIterable, Sendable {
    case both, one, neither

    public var isPassed: Bool { self != .neither }
}

/// DMARCReport is one row of a DMARC aggregate report: somebody else telling
/// you what they did with mail that claimed to come from one of your
/// domains. Opened, it carries the report as it arrived.
public struct DMARCReport: Codable, Hashable, Sendable, Identifiable {
    /// Feedback is the report as it arrived: who sent it, the policy they
    /// saw, and what they did with each batch of mail.
    public struct Feedback: Codable, Hashable, Sendable {
        public let organizationName: String?
        public let email: String?
        public let extraContactInfo: String?
        public let reportId: String?
        public let beginEpochSeconds: Double?
        public let endEpochSeconds: Double?
        public let errorMessages: [String]?
        public let domain: String?
        public let dkimAlignment: String?
        public let spfAlignment: String?
        public let policy: String?
        public let subdomainPolicy: String?
        public let policyPercent: Int?
        public let failureOptions: String?
        public let records: [FeedbackRecord]?

        enum CodingKeys: String, CodingKey {
            case organizationName, email, extraContactInfo, reportId, domain, dkimAlignment, spfAlignment, policy, subdomainPolicy
            case failureOptions, records
            case beginEpochSeconds = "begin"
            case endEpochSeconds = "end"
            case errorMessages = "errors"
            case policyPercent = "percent"
        }

        /// The policy as published: p=, sp=, pct= when not all of it, fo=.
        public var policyText: String {
            var parts: [String] = []
            if let policy, !policy.isEmpty { parts.append("p=\(policy)") }
            if let subdomainPolicy, !subdomainPolicy.isEmpty { parts.append("sp=\(subdomainPolicy)") }
            if let policyPercent, policyPercent != 100 { parts.append("pct=\(policyPercent)") }
            if let failureOptions, !failureOptions.isEmpty { parts.append("fo=\(failureOptions)") }
            return parts.joined(separator: " · ")
        }
    }

    /// FeedbackRecord is a batch of messages from one address, what the
    /// receiver decided, and why.
    public struct FeedbackRecord: Codable, Hashable, Sendable {
        /// DKIMResult is one signature the receiver checked.
        public struct DKIMResult: Codable, Hashable, Sendable {
            public let domain: String?
            public let selector: String?
            public let dkimResult: String?
            public let humanResult: String?

            enum CodingKeys: String, CodingKey {
                case domain, selector, humanResult
                case dkimResult = "result"
            }
        }

        /// SPFResult is one SPF check the receiver made.
        public struct SPFResult: Codable, Hashable, Sendable {
            public let domain: String?
            public let scope: String?
            public let spfResult: String?

            enum CodingKeys: String, CodingKey {
                case domain, scope
                case spfResult = "result"
            }
        }

        public let sourceIp: String?
        public let messageCount: Int?
        public let disposition: String?
        public let dkimEvaluation: String?
        public let spfEvaluation: String?
        public let reasonType: String?
        public let reasonComment: String?
        public let headerFrom: String?
        public let envelopeFrom: String?
        public let envelopeTo: String?
        public let dkimResults: [DKIMResult]?
        public let spfResults: [SPFResult]?

        enum CodingKeys: String, CodingKey {
            case sourceIp, disposition, reasonType, reasonComment, headerFrom, envelopeFrom, envelopeTo
            case messageCount = "count"
            case dkimEvaluation = "dkim"
            case spfEvaluation = "spf"
            case dkimResults = "dkims"
            case spfResults = "spfs"
        }

        /// Whether either evaluation passed.
        public var isPassed: Bool { dkimEvaluation == "pass" || spfEvaluation == "pass" }
    }

    public let id: String
    public let domainId: String?
    public let beginAt: String?
    public let endAt: String?
    public let messageCount: Int?
    public let ip: String?
    public let reverseDnsName: String?
    public let fromDomain: String?
    public let senderDomain: String?
    public let disposition: String?
    public let isDkimAligned: Bool?
    public let isSpfAligned: Bool?
    public let location: ManagedLocation?
    public let feedback: Feedback?

    enum CodingKeys: String, CodingKey {
        case id, domainId, beginAt, endAt, ip, fromDomain, senderDomain, disposition, location, feedback
        case messageCount = "count"
        case reverseDnsName = "rdns"
        case isDkimAligned = "dkimAligned"
        case isSpfAligned = "spfAligned"
    }

    public init(id: String, domainId: String? = nil, fromDomain: String? = nil, disposition: String? = nil, isDkimAligned: Bool, isSpfAligned: Bool) {
        self.id = id
        self.domainId = domainId
        self.beginAt = nil
        self.endAt = nil
        self.messageCount = nil
        self.ip = nil
        self.reverseDnsName = nil
        self.fromDomain = fromDomain
        self.senderDomain = nil
        self.disposition = disposition
        self.isDkimAligned = isDkimAligned
        self.isSpfAligned = isSpfAligned
        self.location = nil
        self.feedback = nil
    }

    public var alignment: ReportAlignment {
        let isDkimAligned = isDkimAligned ?? false
        let isSpfAligned = isSpfAligned ?? false
        if isDkimAligned && isSpfAligned { return .both }
        return isDkimAligned || isSpfAligned ? .one : .neither
    }
}

/// ReportListFilter narrows the reports by domain, alignment and what the
/// receiver did, and by words in the claimed domain or the sender.
public struct ReportListFilter: Hashable, Sendable {
    public var domainId: String?
    public var alignment: ReportAlignment?
    public var disposition: String?
    public var searchText = ""

    public init(domainId: String? = nil, alignment: ReportAlignment? = nil, disposition: String? = nil, searchText: String = "") {
        self.domainId = domainId
        self.alignment = alignment
        self.disposition = disposition
        self.searchText = searchText
    }

    public var isFiltering: Bool { domainId != nil || alignment != nil || disposition != nil || !searchText.isEmpty }

    public func applied(to reports: [DMARCReport]) -> [DMARCReport] {
        reports.filter { report in
            if let domainId, report.domainId != domainId { return false }
            if let alignment, report.alignment != alignment { return false }
            if let disposition, (report.disposition ?? "") != disposition { return false }
            return ManagedWords.matches(searchText, in: [report.fromDomain, report.ip, report.reverseDnsName])
        }
    }
}

// MARK: - Domains

/// DNSRecord is one record a domain should publish, what was found, and
/// whether they match.
public struct DNSRecord: Codable, Hashable, Sendable {
    public let recordType: String?
    public let name: String?
    public let expectedValue: String?
    /// The preference to publish an MX record's host at.
    public let priority: Int?
    /// Worth publishing, but nothing breaks without it.
    public let isOptional: Bool?
    public let foundValues: [String]?
    public let isVerified: Bool?
    public let purpose: String?
    /// What has to be true elsewhere before this record does anything.
    public let blockedReason: String?

    enum CodingKeys: String, CodingKey {
        case name, priority, purpose
        case recordType = "type"
        case expectedValue = "expected"
        case isOptional = "optional"
        case foundValues = "found"
        case isVerified = "verified"
        case blockedReason = "blocked"
    }

    public init(recordType: String, name: String = "", expectedValue: String = "", priority: Int? = nil, isOptional: Bool = false, isVerified: Bool) {
        self.recordType = recordType
        self.name = name
        self.expectedValue = expectedValue
        self.priority = priority
        self.isOptional = isOptional
        self.foundValues = nil
        self.isVerified = isVerified
        self.purpose = nil
        self.blockedReason = nil
    }

    /// What to publish, as one string: an MX record's preference and host
    /// together.
    public var publishedValue: String {
        if let priority, priority != 0 { return "\(priority) \(expectedValue ?? "")" }
        return expectedValue ?? ""
    }

    /// Whether this record is one the mail needs and is not published right.
    public var isMissing: Bool { isVerified != true && isOptional != true }
}

/// DomainDNSCheck is the last check of a domain's DNS: when, what went
/// wrong, and each record.
public struct DomainDNSCheck: Codable, Hashable, Sendable {
    public let checkedAt: String?
    public let errorMessage: String?
    public let records: [DNSRecord]?

    enum CodingKeys: String, CodingKey {
        case checkedAt, records
        case errorMessage = "error"
    }

    public init(checkedAt: String? = nil, records: [DNSRecord]) {
        self.checkedAt = checkedAt
        self.errorMessage = nil
        self.records = records
    }
}

/// DomainDNSSummary is the domain's DNS in one word, as the domain list
/// and overview say it. Optional records are not counted.
public enum DomainDNSSummary: Hashable, Sendable {
    case notChecked
    case allPublished(recordCount: Int)
    case missing(missingCount: Int)

    public static func of(_ check: DomainDNSCheck?) -> DomainDNSSummary {
        let records = check?.records ?? []
        if records.isEmpty { return .notChecked }
        let missingCount = records.filter(\.isMissing).count
        return missingCount == 0 ? .allPublished(recordCount: records.count) : .missing(missingCount: missingCount)
    }
}

/// DomainAlias is an address on a domain and where its mail goes.
public struct DomainAlias: Codable, Hashable, Sendable, Identifiable {
    /// MailServer is a mail server an alias relays to.
    public struct MailServer: Codable, Hashable, Sendable {
        public let host: String?
        public let port: Int?
        public let username: String?
    }

    public let id: String
    /// Empty for a catch-all.
    public let pattern: String?
    public let comment: String?
    /// email, webhook, mailServer, mailbox or null.
    public let aliasKind: String?
    public let email: String?
    public let webhook: String?
    public let mailboxId: String?
    public let isDisabled: Bool?
    public let mailServer: MailServer?

    enum CodingKeys: String, CodingKey {
        case id, pattern, comment, email, webhook, mailboxId, mailServer
        case aliasKind = "kind"
        case isDisabled = "disabled"
    }

    /// The kinds a new alias may be, in the order the dashboard offers them.
    public static let creatableKinds = ["mailbox", "email", "webhook", "null"]

    /// The kinds an existing alias may be changed to: the creatable ones,
    /// and a mail server relay when the alias already is one, since the
    /// relay's host and port are not edited here and so cannot be made new.
    public var selectableKinds: [String] {
        aliasKind == "mailServer" ? DomainAlias.creatableKinds + ["mailServer"] : DomainAlias.creatableKinds
    }

    /// Whether the destination the kind needs has been filled in: an
    /// address to forward to, a URL to post to, or a mailbox to deliver
    /// into. Discarding and relaying need none here.
    public static func isDestinationFilled(aliasKind: String, destination: String, mailboxId: String) -> Bool {
        switch aliasKind {
        case "email", "webhook": return !destination.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        case "mailbox": return !mailboxId.isEmpty
        default: return true
        }
    }

    /// The text to put in the destination field when editing: the address
    /// or URL the alias has now, empty for any other kind.
    public var editableDestination: String {
        switch aliasKind ?? "" {
        case "email": return email ?? ""
        case "webhook": return webhook ?? ""
        default: return ""
        }
    }
}

/// DomainCredential is a username and password one machine sends through
/// the domain with.
public struct DomainCredential: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let comment: String?
    public let alias: String?
    public let isDisabled: Bool?

    enum CodingKeys: String, CodingKey {
        case id, comment, alias
        case isDisabled = "disabled"
    }
}

/// CreatedDomainCredential is a credential just made, with its password,
/// shown this once.
public struct CreatedDomainCredential: Codable, Hashable, Sendable {
    public let username: String
    public let password: String
    public let host: String
    public let port: String
}

/// DomainLogo is the mark this server publishes for a domain, which its
/// BIMI record points at.
public struct DomainLogo: Codable, Hashable, Sendable {
    public let filename: String?
    public let title: String?
    public let url: String?
    public let publicUrl: String?
    public let uploadedAt: String?
}

/// ManagedDomain is a domain this server receives mail for, with what the
/// operator's pages show of it. The list asks for less than one domain's
/// page, so what was not asked for is nil.
public struct ManagedDomain: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let domainName: String
    public let subdomain: String?
    public let comment: String?
    public let spamFilterScoreThreshold: Double?
    public let aliases: [DomainAlias]?
    public let credentials: [DomainCredential]?
    public let dnsCheck: DomainDNSCheck?
    public let mailServers: [String]?
    public let mailHosts: [String]?
    public let linkHost: String?
    public let linkHostname: String?
    public let dkimSelector: String?
    public let hasDkimKey: Bool?
    public let logo: DomainLogo?

    enum CodingKeys: String, CodingKey {
        case id, subdomain, comment, spamFilterScoreThreshold, aliases, credentials, mailServers, mailHosts, linkHost, linkHostname
        case dkimSelector, hasDkimKey, logo
        case domainName = "domain"
        case dnsCheck = "records"
    }

    public init(id: String, domainName: String, dnsCheck: DomainDNSCheck? = nil) {
        self.id = id
        self.domainName = domainName
        self.subdomain = nil
        self.comment = nil
        self.spamFilterScoreThreshold = nil
        self.aliases = nil
        self.credentials = nil
        self.dnsCheck = dnsCheck
        self.mailServers = nil
        self.mailHosts = nil
        self.linkHost = nil
        self.linkHostname = nil
        self.dkimSelector = nil
        self.hasDkimKey = nil
        self.logo = nil
    }

    public var dnsSummary: DomainDNSSummary { DomainDNSSummary.of(dnsCheck) }
}

/// DomainOverview is the domain at a glance: its mail counted by status,
/// when the newest arrived, and what waits in the queue for it.
public struct DomainOverview: Hashable, Sendable {
    public let mailCountByStatus: [String: Int]
    public let newestReceivedAt: String?
    public let pendingDeliveries: [MailDelivery]

    public init(mailCountByStatus: [String: Int], newestReceivedAt: String?, pendingDeliveries: [MailDelivery]) {
        self.mailCountByStatus = mailCountByStatus
        self.newestReceivedAt = newestReceivedAt
        self.pendingDeliveries = pendingDeliveries
    }

    public var totalMailCount: Int { mailCountByStatus.values.reduce(0, +) }
    public var acceptedMailCount: Int { mailCountByStatus["accepted"] ?? 0 }
    public var rejectedMailCount: Int { mailCountByStatus["rejected"] ?? 0 }
    /// Deliveries that have failed at least once.
    public var failingDeliveryCount: Int { pendingDeliveries.filter(\.isFailing).count }
}

/// ServerMailbox is a mailbox on the server, with its owner, for choosing
/// where an alias delivers.
public struct ServerMailbox: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public let userId: String?
    public let username: String
    /// The owner's own name, when they gave one.
    public let ownerName: String?

    enum CodingKeys: String, CodingKey {
        case id, name, userId, username
        case ownerName = "userName"
    }

    public init(id: String, name: String, username: String, ownerName: String? = nil) {
        self.id = id
        self.name = name
        self.userId = nil
        self.username = username
        self.ownerName = ownerName
    }

    /// "username (Their Name) · Mailbox", as the dashboard's picker says it.
    public var pickerText: String {
        var text = username
        if let ownerName, !ownerName.isEmpty, ownerName != username { text += " (\(ownerName))" }
        return "\(text) · \(name)"
    }

    /// "username · Mailbox", as an alias's row says where it delivers.
    public var rowText: String { "\(username) · \(name)" }
}

/// DomainTemplate is a message a domain sends by name, in one language and
/// its translations.
public struct DomainTemplate: Codable, Hashable, Sendable, Identifiable {
    /// Translation is the template's subject and content in another
    /// language.
    public struct Translation: Codable, Hashable, Sendable {
        public let locale: String
        public let subject: String?
        public let htmlContent: String?
        public let textContent: String?
    }

    public let id: String
    public let domainId: String?
    public let layoutId: String?
    public let name: String
    public let comment: String?
    public let locale: String?
    public let subject: String?
    public let htmlContent: String?
    public let textContent: String?
    public let variables: [String]?
    public let modifiedAt: String?
    public let translations: [Translation]?

    public init(id: String, name: String, layoutId: String? = nil, locale: String? = nil, translationLocales: [String] = []) {
        self.id = id
        self.domainId = nil
        self.layoutId = layoutId
        self.name = name
        self.comment = nil
        self.locale = locale
        self.subject = nil
        self.htmlContent = nil
        self.textContent = nil
        self.variables = nil
        self.modifiedAt = nil
        self.translations = translationLocales.map { Translation(locale: $0, subject: nil, htmlContent: nil, textContent: nil) }
    }

    /// The languages it is written in: the default first, then each
    /// translation.
    public var locales: [String] { ([locale ?? ""] + (translations ?? []).map(\.locale)).filter { !$0.isEmpty } }
}

/// DomainLayout is what goes around a domain's templates: the header, the
/// footer and the styles.
public struct DomainLayout: Codable, Hashable, Sendable, Identifiable {
    /// Translation is the layout's content in another language.
    public struct Translation: Codable, Hashable, Sendable {
        public let locale: String
        public let htmlContent: String?
        public let textContent: String?
    }

    public let id: String
    public let domainId: String?
    /// What it is for; a layout has no name of its own.
    public let comment: String?
    public let locale: String?
    public let htmlContent: String?
    public let textContent: String?
    public let modifiedAt: String?
    public let translations: [Translation]?

    public init(id: String, comment: String? = nil) {
        self.id = id
        self.domainId = nil
        self.comment = comment
        self.locale = nil
        self.htmlContent = nil
        self.textContent = nil
        self.modifiedAt = nil
        self.translations = nil
    }

    public var locales: [String] { ([locale ?? ""] + (translations ?? []).map(\.locale)).filter { !$0.isEmpty } }

    /// How many of these templates sit in each layout, by its id.
    public static func templateCountByLayoutId(_ templates: [DomainTemplate]) -> [String: Int] {
        var templateCountByLayoutId: [String: Int] = [:]
        for template in templates {
            if let layoutId = template.layoutId, !layoutId.isEmpty { templateCountByLayoutId[layoutId, default: 0] += 1 }
        }
        return templateCountByLayoutId
    }
}

// MARK: - Documents

extension Documents {
    public static let manageOperatorAccess = """
        query { GetSession { manages permissions { everywhere byDomain { domainId permissions } } } }
        """
    public static let manageDomainNames = """
        { ListDomains { id domain } }
        """
    public static let manageAllMail = """
        query ($domainId: String!, $pagination: PaginationInput) {
          ListMails(domainId: $domainId, pagination: $pagination) {
            id domainId sender from subject status kind size receivedAt
          }
        }
        """
    public static let manageMailOpens = """
        query ($mailIds: [String!]!) {
          ListMailOpens(mailIds: $mailIds) { mailId trackable opened openedAt openCount }
        }
        """
    public static let manageMail = """
        query ($mailId: String!) {
          GetSpamTraining(mailId: $mailId) { label }
          GetSettings { antispam { effectiveEngine bayesEnabled bayesMinimumMessages bayesLearnedSpam bayesLearnedHam } }
          GetMail(mailId: $mailId) {
            id sender from subject recipients status kind size receivedAt
            ip rdns hello messageId envelopeId tlsVersion tlsCipherSuite
            location { city country }
            authenticationResults {
              senderMx { domain mailServers }
              fromMx { domain mailServers }
              spf { domain ip result }
              dmarc { domain policy subdomainPolicy dkimAlignment spfAlignment result }
              dkims { domain selector identifier result }
              arc { result instances }
              spamFilter { score result symbols checks { symbol score description } }
              antivirus { viruses }
              contentFilter { unsafeExtensions }
              errors
            }
          }
          GetMailContent(mailId: $mailId) {
            mailId available text html hasRemoteContent imagesAllowed size rawHeaders
            headers { key value }
            attachments { index filename contentType size inline }
          }
          GetMailOpens(mailId: $mailId) {
            trackable opened openedAt lastOpenedAt openCount ip
          }
          ListDeliveriesByMail(mailId: $mailId) {
            id recipient kind status size attempts error method destination
            attemptedAt deliveredAt droppedAt notifiedAt retryAt
            deliveryStatuses {
              reportingMta
              recipientStatuses { finalRecipient action status remoteMta diagnosticCode lastAttemptDate }
            }
          }
        }
        """
    public static let manageMarkMail = """
        mutation ($mailId: String!, $label: String!) {
          MarkMail(mailId: $mailId, label: $label) { mailId label learnedSpam learnedHam }
        }
        """
    public static let manageForgetMail = """
        mutation ($mailId: String!) {
          ForgetMail(mailId: $mailId) { mailId label learnedSpam learnedHam }
        }
        """
    public static let managePendingDeliveries = """
        query {
          ListPendingDeliveries(domainId: "", pagination: { first: 500 }) {
            id mailId domainId recipient kind status attempts error retryAt
          }
        }
        """
    public static let manageRetryDelivery = """
        mutation ($deliveryId: String!) {
          RetryDelivery(deliveryId: $deliveryId) { id status attempts }
        }
        """
    public static let manageReports = """
        query ($pagination: PaginationInput) {
          ListReports(domainId: "", pagination: $pagination) {
            id domainId beginAt endAt count ip rdns
            fromDomain senderDomain disposition dkimAligned spfAligned
          }
        }
        """
    public static let manageReport = """
        query ($reportId: String!) {
          GetReport(reportId: $reportId) {
            id domainId beginAt endAt count ip rdns
            fromDomain senderDomain disposition dkimAligned spfAligned
            location { city country }
            feedback {
              organizationName email extraContactInfo reportId begin end errors
              domain dkimAlignment spfAlignment policy subdomainPolicy percent failureOptions
              records {
                sourceIp count disposition dkim spf reasonType reasonComment
                headerFrom envelopeFrom envelopeTo
                dkims { domain selector result humanResult }
                spfs { domain scope result }
              }
            }
          }
        }
        """
    public static let manageDomains = """
        {
          ListDomains {
            id domain subdomain comment
            aliases { id }
            credentials { id }
            records { records { type verified optional } }
          }
        }
        """
    public static let manageCreateDomain = """
        mutation ($domain: String!) {
          CreateDomain(domainParameters: { domain: $domain }) { id domain }
        }
        """
    public static let manageDomain = """
        query ($domainId: String!) {
          GetDomain(domainId: $domainId) {
            id domain subdomain comment spamFilterScoreThreshold mailServers mailHosts linkHost linkHostname dkimSelector hasDkimKey
            logo { filename title url publicUrl uploadedAt }
            aliases { id pattern comment kind email webhook mailboxId disabled mailServer { host port username } }
            credentials { id comment alias disabled }
            records { checkedAt error records { type name expected priority optional found verified purpose blocked } }
          }
        }
        """
    public static let manageDomainOverview = """
        query ($domainId: String!) {
          CountMailsBy(domainId: $domainId, field: "status") {
            value
            count
          }
          ListMails(
            domainId: $domainId
            aggregations: [{ sort: [{ field: "receivedAt", direction: "descending" }] }]
            pagination: { first: 1 }
          ) {
            id
            receivedAt
          }
          ListPendingDeliveries(domainId: $domainId, pagination: { first: 500 }) {
            id
            status
            retryAt
          }
        }
        """
    public static let manageAllMailboxes = """
        { ListAllMailboxes { id name userId username userName } }
        """
    public static let manageCreateAlias = """
        mutation ($domainId: String!, $pattern: String!, $comment: String, $kind: String!, $email: String, $webhook: String, $mailboxId: String) {
          CreateAlias(domainId: $domainId, aliasParameters: { pattern: $pattern, comment: $comment, kind: $kind, email: $email, webhook: $webhook, mailboxId: $mailboxId }) {
            id
          }
        }
        """
    public static let manageUpdateAlias = """
        mutation ($aliasId: String!, $pattern: String!, $comment: String, $kind: String!, $email: String, $webhook: String, $mailboxId: String, $disabled: Boolean) {
          UpdateAlias(aliasId: $aliasId, aliasParameters: { pattern: $pattern, comment: $comment, kind: $kind, email: $email, webhook: $webhook, mailboxId: $mailboxId, disabled: $disabled }) {
            id
          }
        }
        """
    public static let manageDeleteAlias = """
        mutation ($aliasId: String!) { DeleteAlias(aliasId: $aliasId) }
        """
    public static let manageCreateCredential = """
        mutation ($domainId: String!, $comment: String) {
          CreateCredential(domainId: $domainId, credentialParameters: { comment: $comment }) {
            username password host port
          }
        }
        """
    public static let manageUpdateCredential = """
        mutation ($credentialId: String!, $comment: String, $alias: String, $disabled: Boolean) {
          UpdateCredential(credentialId: $credentialId, credentialParameters: { comment: $comment, alias: $alias, disabled: $disabled }) {
            id
          }
        }
        """
    public static let manageDeleteCredential = """
        mutation ($credentialId: String!) { DeleteCredential(credentialId: $credentialId) }
        """
    public static let manageCheckDomain = """
        mutation ($domainId: String!) { CheckDomain(domainId: $domainId) { id } }
        """
    public static let manageDeleteDomainLogo = """
        mutation ($domainId: String!) { DeleteDomainLogo(domainId: $domainId) }
        """
    public static let manageUpdateMailServers = """
        mutation ($domainId: String!, $mailServers: [String!]) {
          UpdateDomain(domainId: $domainId, domainParameters: { mailServers: $mailServers }) {
            id mailServers mailHosts
          }
        }
        """
    public static let manageUpdateLinkHost = """
        mutation ($domainId: String!, $linkHost: String) {
          UpdateDomain(domainId: $domainId, domainParameters: { linkHost: $linkHost }) {
            id linkHost linkHostname
          }
        }
        """
    public static let manageUpdateSelector = """
        mutation ($domainId: String!, $selector: String!) {
          UpdateDomain(domainId: $domainId, domainParameters: { dkimSelector: $selector }) {
            id dkimSelector
          }
        }
        """
    public static let manageRegenerateKey = """
        mutation ($domainId: String!) {
          RegenerateDomainKey(domainId: $domainId) { id dkimSelector hasDkimKey }
        }
        """
    public static let manageTemplates = """
        query ($domainId: String!) {
          ListTemplates(domainId: $domainId) {
            id name comment locale layoutId modifiedAt variables
            translations { locale }
          }
          ListLayouts(domainId: $domainId) {
            id comment locale modifiedAt
            translations { locale }
          }
        }
        """
    public static let manageTemplate = """
        query ($templateId: String!) {
          GetTemplate(templateId: $templateId) {
            id domainId layoutId name comment locale subject htmlContent textContent variables modifiedAt
            translations { locale subject htmlContent textContent }
          }
        }
        """
    public static let manageLayout = """
        query ($layoutId: String!) {
          GetLayout(layoutId: $layoutId) {
            id domainId comment locale htmlContent textContent modifiedAt
            translations { locale htmlContent textContent }
          }
        }
        """
    public static let manageDeleteTemplate = """
        mutation ($templateId: String!) { DeleteTemplate(templateId: $templateId) }
        """
    public static let manageDeleteLayout = """
        mutation ($layoutId: String!) { DeleteLayout(layoutId: $layoutId) }
        """
}

// MARK: - Connection

extension Connection {
    /// What the signed-in person may open on the management side: the
    /// session's `manages`, which the server sets when any permission that
    /// opens it is held, and the permissions behind it, which say which
    /// pages.
    public func operatorAccess() async throws -> OperatorAccess {
        struct Answer: Decodable, Sendable {
            struct Session: Decodable, Sendable {
                struct Permissions: Decodable, Sendable {
                    let everywhere: [String]?
                    let byDomain: [OperatorAccess.DomainPermissions]?
                }
                let manages: Bool?
                let permissions: Permissions?
            }
            let GetSession: Session
        }
        let session = try await execute(Documents.manageOperatorAccess, as: Answer.self).GetSession
        return OperatorAccess(
            isOperator: session.manages ?? false, everywherePermissions: session.permissions?.everywhere ?? [],
            domainPermissions: session.permissions?.byDomain ?? [])
    }

    /// Whether the signed-in person is an operator: whether they hold any
    /// permission that opens the management side.
    public func isOperator() async throws -> Bool {
        try await operatorAccess().isOperator
    }

    /// Every domain's id and name, for naming and filtering by domain.
    public func managedDomainNames() async throws -> [ManagedDomain] {
        struct Answer: Decodable, Sendable { let ListDomains: [ManagedDomain?] }
        return try await execute(Documents.manageDomainNames, as: Answer.self).ListDomains.compactMap { $0 }
    }

    /// The newest mail on the server, for every domain.
    public func allMail(first: Int = ManagedWords.listLimit) async throws -> [AuditedMail] {
        struct Answer: Decodable, Sendable { let ListMails: [AuditedMail?] }
        return try await execute(
            Documents.manageAllMail,
            variables: ["domainId": .string(""), "pagination": .object(["first": .number(Double(first))])], as: Answer.self
        ).ListMails.compactMap { $0 }
    }

    /// Whether the pictures in these messages have been fetched, by message.
    public func mailOpens(mailIds: [String]) async throws -> [String: AuditedMailOpens] {
        guard !mailIds.isEmpty else { return [:] }
        struct Answer: Decodable, Sendable { let ListMailOpens: [AuditedMailOpens?] }
        let opens = try await execute(
            Documents.manageMailOpens, variables: ["mailIds": .array(mailIds.map { .string($0) })], as: Answer.self
        ).ListMailOpens.compactMap { $0 }
        return Dictionary(opens.map { ($0.mailId ?? "", $0) }, uniquingKeysWith: { first, _ in first })
    }

    /// One message, as the operator's page for it shows it.
    public func auditedMail(mailId: String) async throws -> AuditedMailDetail {
        struct Answer: Decodable, Sendable {
            struct Training: Decodable, Sendable { let label: String? }
            struct Settings: Decodable, Sendable { let antispam: AntispamClassifier? }
            let GetSpamTraining: Training?
            let GetSettings: Settings?
            let GetMail: AuditedMail
            let GetMailContent: MailContent?
            let GetMailOpens: AuditedMailOpens?
            let ListDeliveriesByMail: [MailDelivery?]?
        }
        let answer = try await execute(Documents.manageMail, variables: ["mailId": .string(mailId)], as: Answer.self)
        let trainingLabel = answer.GetSpamTraining?.label.flatMap { $0.isEmpty ? nil : $0 }
        return AuditedMailDetail(
            mail: answer.GetMail, content: answer.GetMailContent, opens: answer.GetMailOpens,
            deliveries: (answer.ListDeliveriesByMail ?? []).compactMap { $0 }, trainingLabel: trainingLabel,
            classifier: answer.GetSettings?.antispam)
    }

    /// Teaches the filter that a message is spam or ham, or, with nil,
    /// forgets what it was taught about it.
    public func teachSpamFilter(mailId: String, trainingLabel: String?) async throws -> MailSpamTraining {
        if let trainingLabel {
            struct Answer: Decodable, Sendable { let MarkMail: MailSpamTraining }
            return try await execute(
                Documents.manageMarkMail, variables: ["mailId": .string(mailId), "label": .string(trainingLabel)], as: Answer.self
            ).MarkMail
        }
        struct Answer: Decodable, Sendable { let ForgetMail: MailSpamTraining }
        return try await execute(Documents.manageForgetMail, variables: ["mailId": .string(mailId)], as: Answer.self).ForgetMail
    }

    /// Every delivery still waiting, for every domain: the delivery queue.
    public func pendingDeliveries() async throws -> [MailDelivery] {
        struct Answer: Decodable, Sendable { let ListPendingDeliveries: [MailDelivery?] }
        return try await execute(Documents.managePendingDeliveries, as: Answer.self).ListPendingDeliveries.compactMap { $0 }
    }

    /// Tries a waiting delivery again now.
    public func retryDelivery(deliveryId: String) async throws {
        struct Answer: Decodable, Sendable { let RetryDelivery: JSONValue? }
        _ = try await execute(Documents.manageRetryDelivery, variables: ["deliveryId": .string(deliveryId)], as: Answer.self)
    }

    /// The newest DMARC aggregate report rows, for every domain.
    public func dmarcReports(first: Int = ManagedWords.listLimit) async throws -> [DMARCReport] {
        struct Answer: Decodable, Sendable { let ListReports: [DMARCReport?] }
        return try await execute(
            Documents.manageReports, variables: ["pagination": .object(["first": .number(Double(first))])], as: Answer.self
        ).ListReports.compactMap { $0 }
    }

    /// One DMARC aggregate report, with the report as it arrived; nil when
    /// there is none by that id.
    public func dmarcReport(reportId: String) async throws -> DMARCReport? {
        struct Answer: Decodable, Sendable { let GetReport: DMARCReport? }
        return try await execute(Documents.manageReport, variables: ["reportId": .string(reportId)], as: Answer.self).GetReport
    }

    /// Every domain, with how many aliases and credentials it has and its
    /// DNS in one word.
    public func managedDomains() async throws -> [ManagedDomain] {
        struct Answer: Decodable, Sendable { let ListDomains: [ManagedDomain?] }
        return try await execute(Documents.manageDomains, as: Answer.self).ListDomains.compactMap { $0 }
    }

    /// Adds a domain, and returns its id.
    @discardableResult
    public func createManagedDomain(domainName: String) async throws -> String {
        struct Answer: Decodable, Sendable {
            struct Created: Decodable, Sendable { let id: String }
            let CreateDomain: Created
        }
        return try await execute(Documents.manageCreateDomain, variables: ["domain": .string(domainName)], as: Answer.self).CreateDomain.id
    }

    /// One domain, with everything its pages show.
    public func managedDomain(domainId: String) async throws -> ManagedDomain? {
        struct Answer: Decodable, Sendable { let GetDomain: ManagedDomain? }
        return try await execute(Documents.manageDomain, variables: ["domainId": .string(domainId)], as: Answer.self).GetDomain
    }

    /// The domain at a glance: its mail by status, the newest arrival and
    /// its queue.
    public func domainOverview(domainId: String) async throws -> DomainOverview {
        struct Answer: Decodable, Sendable {
            struct Facet: Decodable, Sendable {
                let facetValue: String
                let mailCount: Int

                enum CodingKeys: String, CodingKey {
                    case facetValue = "value"
                    case mailCount = "count"
                }
            }
            struct Newest: Decodable, Sendable { let receivedAt: String? }
            let CountMailsBy: [Facet?]?
            let ListMails: [Newest?]?
            let ListPendingDeliveries: [MailDelivery?]?
        }
        let answer = try await execute(Documents.manageDomainOverview, variables: ["domainId": .string(domainId)], as: Answer.self)
        var mailCountByStatus: [String: Int] = [:]
        for facet in (answer.CountMailsBy ?? []).compactMap({ $0 }) {
            mailCountByStatus[facet.facetValue, default: 0] += facet.mailCount
        }
        return DomainOverview(
            mailCountByStatus: mailCountByStatus, newestReceivedAt: answer.ListMails?.compactMap({ $0 }).first?.receivedAt,
            pendingDeliveries: (answer.ListPendingDeliveries ?? []).compactMap { $0 })
    }

    /// Every mailbox on the server, with its owner, for an alias to deliver
    /// into.
    public func serverMailboxes() async throws -> [ServerMailbox] {
        struct Answer: Decodable, Sendable { let ListAllMailboxes: [ServerMailbox?] }
        return try await execute(Documents.manageAllMailboxes, as: Answer.self).ListAllMailboxes.compactMap { $0 }
    }

    /// Adds an address to a domain. The destination is the address to
    /// forward to for an email alias and the address to post to for a
    /// webhook; a mailbox alias delivers into the mailbox given.
    public func createDomainAlias(
        domainId: String, pattern: String, comment: String = "", aliasKind: String, destination: String, mailboxId: String
    ) async throws {
        struct Answer: Decodable, Sendable { let CreateAlias: JSONValue? }
        _ = try await execute(
            Documents.manageCreateAlias,
            variables: [
                "domainId": .string(domainId), "pattern": .string(pattern), "comment": .string(comment), "kind": .string(aliasKind),
                "email": aliasKind == "email" ? .string(destination) : .null,
                "webhook": aliasKind == "webhook" ? .string(destination) : .null,
                "mailboxId": aliasKind == "mailbox" ? .string(mailboxId) : .null,
            ], as: Answer.self)
    }

    /// Changes an alias: its pattern, its note, where its mail goes, and
    /// whether it is ignored. Only the destination its kind needs is sent
    /// as a value; the others are null, which the server leaves as they
    /// were. A relay keeps its host, port and password, which are not
    /// sent. The server leaves the pattern alone when it is empty, so a
    /// catch-all stays a catch-all and an address cannot become one.
    public func updateDomainAlias(
        aliasId: String, pattern: String, comment: String, aliasKind: String, destination: String, mailboxId: String, isDisabled: Bool
    ) async throws {
        struct Answer: Decodable, Sendable { let UpdateAlias: JSONValue? }
        _ = try await execute(
            Documents.manageUpdateAlias,
            variables: [
                "aliasId": .string(aliasId), "pattern": .string(pattern), "comment": .string(comment), "kind": .string(aliasKind),
                "email": aliasKind == "email" ? .string(destination) : .null,
                "webhook": aliasKind == "webhook" ? .string(destination) : .null,
                "mailboxId": aliasKind == "mailbox" ? .string(mailboxId) : .null,
                "disabled": .bool(isDisabled),
            ], as: Answer.self)
    }

    public func deleteDomainAlias(aliasId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteAlias: JSONValue? }
        _ = try await execute(Documents.manageDeleteAlias, variables: ["aliasId": .string(aliasId)], as: Answer.self)
    }

    /// Makes a credential for sending through the domain; its password is
    /// shown this once.
    public func createDomainCredential(domainId: String, comment: String) async throws -> CreatedDomainCredential {
        struct Answer: Decodable, Sendable { let CreateCredential: CreatedDomainCredential }
        return try await execute(
            Documents.manageCreateCredential, variables: ["domainId": .string(domainId), "comment": .string(comment)], as: Answer.self
        ).CreateCredential
    }

    /// Changes a credential: its note, the one local part it may send as
    /// (empty for any), and whether it is refused. Its password is never
    /// changed or shown, which the server does not offer.
    public func updateDomainCredential(credentialId: String, comment: String, restrictedLocalPart: String, isDisabled: Bool) async throws {
        struct Answer: Decodable, Sendable { let UpdateCredential: JSONValue? }
        _ = try await execute(
            Documents.manageUpdateCredential,
            variables: [
                "credentialId": .string(credentialId), "comment": .string(comment), "alias": .string(restrictedLocalPart),
                "disabled": .bool(isDisabled),
            ], as: Answer.self)
    }

    public func deleteDomainCredential(credentialId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteCredential: JSONValue? }
        _ = try await execute(Documents.manageDeleteCredential, variables: ["credentialId": .string(credentialId)], as: Answer.self)
    }

    /// Checks the domain's DNS again now.
    public func checkDomain(domainId: String) async throws {
        struct Answer: Decodable, Sendable { let CheckDomain: JSONValue? }
        _ = try await execute(Documents.manageCheckDomain, variables: ["domainId": .string(domainId)], as: Answer.self)
    }

    /// Sets the names the domain's MX records point at, then checks its DNS
    /// again, since what to publish has changed.
    public func updateDomainMailServers(domainId: String, mailServers: [String]) async throws {
        struct Answer: Decodable, Sendable { let UpdateDomain: JSONValue? }
        _ = try await execute(
            Documents.manageUpdateMailServers,
            variables: ["domainId": .string(domainId), "mailServers": .array(mailServers.map { .string($0) })], as: Answer.self)
        try await checkDomain(domainId: domainId)
    }

    /// Sets the host pictures in the domain's mail are fetched from; empty
    /// for the default.
    public func updateDomainLinkHost(domainId: String, linkHost: String) async throws {
        struct Answer: Decodable, Sendable { let UpdateDomain: JSONValue? }
        _ = try await execute(
            Documents.manageUpdateLinkHost, variables: ["domainId": .string(domainId), "linkHost": .string(linkHost)], as: Answer.self)
    }

    /// Moves the domain's signing key to another selector, then checks its
    /// DNS again, since the record to publish has moved.
    public func updateDomainDkimSelector(domainId: String, selector: String) async throws {
        struct Answer: Decodable, Sendable { let UpdateDomain: JSONValue? }
        _ = try await execute(
            Documents.manageUpdateSelector, variables: ["domainId": .string(domainId), "selector": .string(selector)], as: Answer.self)
        try await checkDomain(domainId: domainId)
    }

    /// Makes the domain a new signing key, replacing the one it has.
    public func regenerateDomainKey(domainId: String) async throws {
        struct Answer: Decodable, Sendable { let RegenerateDomainKey: JSONValue? }
        _ = try await execute(Documents.manageRegenerateKey, variables: ["domainId": .string(domainId)], as: Answer.self)
    }

    /// Stops publishing the domain's logo, then checks its DNS again, since
    /// the BIMI record named it.
    public func deleteDomainLogo(domainId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteDomainLogo: JSONValue? }
        _ = try await execute(Documents.manageDeleteDomainLogo, variables: ["domainId": .string(domainId)], as: Answer.self)
        try await checkDomain(domainId: domainId)
    }

    /// Publishes an SVG as the domain's logo, then checks its DNS again so
    /// the record to copy names it. The server checks the file first and
    /// says what is wrong with one it refuses.
    public func uploadDomainLogo(domainId: String, fileName: String, svgData: Data) async throws {
        let boundary = "teanode-\(UUID().uuidString)"
        let encodedId = domainId.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? domainId
        var request = URLRequest(url: account.server.endpoint("/api/v1/domains/\(encodedId)/logo"))
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        request.httpBody = Upload.body(boundary: boundary, name: fileName, contentType: "image/svg+xml", data: svgData)
        let (answer, response) = try await send(authorizing: request)
        guard response.statusCode == 200 else {
            struct Refusal: Decodable { let error: String? }
            let refusal = try? JSONDecoder().decode(Refusal.self, from: answer)
            throw TeaNodeError.refused(refusal?.error ?? String(decoding: answer.prefix(300), as: UTF8.self))
        }
        try await checkDomain(domainId: domainId)
    }

    /// The domain's templates and layouts, as their list shows them.
    public func domainTemplates(domainId: String) async throws -> (templates: [DomainTemplate], layouts: [DomainLayout]) {
        struct Answer: Decodable, Sendable {
            let ListTemplates: [DomainTemplate?]?
            let ListLayouts: [DomainLayout?]?
        }
        let answer = try await execute(Documents.manageTemplates, variables: ["domainId": .string(domainId)], as: Answer.self)
        return ((answer.ListTemplates ?? []).compactMap { $0 }, (answer.ListLayouts ?? []).compactMap { $0 })
    }

    /// One template whole, in every language.
    public func domainTemplate(templateId: String) async throws -> DomainTemplate? {
        struct Answer: Decodable, Sendable { let GetTemplate: DomainTemplate? }
        return try await execute(Documents.manageTemplate, variables: ["templateId": .string(templateId)], as: Answer.self).GetTemplate
    }

    /// One layout whole, in every language.
    public func domainLayout(layoutId: String) async throws -> DomainLayout? {
        struct Answer: Decodable, Sendable { let GetLayout: DomainLayout? }
        return try await execute(Documents.manageLayout, variables: ["layoutId": .string(layoutId)], as: Answer.self).GetLayout
    }

    public func deleteDomainTemplate(templateId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteTemplate: JSONValue? }
        _ = try await execute(Documents.manageDeleteTemplate, variables: ["templateId": .string(templateId)], as: Answer.self)
    }

    public func deleteDomainLayout(layoutId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteLayout: JSONValue? }
        _ = try await execute(Documents.manageDeleteLayout, variables: ["layoutId": .string(layoutId)], as: Answer.self)
    }
}
