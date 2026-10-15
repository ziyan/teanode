import Foundation

/// KnowledgeRecordLink joins a page of the agent's memory to one of the
/// person's own records: a contact card, a mail, an event or reminder, a
/// message of a conversation, a finance transaction or receipt, a passage of
/// a recording. Nothing of the record is kept in the link: its title, date
/// and where it opens are found when the link is read.
public struct KnowledgeRecordLink: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    /// The page the link is on, filled for the pages that link to a record.
    public let nodePath: String
    public let nodeName: String
    public let nodeKind: String
    public let recordKind: String
    public let recordId: String
    public let recordRelation: String
    /// "stated", or "proposed" where the agent only guessed.
    public let linkStatus: String
    public let passageStartMS: Int64
    public let passageEndMS: Int64
    public let transcriptRunId: String
    /// How many passage links of one recording this row stands for, the
    /// range above being the first of them; zero for any other kind, and
    /// missing from a server that lists every passage on its own.
    public let passageCount: Int?
    public let note: String
    public let happenedAt: String?
    public let createdAt: String
    public let recordTitle: String
    public let recordHappenedAt: String?
    /// Where the dashboard opens the record, such as /mailbox/starred/ID.
    public let dashboardPath: String
    /// The record is gone: the link can only be removed.
    public let isUnavailable: Bool

    /// How many passages of the recording the row covers, else zero.
    public var coveredPassageCount: Int { passageCount ?? 0 }

    /// The agent only guessed this link; the person confirms or removes it.
    public var isProposed: Bool { linkStatus == "proposed" }

    /// What a tap can open: the record is still there and has a place.
    public var canOpen: Bool { !isUnavailable && RecordTarget(dashboardPath: dashboardPath) != nil }

    /// The date of the record, else of the link.
    public var date: Date? { ServerTime.date(recordHappenedAt) ?? ServerTime.date(happenedAt) }

    /// The record as the command line names it: kind:id, with the range of
    /// a passage of a recording.
    public var reference: String {
        recordKind == "recording_passage" ? "\(recordKind):\(recordId)@\(passageStartMS)-\(passageEndMS)" : "\(recordKind):\(recordId)"
    }
}

/// KnowledgeEvidenceRecord is the record one piece of a fact's evidence
/// names, found again so that the source of a fact opens it.
public struct KnowledgeEvidenceRecord: Codable, Hashable, Sendable {
    public let factId: String
    public let evidenceIndex: Int
    public let recordKind: String
    public let recordId: String
    public let passageStartMS: Int64
    public let passageEndMS: Int64
    public let transcriptRunId: String
    public let recordTitle: String
    public let dashboardPath: String
    public let isUnavailable: Bool

    public var canOpen: Bool { !isUnavailable && RecordTarget(dashboardPath: dashboardPath) != nil }
}

/// KnowledgeRecordGroup is the links of one kind of record.
public struct KnowledgeRecordGroup: Hashable, Sendable, Identifiable {
    public let recordKind: String
    public let links: [KnowledgeRecordLink]
    public var id: String { recordKind }
}

/// KnowledgeRecordKinds are the kinds of record, in the order a page lists
/// them: the most personal first.
public enum KnowledgeRecordKinds {
    public static let all = [
        "contact", "mail", "calendar_event", "reminder", "conversation_message", "finance_transaction", "finance_receipt",
        "recording_passage",
    ]

    /// The SF Symbol that marks a kind.
    public static func symbol(_ recordKind: String) -> String {
        switch recordKind {
        case "contact": "person.crop.circle"
        case "mail": "envelope"
        case "calendar_event": "calendar"
        case "reminder": "checklist"
        case "conversation_message": "bubble.left"
        case "finance_transaction": "creditcard"
        case "finance_receipt": "receipt"
        case "recording_passage": "waveform"
        default: "doc"
        }
    }

    /// Groups links by kind in the order above, a kind this build does not
    /// know after them so a newer server's kind is listed and not dropped.
    public static func grouped(_ links: [KnowledgeRecordLink]) -> [KnowledgeRecordGroup] {
        var byKind: [String: [KnowledgeRecordLink]] = [:]
        for link in links { byKind[link.recordKind, default: []].append(link) }
        let unknown = byKind.keys.filter { !all.contains($0) }.sorted()
        return (all.filter { byKind[$0] != nil } + unknown).map { KnowledgeRecordGroup(recordKind: $0, links: byKind[$0] ?? []) }
    }

    /// The pages that link to a record, each once: a statement is kept over
    /// a guess where a page links both ways.
    public static func pages(of links: [KnowledgeRecordLink]) -> [KnowledgeRecordLink] {
        var kept: [String: KnowledgeRecordLink] = [:]
        var order: [String] = []
        for link in links {
            if let earlier = kept[link.nodePath] {
                if earlier.isProposed && !link.isProposed { kept[link.nodePath] = link }
            } else {
                kept[link.nodePath] = link
                order.append(link.nodePath)
            }
        }
        return order.compactMap { kept[$0] }
    }
}

/// RecordTarget is where the app opens a record, read from the path the
/// dashboard opens it at, which is what the server returns for every link.
public enum RecordTarget: Hashable, Sendable {
    case mail(itemId: String)
    case contact(id: String)
    case calendarEvent(id: String)
    case reminder(id: String)
    case conversation(id: String)
    case financeTransaction(id: String)
    case financeReceipt(id: String)
    /// A recording, with its passage in the query as the dashboard writes it.
    case recording(path: String)

    public init?(dashboardPath: String) {
        guard dashboardPath.hasPrefix("/"), dashboardPath.count <= 600,
            let components = URLComponents(string: dashboardPath)
        else { return nil }
        let segments = components.path.split(separator: "/").map(String.init)
        func value(_ name: String) -> String? {
            guard let found = components.queryItems?.first(where: { $0.name == name })?.value, !found.isEmpty else { return nil }
            return found
        }
        switch segments.first ?? "" {
        case "mailbox":
            guard segments.count == 3, !segments[2].isEmpty else { return nil }
            self = .mail(itemId: segments[2])
        case "contacts":
            guard let id = value("contact") else { return nil }
            self = .contact(id: id)
        case "calendar":
            guard let id = value("event") else { return nil }
            self = .calendarEvent(id: id)
        case "reminders":
            guard let id = value("reminder") else { return nil }
            self = .reminder(id: id)
        case "agent":
            guard let id = value("conversation") else { return nil }
            self = .conversation(id: id)
        case "finance":
            if segments.count == 2, segments[1] == "transactions", let id = value("transaction") {
                self = .financeTransaction(id: id)
            } else if segments.count == 2, segments[1] == "receipts", let id = value("receipt") {
                self = .financeReceipt(id: id)
            } else {
                return nil
            }
        case "recordings":
            guard segments.count == 2 else { return nil }
            self = .recording(path: dashboardPath)
        default:
            return nil
        }
    }
}

extension Documents {
    /// What a query selects of a link.
    static let recordLinkFields = """
        id nodePath nodeName nodeKind recordKind recordId recordRelation linkStatus passageStartMS passageEndMS transcriptRunId passageCount note happenedAt createdAt recordTitle recordHappenedAt dashboardPath isUnavailable
        """
    public static let readRecordMemoryLinks = """
        query ($recordKind: String!, $recordId: String!, $passageStartMS: Int64, $passageEndMS: Int64) {
          ReadRecordMemoryLinks(recordKind: $recordKind, recordId: $recordId, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS) { \(recordLinkFields) }
        }
        """
    public static let listKnowledgeRecordLinks = """
        query ($path: String!, $first: Int, $offset: Int) {
          ListAgentRecordLinks(path: $path, first: $first, offset: $offset) {
            recordLinks { \(recordLinkFields) }
            recordLinkCount nextOffset
          }
        }
        """
    public static let linkKnowledgeRecord = """
        mutation ($path: String!, $recordKind: String!, $recordId: String!, $recordRelation: String!, $passageStartMS: Int64, $passageEndMS: Int64, $transcriptRunId: String) {
          LinkAgentRecord(path: $path, recordKind: $recordKind, recordId: $recordId, recordRelation: $recordRelation, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS, transcriptRunId: $transcriptRunId) { id }
        }
        """
    public static let unlinkKnowledgeRecord = """
        mutation ($path: String!, $recordKind: String!, $recordId: String!, $recordRelation: String, $passageStartMS: Int64, $passageEndMS: Int64) {
          UnlinkAgentRecord(path: $path, recordKind: $recordKind, recordId: $recordId, recordRelation: $recordRelation, passageStartMS: $passageStartMS, passageEndMS: $passageEndMS)
        }
        """
    public static let confirmKnowledgeRecord = """
        mutation ($path: String!, $recordKind: String!, $recordId: String!, $recordRelation: String) {
          ConfirmAgentRecord(path: $path, recordKind: $recordKind, recordId: $recordId, recordRelation: $recordRelation)
        }
        """
    public static let setKnowledgeContact = """
        mutation ($path: String!, $contactId: String) { SetAgentNodeContact(path: $path, contactId: $contactId) }
        """
}

extension Connection {
    /// The pages of memory that link to one record. For a passage of a
    /// recording the range narrows it to the passages that overlap it.
    public func recordMemoryLinks(recordKind: String, recordId: String, passageStartMS: Int64? = nil, passageEndMS: Int64? = nil) async throws
        -> [KnowledgeRecordLink]
    {
        struct Answer: Decodable, Sendable { let ReadRecordMemoryLinks: [KnowledgeRecordLink?] }
        var variables: [String: JSONValue] = ["recordKind": .string(recordKind), "recordId": .string(recordId)]
        if let passageStartMS { variables["passageStartMS"] = .number(Double(passageStartMS)) }
        if let passageEndMS { variables["passageEndMS"] = .number(Double(passageEndMS)) }
        return try await execute(Documents.readRecordMemoryLinks, variables: variables, as: Answer.self).ReadRecordMemoryLinks.compactMap { $0 }
    }

    /// A part of a page's record links, for a page with more than it read.
    public func knowledgeRecordLinks(_ path: String, first: Int = 50, offset: Int = 0) async throws -> [KnowledgeRecordLink] {
        struct Answer: Decodable, Sendable {
            struct Part: Decodable, Sendable { let recordLinks: [KnowledgeRecordLink?] }
            let ListAgentRecordLinks: Part
        }
        return try await execute(
            Documents.listKnowledgeRecordLinks, variables: ["path": .string(path), "first": .number(Double(first)), "offset": .number(Double(offset))],
            as: Answer.self
        ).ListAgentRecordLinks.recordLinks.compactMap { $0 }
    }

    /// States a link: what the person asked for, and so what confirms a
    /// guess the agent made.
    public func linkKnowledgeRecord(_ path: String, link: KnowledgeRecordLink) async throws {
        struct Answer: Decodable, Sendable {
            struct Linked: Decodable, Sendable { let id: String }
            let LinkAgentRecord: Linked?
        }
        var variables: [String: JSONValue] = [
            "path": .string(path), "recordKind": .string(link.recordKind), "recordId": .string(link.recordId),
            "recordRelation": .string(link.recordRelation),
        ]
        if link.recordKind == "recording_passage" {
            variables["passageStartMS"] = .number(Double(link.passageStartMS))
            variables["passageEndMS"] = .number(Double(link.passageEndMS))
            if !link.transcriptRunId.isEmpty { variables["transcriptRunId"] = .string(link.transcriptRunId) }
        }
        _ = try await execute(Documents.linkKnowledgeRecord, variables: variables, as: Answer.self)
    }

    /// States the agent's guesses at a page's links to one record: every
    /// passage of a recording the row stands for, or the one link of any
    /// other kind. Stating a link again is how the person confirms it.
    public func confirmKnowledgeRecord(_ path: String, link: KnowledgeRecordLink) async throws {
        struct Answer: Decodable, Sendable { let ConfirmAgentRecord: Int }
        _ = try await execute(
            Documents.confirmKnowledgeRecord,
            variables: [
                "path": .string(path), "recordKind": .string(link.recordKind), "recordId": .string(link.recordId),
                "recordRelation": .string(link.recordRelation),
            ], as: Answer.self)
    }

    /// Takes one relation of a page to a record away. The automatic passes
    /// do not make it again.
    public func unlinkKnowledgeRecord(_ path: String, link: KnowledgeRecordLink) async throws {
        struct Answer: Decodable, Sendable { let UnlinkAgentRecord: Bool }
        var variables: [String: JSONValue] = [
            "path": .string(path), "recordKind": .string(link.recordKind), "recordId": .string(link.recordId),
            "recordRelation": .string(link.recordRelation),
        ]
        // A row of a recording stands for every passage the page is linked
        // to in that relation, so no range is named and all of them go.
        _ = try await execute(Documents.unlinkKnowledgeRecord, variables: variables, as: Answer.self)
    }

    /// Names the card a person's page is about, from the person's own
    /// address books; an empty identifier clears it.
    public func setKnowledgeContact(_ path: String, contactId: String) async throws {
        struct Answer: Decodable, Sendable { let SetAgentNodeContact: Bool }
        _ = try await execute(
            Documents.setKnowledgeContact, variables: ["path": .string(path), "contactId": contactId.isEmpty ? .null : .string(contactId)],
            as: Answer.self)
    }
}
