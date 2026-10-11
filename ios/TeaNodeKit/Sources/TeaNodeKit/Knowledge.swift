import Foundation

/// KnowledgeNode is a page of the agent's memory: a person, a project, a
/// topic, or a folder of pages.
public struct KnowledgeNode: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let path: String
    public let kind: String
    public let name: String
    public let summary: String
    public let aliases: [String]?
    public let pinned: Bool?
    public let dormant: Bool?
    public let contactId: String?
    public let overview: String?
    public let overviewWrittenAt: String?
    public let modifiedAt: String?

    /// The name it is shown by: its own, or the last part of its path with
    /// a capital.
    public var shownName: String {
        if !name.isEmpty { return name }
        let last = path.split(separator: "/").last.map(String.init) ?? path
        return last.prefix(1).uppercased() + last.dropFirst()
    }

    public var isFolder: Bool { kind == "folder" }
    public var isSelf: Bool { path == "self" }

    /// The path of the page it is filed under; empty for a root.
    public var parentPath: String {
        guard let cut = path.lastIndex(of: "/") else { return "" }
        return String(path[..<cut])
    }
}

/// KnowledgeRow is a page in a folder's list, with what to say under it.
public struct KnowledgeRow: Codable, Hashable, Sendable {
    public let node: KnowledgeNode?
    public let hint: String
    public let children: Int

    /// The line under its name: the hint, else the summary, at most 90
    /// characters.
    public var line: String {
        let text = hint.isEmpty ? (node?.summary ?? "") : hint
        return text.count > 90 ? String(text.prefix(89)) + "…" : text
    }

    /// Whether a tap can go into it: a folder, or a page with pages under it.
    public var opensInto: Bool { node?.isFolder == true || children > 0 }

    /// Self and pinned pages first, then those that open into more, then the
    /// rest, each group in the server's order.
    public static func ordered(_ rows: [KnowledgeRow]) -> [KnowledgeRow] {
        func rank(_ row: KnowledgeRow) -> Int {
            if row.node?.isSelf == true || row.node?.pinned == true { return 0 }
            return row.opensInto ? 1 : 2
        }
        return rows.enumerated().sorted { left, right in
            let leftRank = rank(left.element)
            let rightRank = rank(right.element)
            return leftRank != rightRank ? leftRank < rightRank : left.offset < right.offset
        }.map(\.element)
    }
}

public struct KnowledgeRowPage: Codable, Hashable, Sendable {
    public let rows: [KnowledgeRow?]
    public let total: Int
}

/// KnowledgeEvidence is where a fact came from.
public struct KnowledgeEvidence: Codable, Hashable, Sendable {
    public let kind: String
    public let id: String
    public let quote: String
}

/// KnowledgeFact is one thing the agent knows, numbered on its page.
public struct KnowledgeFact: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let number: Int
    public let kind: String
    public let text: String
    public let happenedAt: String?
    public let inferred: Bool
    public let evidence: [KnowledgeEvidence]
    public let audiences: [String]?

    /// A reflection's own kind (pattern, tension, trend, risk, question),
    /// from the dream evidence that wrote it.
    public var reflectionKind: String? {
        evidence.first { $0.kind == "dream" && $0.quote.hasPrefix("reflection: ") }
            .map { String($0.quote.dropFirst("reflection: ".count)) }
    }

    /// The pages a reflection cites, as path#number.
    public var citations: [String] {
        evidence.filter { $0.kind == "memory" && !$0.quote.isEmpty }.map(\.quote)
    }
}

public struct KnowledgeFoldedFact: Codable, Hashable, Sendable {
    public struct Folded: Codable, Hashable, Sendable {
        public let id: String
        public let number: Int
        public let text: String
    }

    public let into: Int
    public let fact: Folded?
}

/// KnowledgeAttachment is a file a fact came with.
public struct KnowledgeAttachment: Codable, Hashable, Sendable {
    public let documentId: String
    public let name: String
    public let contentType: String
    public let channel: String
    public let path: String
}

/// KnowledgePage is a page whole: its facts, the facts folded into others,
/// the pages under it, the files and the contact it is.
public struct KnowledgePage: Codable, Hashable, Sendable {
    public struct Contact: Codable, Hashable, Sendable {
        public let id: String
        public let name: String
        public let emails: [String]
        public let organization: String
    }

    public let node: KnowledgeNode?
    public let facts: [KnowledgeFact?]
    public let folded: [KnowledgeFoldedFact?]
    public let children: [KnowledgeNode?]
    public let attachments: [KnowledgeAttachment?]
    public let contact: Contact?

    public var presentFacts: [KnowledgeFact] { facts.compactMap { $0 } }

    /// The files a fact cites: its evidence ids, without any [...] part,
    /// matched to the page's attachments.
    public func attachments(of fact: KnowledgeFact) -> [KnowledgeAttachment] {
        let cited = Set(fact.evidence.map { evidence in
            evidence.id.replacingOccurrences(of: #"\[.*\]"#, with: "", options: .regularExpression)
        })
        return attachments.compactMap { $0 }.filter { cited.contains($0.documentId) }
    }
}

public struct KnowledgeRevision: Codable, Hashable, Sendable {
    public let revision: Int
    public let actor: String
    public let change: String
    public let before: String
    public let createdAt: String
}

/// KnowledgeSearch is what a search found: pages, and facts with their
/// pages, and how much more there is.
public struct KnowledgeSearch: Codable, Hashable, Sendable {
    public struct FoundFact: Codable, Hashable, Sendable {
        public struct Fact: Codable, Hashable, Sendable {
            public let id: String
            public let number: Int
            public let text: String
        }

        public let fact: Fact?
        public let path: String
        public let name: String
    }

    public let nodes: [KnowledgeNode?]
    public let facts: [FoundFact?]
    public let moreNodeCount: Int
    public let isMoreNodeCountLowerBound: Bool
    public let moreFactCount: Int
    public let isMoreFactCountLowerBound: Bool
    /// Where the next page starts; zero on the last.
    public let nextOffset: Int
}

/// KnowledgeNeighbours are a page's connections: what it is filed under,
/// the pages it links to and from, and how many are left out.
public struct KnowledgeNeighbours: Codable, Hashable, Sendable {
    public struct Neighbour: Codable, Hashable, Sendable {
        public let node: KnowledgeNode?
        public let relation: String
        public let outward: Bool
        public let note: String
        public let weight: Double
        /// "proposed" for a link the agent guessed.
        public let status: String
    }

    public let node: KnowledgeNode?
    public let parent: KnowledgeNode?
    public let neighbours: [Neighbour?]
    public let children: Int
    public let links: Int
}

/// KnowledgeRecall is what the agent's memory brings up for a question.
public struct KnowledgeRecall: Codable, Hashable, Sendable {
    public struct Page: Codable, Hashable, Sendable {
        public struct Fact: Codable, Hashable, Sendable {
            public let number: Int
            public let text: String
        }

        public let path: String
        public let facts: [Fact?]
    }

    public let pages: [Page?]
}

/// DocumentSearch is what a search of the agent's sources found.
public struct DocumentSearch: Codable, Hashable, Sendable {
    public struct Passage: Codable, Hashable, Sendable {
        public let documentId: String
        public let externalId: String
        public let title: String
        public let author: String
        public let source: String
        public let happenedAt: String?
        public let number: Int
        public let text: String
    }

    public struct Definition: Codable, Hashable, Sendable {
        public let symbol: String
        public let kind: String
        public let documentId: String
        public let title: String
    }

    public struct Directory: Codable, Hashable, Sendable {
        public let directory: String
        public let sourceId: String
        public let source: String
        public let passageCount: Int
    }

    public let passages: [Passage?]
    public let definitions: [Definition?]
    public let directories: [Directory?]
    public let meaningful: Bool
    public let moreCount: Int
    public let isMoreCountLowerBound: Bool
    public let nextOffset: Int
}

/// DocumentExtract is a slice of a document, read a part at a time.
public struct DocumentExtract: Codable, Hashable, Sendable {
    public let documentId: String
    public let title: String
    public let author: String
    public let source: String
    public let happenedAt: String?
    public let text: String
    public let total: Int
    /// Where the next slice starts; zero at the end.
    public let next: Int
}

/// KnowledgeSource is a place the agent reads: what it is, where, how often,
/// and how far the reading has got. A list that asks only for the id and the
/// name gets the rest as empty.
public struct KnowledgeSource: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    public var kind: String = ""
    public var specification = Specification()
    /// Where in the graph what it finds is filed.
    public var rootPath: String = ""
    public var enabled: Bool = true
    public var cron: String = ""
    public var lastRunAt: String?
    public var lastError: String = ""
    public var documentCount: Int = 0
    public var chunkCount: Int = 0
    public var refusedCount: Int = 0
    /// Whether the last pass left work behind and wants another.
    public var more: Bool = false
    /// The checkouts under it holding none of the person's commits, kept to
    /// what git says about them, and how many files that left unread.
    public var checkoutsKeptToProfile: Int = 0
    public var filesKeptToProfile: Int = 0

    public init(id: String, name: String, kind: String = "", specification: Specification = Specification(), enabled: Bool = true) {
        self.id = id
        self.name = name
        self.kind = kind
        self.specification = specification
        self.enabled = enabled
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        name = try container.decode(String.self, forKey: .name)
        kind = try container.decodeIfPresent(String.self, forKey: .kind) ?? ""
        specification = try container.decodeIfPresent(Specification.self, forKey: .specification) ?? Specification()
        rootPath = try container.decodeIfPresent(String.self, forKey: .rootPath) ?? ""
        enabled = try container.decodeIfPresent(Bool.self, forKey: .enabled) ?? true
        cron = try container.decodeIfPresent(String.self, forKey: .cron) ?? ""
        lastRunAt = try container.decodeIfPresent(String.self, forKey: .lastRunAt)
        lastError = try container.decodeIfPresent(String.self, forKey: .lastError) ?? ""
        documentCount = try container.decodeIfPresent(Int.self, forKey: .documentCount) ?? 0
        chunkCount = try container.decodeIfPresent(Int.self, forKey: .chunkCount) ?? 0
        refusedCount = try container.decodeIfPresent(Int.self, forKey: .refusedCount) ?? 0
        more = try container.decodeIfPresent(Bool.self, forKey: .more) ?? false
        checkoutsKeptToProfile = try container.decodeIfPresent(Int.self, forKey: .checkoutsKeptToProfile) ?? 0
        filesKeptToProfile = try container.decodeIfPresent(Int.self, forKey: .filesKeptToProfile) ?? 0
    }
}

/// KnowledgeWords are the fixed vocabularies the page editor offers.
public enum KnowledgeWords {
    public static let factKinds = ["fact", "preference", "decision", "event", "howto"]
    public static let audiences = ["triage", "reply", "research", "summaries"]
    public static let relations = [
        "part_of", "works_on", "member_of", "knows", "owns", "uses", "located_in", "related_to", "decided_in", "about", "depends_on",
    ]

    /// Whether a "when it was true" is one the server reads: a year, a
    /// month or a day.
    public static func isHappenedValid(_ text: String) -> Bool {
        text.isEmpty || text.range(of: #"^\d{4}(-\d{2}(-\d{2})?)?$"#, options: .regularExpression) != nil
    }
}

extension Documents {
    static let nodeFields = """
        id path kind name summary aliases pinned dormant contactId overview overviewWrittenAt modifiedAt
        """
    public static let knowledgeChildren = """
        query ($path: String!, $first: Int, $offset: Int) {
          AgentGraphChildren(path: $path, first: $first, offset: $offset) {
            rows { node { \(nodeFields) } hint children }
            total
          }
        }
        """
    public static let knowledgeSearch = """
        query ($query: String!, $first: Int, $offset: Int) {
          SearchAgentGraph(query: $query, first: $first, offset: $offset) {
            nodes { \(nodeFields) }
            facts { fact { id number text } path name }
            moreNodeCount isMoreNodeCountLowerBound moreFactCount isMoreFactCountLowerBound nextOffset
          }
        }
        """
    public static let knowledgePage = """
        query ($path: String!) {
          AgentGraphPage(path: $path) {
            node { \(nodeFields) }
            facts { id number kind text happenedAt inferred evidence { kind id quote } audiences }
            folded { into fact { id number text } }
            children { \(nodeFields) }
            attachments { documentId name contentType channel path }
            contact { id name emails organization }
          }
        }
        """
    public static let knowledgeHistory = """
        query ($path: String!, $first: Int) {
          ListAgentPageHistory(path: $path, first: $first) { revision actor change before createdAt }
        }
        """
    public static let knowledgeNeighbours = """
        query ($path: String!) {
          AgentGraphNeighbours(path: $path) {
            node { \(nodeFields) }
            parent { \(nodeFields) }
            neighbours { node { \(nodeFields) } relation outward note weight status }
            children links
          }
        }
        """
    public static let saveKnowledgeNode = """
        mutation ($path: String!, $kind: String, $name: String, $summary: String, $aliases: [String!], $pinned: Boolean) {
          SaveAgentNode(path: $path, kind: $kind, name: $name, summary: $summary, aliases: $aliases, pinned: $pinned) { id path }
        }
        """
    public static let saveKnowledgeFact = """
        mutation ($path: String!, $number: Int, $kind: String, $text: String!, $happened: String, $audiences: [String!]) {
          SaveAgentFact(path: $path, number: $number, kind: $kind, text: $text, happened: $happened, audiences: $audiences) { id number }
        }
        """
    public static let moveKnowledgeNode = """
        mutation ($path: String!, $under: String!) { MoveAgentNode(path: $path, under: $under) { id path } }
        """
    public static let mergeKnowledgeNodes = """
        mutation ($path: String!, $into: String!) { MergeAgentNodes(path: $path, into: $into) { id path } }
        """
    public static let moveKnowledgeFact = """
        mutation ($path: String!, $number: Int!, $to: String!) { MoveAgentFact(path: $path, number: $number, to: $to) { id number } }
        """
    public static let linkKnowledgeNodes = """
        mutation ($path: String!, $to: String!, $relation: String!, $note: String) {
          LinkAgentNodes(path: $path, to: $to, relation: $relation, note: $note)
        }
        """
    public static let unlinkKnowledgeNodes = """
        mutation ($path: String!, $to: String!, $relation: String!) {
          UnlinkAgentNodes(path: $path, to: $to, relation: $relation)
        }
        """
    public static let deleteKnowledgeFact = """
        mutation ($path: String!, $number: Int!) { DeleteAgentFact(path: $path, number: $number) }
        """
    public static let deleteKnowledgeNode = """
        mutation ($path: String!) { DeleteAgentNode(path: $path) }
        """
    public static let recallMemory = """
        query ($question: String!) { RecallAgentMemory(question: $question) { pages { path facts { number text } } } }
        """
    public static let searchDocuments = """
        query ($query: String!, $first: Int, $offset: Int, $sourceId: String, $directory: String) {
          SearchAgentDocuments(query: $query, first: $first, offset: $offset, sourceId: $sourceId, directory: $directory) {
            passages { documentId externalId title author source happenedAt number text }
            definitions { symbol kind documentId title }
            directories { directory sourceId source passageCount }
            meaningful moreCount isMoreCountLowerBound nextOffset
          }
        }
        """
    public static let readDocument = """
        query ($documentId: String!, $from: Int) {
          ReadAgentDocument(documentId: $documentId, from: $from) { documentId title author source happenedAt text total next }
        }
        """
    public static let knowledgeSources = """
        query { ListAgentKnowledgeSources { id name } }
        """
}

extension Connection {
    public func knowledgeChildren(of path: String, first: Int = 50, offset: Int = 0) async throws -> KnowledgeRowPage {
        struct Answer: Decodable, Sendable { let AgentGraphChildren: KnowledgeRowPage }
        return try await execute(Documents.knowledgeChildren, variables: [
            "path": .string(path), "first": .number(Double(first)), "offset": .number(Double(offset)),
        ], as: Answer.self).AgentGraphChildren
    }

    public func searchKnowledge(_ words: String, first: Int = 60, offset: Int = 0) async throws -> KnowledgeSearch {
        struct Answer: Decodable, Sendable { let SearchAgentGraph: KnowledgeSearch }
        return try await execute(Documents.knowledgeSearch, variables: [
            "query": .string(words), "first": .number(Double(first)), "offset": .number(Double(offset)),
        ], as: Answer.self).SearchAgentGraph
    }

    /// A page whole, or nil when there is no page at the path.
    public func knowledgePage(_ path: String) async throws -> KnowledgePage? {
        struct Answer: Decodable, Sendable { let AgentGraphPage: KnowledgePage? }
        return try await execute(Documents.knowledgePage, variables: ["path": .string(path)], as: Answer.self).AgentGraphPage
    }

    public func knowledgeHistory(_ path: String, first: Int = 50) async throws -> [KnowledgeRevision] {
        struct Answer: Decodable, Sendable { let ListAgentPageHistory: [KnowledgeRevision?] }
        return try await execute(Documents.knowledgeHistory, variables: ["path": .string(path), "first": .number(Double(first))], as: Answer.self)
            .ListAgentPageHistory.compactMap { $0 }
    }

    public func knowledgeNeighbours(_ path: String) async throws -> KnowledgeNeighbours? {
        struct Answer: Decodable, Sendable { let AgentGraphNeighbours: KnowledgeNeighbours? }
        return try await execute(Documents.knowledgeNeighbours, variables: ["path": .string(path)], as: Answer.self).AgentGraphNeighbours
    }

    /// Saves a page's name, summary, other names and pin. The summary is
    /// always written, an empty one clearing it; the other names only when
    /// given.
    public func saveKnowledgeNode(path: String, name: String, summary: String, aliases: [String]?, isPinned: Bool?) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveAgentNode: Saved?
        }
        var variables: [String: JSONValue] = ["path": .string(path), "name": .string(name), "summary": .string(summary)]
        if let aliases { variables["aliases"] = .array(aliases.map { .string($0) }) }
        if let isPinned { variables["pinned"] = .bool(isPinned) }
        _ = try await execute(Documents.saveKnowledgeNode, variables: variables, as: Answer.self)
    }

    /// Adds a fact, or changes one by its number. Returns its number, which
    /// for a new fact may be an older one's with the same words.
    public func saveKnowledgeFact(
        path: String, number: Int?, kind: String, text: String, happened: String, audiences: [String]?
    ) async throws -> Int {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let number: Int }
            let SaveAgentFact: Saved
        }
        var variables: [String: JSONValue] = [
            "path": .string(path), "kind": .string(kind), "text": .string(text), "happened": .string(happened),
        ]
        if let number { variables["number"] = .number(Double(number)) }
        if let audiences { variables["audiences"] = .array(audiences.map { .string($0) }) }
        return try await execute(Documents.saveKnowledgeFact, variables: variables, as: Answer.self).SaveAgentFact.number
    }

    /// Files a page under another; returns its new path.
    public func moveKnowledgeNode(_ path: String, under: String) async throws -> String? {
        struct Answer: Decodable, Sendable {
            struct Moved: Decodable, Sendable { let path: String }
            let MoveAgentNode: Moved?
        }
        return try await execute(Documents.moveKnowledgeNode, variables: ["path": .string(path), "under": .string(under)], as: Answer.self)
            .MoveAgentNode?.path
    }

    /// Folds a page into another; returns the path that is left.
    public func mergeKnowledgeNodes(_ path: String, into: String) async throws -> String? {
        struct Answer: Decodable, Sendable {
            struct Merged: Decodable, Sendable { let path: String }
            let MergeAgentNodes: Merged?
        }
        return try await execute(Documents.mergeKnowledgeNodes, variables: ["path": .string(path), "into": .string(into)], as: Answer.self)
            .MergeAgentNodes?.path
    }

    public func moveKnowledgeFact(path: String, number: Int, to: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Moved: Decodable, Sendable { let number: Int }
            let MoveAgentFact: Moved?
        }
        _ = try await execute(Documents.moveKnowledgeFact, variables: [
            "path": .string(path), "number": .number(Double(number)), "to": .string(to),
        ], as: Answer.self)
    }

    public func linkKnowledgeNodes(path: String, to: String, relation: String, note: String) async throws {
        struct Answer: Decodable, Sendable { let LinkAgentNodes: JSONValue? }
        _ = try await execute(Documents.linkKnowledgeNodes, variables: [
            "path": .string(path), "to": .string(to), "relation": .string(relation), "note": .string(note),
        ], as: Answer.self)
    }

    public func unlinkKnowledgeNodes(path: String, to: String, relation: String) async throws {
        struct Answer: Decodable, Sendable { let UnlinkAgentNodes: JSONValue? }
        _ = try await execute(Documents.unlinkKnowledgeNodes, variables: [
            "path": .string(path), "to": .string(to), "relation": .string(relation),
        ], as: Answer.self)
    }

    /// Forgets a fact; the agent is shown it as what not to keep.
    public func deleteKnowledgeFact(path: String, number: Int) async throws {
        struct Answer: Decodable, Sendable { let DeleteAgentFact: JSONValue? }
        _ = try await execute(Documents.deleteKnowledgeFact, variables: ["path": .string(path), "number": .number(Double(number))], as: Answer.self)
    }

    /// Forgets a page and every page under it.
    public func deleteKnowledgeNode(_ path: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteAgentNode: JSONValue? }
        _ = try await execute(Documents.deleteKnowledgeNode, variables: ["path": .string(path)], as: Answer.self)
    }

    public func recallMemory(_ question: String) async throws -> KnowledgeRecall {
        struct Answer: Decodable, Sendable { let RecallAgentMemory: KnowledgeRecall }
        return try await execute(Documents.recallMemory, variables: ["question": .string(question)], as: Answer.self).RecallAgentMemory
    }

    public func searchDocuments(_ words: String, sourceId: String?, directory: String?, first: Int = 20, offset: Int = 0) async throws -> DocumentSearch {
        struct Answer: Decodable, Sendable { let SearchAgentDocuments: DocumentSearch }
        var variables: [String: JSONValue] = ["query": .string(words), "first": .number(Double(first)), "offset": .number(Double(offset))]
        if let sourceId, !sourceId.isEmpty { variables["sourceId"] = .string(sourceId) }
        if let directory, !directory.isEmpty { variables["directory"] = .string(directory) }
        return try await execute(Documents.searchDocuments, variables: variables, as: Answer.self).SearchAgentDocuments
    }

    /// A slice of a document from a character on; nil when it is no longer
    /// kept.
    public func readDocument(_ documentId: String, from: Int) async throws -> DocumentExtract? {
        struct Answer: Decodable, Sendable { let ReadAgentDocument: DocumentExtract? }
        return try await execute(Documents.readDocument, variables: ["documentId": .string(documentId), "from": .number(Double(from))], as: Answer.self)
            .ReadAgentDocument
    }

    public func knowledgeSources() async throws -> [KnowledgeSource] {
        struct Answer: Decodable, Sendable { let ListAgentKnowledgeSources: [KnowledgeSource?] }
        return try await execute(Documents.knowledgeSources, as: Answer.self).ListAgentKnowledgeSources.compactMap { $0 }
    }
}
