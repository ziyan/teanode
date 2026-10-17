import Foundation

// Pages of the agent's memory shared with other people, and the pages other
// people shared. The same operations the dashboard, the command line and the
// memory tool use. What a reader is shown is decided on the server
// (internal/agent/shared_memory.go); nothing here changes a shared page.

/// SharedKnowledgeFact is one fact of a shared page, numbered as its owner
/// numbers it, with where the owner's agent read it and, when the owner chose
/// to share them, the words.
public struct SharedKnowledgeFact: Codable, Hashable, Sendable {
    public let number: Int
    public let factKind: String
    public let text: String
    public let happenedAt: String?
    public let isInferred: Bool
    public let evidenceSource: String?
    public let evidenceQuote: String?
}

/// SharedKnowledgeEdge is a link between two shared pages.
public struct SharedKnowledgeEdge: Codable, Hashable, Sendable {
    public let fromPath: String
    public let toPath: String
    public let relation: String
    public let isProposed: Bool
}

/// SharedKnowledgeChild is a page under a shared page.
public struct SharedKnowledgeChild: Codable, Hashable, Sendable {
    public let path: String
    public let name: String
    public let opening: String?
}

/// SharedKnowledgeRoot is the root of a subtree another person shared.
public struct SharedKnowledgeRoot: Codable, Hashable, Sendable, Identifiable {
    public let ownerUserId: String
    public let ownerUsername: String
    public let ownerName: String
    public let ownerAvatarUrl: String?
    public let ownerAvatarColorIndex: Int?
    public let path: String
    public let name: String
    public let nodeKind: String
    public let opening: String
    public let isQuoteShared: Bool

    public var id: String { ownerUserId + "|" + path }

    /// The name it is listed by: its own, or its path.
    public var shownName: String { name.isEmpty ? path : name }

    /// Who shared it: their name, or their username when they have none.
    public var shownOwner: String { ownerName.isEmpty ? ownerUsername : ownerName }
}

/// SharedKnowledgeOwnerGroup is the roots one person shared.
public struct SharedKnowledgeOwnerGroup: Hashable, Sendable, Identifiable {
    public let ownerUserId: String
    public let ownerName: String
    public let ownerAvatarUrl: String?
    public let ownerAvatarColorIndex: Int?
    public let roots: [SharedKnowledgeRoot]

    public var id: String { ownerUserId }

    /// The roots under whoever shared them, in the order the server listed
    /// them (by owner, then path).
    public static func grouped(_ roots: [SharedKnowledgeRoot]) -> [SharedKnowledgeOwnerGroup] {
        var order: [String] = []
        var byOwner: [String: [SharedKnowledgeRoot]] = [:]
        for root in roots {
            if byOwner[root.ownerUserId] == nil { order.append(root.ownerUserId) }
            byOwner[root.ownerUserId, default: []].append(root)
        }
        return order.compactMap { ownerUserId in
            guard let owned = byOwner[ownerUserId], let first = owned.first else { return nil }
            return SharedKnowledgeOwnerGroup(
                ownerUserId: ownerUserId, ownerName: first.shownOwner, ownerAvatarUrl: first.ownerAvatarUrl,
                ownerAvatarColorIndex: first.ownerAvatarColorIndex, roots: owned)
        }
    }
}

/// SharedKnowledgePage is a page another person shared, as the person is
/// shown it.
public struct SharedKnowledgePage: Codable, Hashable, Sendable {
    public let ownerUserId: String
    public let ownerUsername: String
    public let ownerName: String
    public let ownerAvatarUrl: String?
    public let ownerAvatarColorIndex: Int?
    public let path: String
    public let name: String
    public let nodeKind: String
    public let aliases: [String]
    public let opening: String
    public let isQuoteShared: Bool
    public let facts: [SharedKnowledgeFact?]
    public let edges: [SharedKnowledgeEdge?]
    public let children: [SharedKnowledgeChild?]

    public var shownName: String { name.isEmpty ? path : name }
    public var shownOwner: String { ownerName.isEmpty ? ownerUsername : ownerName }
    public var presentFacts: [SharedKnowledgeFact] { facts.compactMap { $0 } }
    public var presentEdges: [SharedKnowledgeEdge] { edges.compactMap { $0 } }
    public var presentChildren: [SharedKnowledgeChild] { children.compactMap { $0 } }

    /// How a fact of this page is cited: the path and number, and whose it is.
    public func citation(of fact: SharedKnowledgeFact) -> String {
        "\(path)#\(fact.number) (shared by \(shownOwner))"
    }
}

/// KnowledgeShare is one share the person made of one of their own pages.
public struct KnowledgeShare: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let path: String
    public let pageName: String?
    public let granteeUserId: String?
    public let granteeUsername: String?
    public let granteeName: String?
    public let granteeCircleId: String?
    public let granteeCircleName: String?
    public let isQuoteShared: Bool

    /// Whether it is with a circle rather than a person.
    public var isWithCircle: Bool { granteeCircleId != nil }

    /// Who it is with: the person's name, or their username when they have no
    /// name, or the circle's name.
    public var granteeDisplayName: String {
        if isWithCircle { return granteeCircleName ?? "" }
        if let granteeName, !granteeName.isEmpty { return granteeName }
        return granteeUsername ?? ""
    }

    /// Whether this share reaches the page at the path: the page itself, or
    /// one above it, at a slash ("people/a" reaches people/a/x and not
    /// people/ab).
    public func covers(_ pagePath: String) -> Bool {
        pagePath == path || pagePath.hasPrefix(path + "/")
    }
}

/// KnowledgeSharePreview is what a share would show, and how many pages it
/// would reach.
public struct KnowledgeSharePreview: Codable, Hashable, Sendable {
    public let page: SharedKnowledgePage
    public let reachedPageCount: Int
    public let isReachedPageCountCut: Bool
}

extension Documents {
    static let sharedKnowledgePageFields = """
        ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex
        path name nodeKind aliases opening isQuoteShared
        facts { number factKind text happenedAt isInferred evidenceSource evidenceQuote }
        edges { fromPath toPath relation isProposed }
        children { path name opening }
        """
    public static let knowledgeShares = """
        query { ListAgentPageShares { id path pageName granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName isQuoteShared } }
        """
    public static let shareKnowledgePage = """
        mutation ($path: String!, $granteeUserId: String, $granteeCircleId: String, $isQuoteShared: Boolean) {
          ShareAgentPage(path: $path, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, isQuoteShared: $isQuoteShared) { id }
        }
        """
    public static let unshareKnowledgePage = """
        mutation ($shareId: String!) { UnshareAgentPage(shareId: $shareId) }
        """
    public static let previewKnowledgeShare = """
        query ($path: String!, $granteeUserId: String, $granteeCircleId: String, $isQuoteShared: Boolean) {
          PreviewAgentPageShare(path: $path, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, isQuoteShared: $isQuoteShared) {
            page { \(sharedKnowledgePageFields) }
            reachedPageCount isReachedPageCountCut
          }
        }
        """
    public static let sharedKnowledgeRoots = """
        query { ListSharedAgentPages {
          ownerUserId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex path name nodeKind opening isQuoteShared
        } }
        """
    public static let sharedKnowledgePage = """
        query ($ownerUserId: String!, $path: String!) {
          ReadSharedAgentPage(ownerUserId: $ownerUserId, path: $path) { \(sharedKnowledgePageFields) }
        }
        """
}

extension Connection {
    /// The shares the person made of their own pages.
    public func knowledgeShares() async throws -> [KnowledgeShare] {
        struct Answer: Decodable, Sendable { let ListAgentPageShares: [KnowledgeShare] }
        return try await execute(Documents.knowledgeShares, as: Answer.self).ListAgentPageShares
    }

    /// Shares one of the person's pages, and everything under it, with a
    /// person or a circle (one of the two identifiers), or changes whether the
    /// quotes are shared when it is shared with them already.
    public func shareKnowledgePage(path: String, granteeUserId: String?, granteeCircleId: String?, isQuoteShared: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Shared: Decodable, Sendable { let id: String }
            let ShareAgentPage: Shared
        }
        _ = try await execute(
            Documents.shareKnowledgePage,
            variables: [
                "path": .string(path), "granteeUserId": granteeUserId.map { .string($0) } ?? .null,
                "granteeCircleId": granteeCircleId.map { .string($0) } ?? .null, "isQuoteShared": .bool(isQuoteShared),
            ], as: Answer.self)
    }

    /// Takes a share back. The reader loses the page at once: nothing was copied.
    public func unshareKnowledgePage(shareId: String) async throws {
        struct Answer: Decodable, Sendable { let UnshareAgentPage: Bool? }
        _ = try await execute(Documents.unshareKnowledgePage, variables: ["shareId": .string(shareId)], as: Answer.self)
    }

    /// Exactly what a person or a circle would be shown of a page if it were
    /// shared with them, made before there is a share.
    public func previewKnowledgeShare(path: String, granteeUserId: String?, granteeCircleId: String?, isQuoteShared: Bool) async throws
        -> KnowledgeSharePreview
    {
        struct Answer: Decodable, Sendable { let PreviewAgentPageShare: KnowledgeSharePreview }
        return try await execute(
            Documents.previewKnowledgeShare,
            variables: [
                "path": .string(path), "granteeUserId": granteeUserId.map { .string($0) } ?? .null,
                "granteeCircleId": granteeCircleId.map { .string($0) } ?? .null, "isQuoteShared": .bool(isQuoteShared),
            ], as: Answer.self
        ).PreviewAgentPageShare
    }

    /// The roots other people shared with the person, by who shared them.
    public func sharedKnowledgeRoots() async throws -> [SharedKnowledgeRoot] {
        struct Answer: Decodable, Sendable { let ListSharedAgentPages: [SharedKnowledgeRoot] }
        return try await execute(Documents.sharedKnowledgeRoots, as: Answer.self).ListSharedAgentPages
    }

    /// One page another person shared, or a page under one. A page not
    /// shared with the person is not found.
    public func sharedKnowledgePage(ownerUserId: String, path: String) async throws -> SharedKnowledgePage {
        struct Answer: Decodable, Sendable { let ReadSharedAgentPage: SharedKnowledgePage }
        return try await execute(
            Documents.sharedKnowledgePage, variables: ["ownerUserId": .string(ownerUserId), "path": .string(path)], as: Answer.self
        ).ReadSharedAgentPage
    }
}

/// SharedCitations turns a fact of a shared page cited in an answer's words,
/// "people/a-grandparent#3 (shared by Ada)", into a link to the page:
/// [that](memory:people/a-grandparent#3@ada), which the app already opens as
/// the owner's shared page. The words name the owner by display name, so the
/// username comes from the pages shared with the person.
public enum SharedCitations {
    static let pattern = try! NSRegularExpression(
        pattern: #"([a-z0-9][a-z0-9-]*(?:/[a-z0-9][a-z0-9-]*)*)#(\d{1,6}) \(shared by ([^)\n]+)\)"#)

    /// Whether the words cite a fact of a shared page.
    public static func hasCitation(in text: String) -> Bool {
        pattern.firstMatch(in: text, range: NSRange(text.startIndex..., in: text)) != nil
    }

    /// Owner display name (lower case) to username, for the people who shared
    /// pages. A name two people share is left out: a link could open either.
    public static func usernames(of roots: [SharedKnowledgeRoot]) -> [String: String] {
        var found: [String: String] = [:]
        var ambiguous: Set<String> = []
        for root in roots {
            let name = root.ownerName.trimmingCharacters(in: .whitespaces).lowercased()
            if name.isEmpty || ambiguous.contains(name) { continue }
            if let known = found[name], known != root.ownerUsername {
                found[name] = nil
                ambiguous.insert(name)
                continue
            }
            found[name] = root.ownerUsername
        }
        return found
    }

    /// The words with each shared citation whose owner is known made a link.
    /// One the agent already made a link of, or whose owner is not known, is
    /// left as it was.
    public static func linking(_ text: String, usernames: [String: String]) -> String {
        if usernames.isEmpty { return text }
        var linked = text
        let matches = pattern.matches(in: text, range: NSRange(text.startIndex..., in: text))
        // From the end, so an earlier replacement does not move a later range.
        for match in matches.reversed() {
            guard let whole = Range(match.range, in: linked),
                let path = Range(match.range(at: 1), in: linked),
                let number = Range(match.range(at: 2), in: linked),
                let name = Range(match.range(at: 3), in: linked)
            else { continue }
            if whole.lowerBound > linked.startIndex, linked[linked.index(before: whole.lowerBound)] == "[" { continue }
            if whole.upperBound < linked.endIndex, linked[whole.upperBound] == "]" { continue }
            let written = String(linked[whole])
            let owner = String(linked[name]).trimmingCharacters(in: .whitespaces).lowercased()
            guard let username = usernames[owner], !username.isEmpty,
                username.allSatisfy({ $0.isLetter || $0.isNumber || "._~-".contains($0) }),
                !written.contains("["), !written.contains("]")
            else { continue }
            let link = "[\(written)](memory:\(linked[path])#\(linked[number])@\(username))"
            linked.replaceSubrange(whole, with: link)
        }
        return linked
    }
}
