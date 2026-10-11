import Foundation

/// AccountSession is one browser signed in to the person's account, as the
/// dashboard's Sessions page lists it.
public struct AccountSession: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    /// Whether this is the session reading the list. It is never revoked
    /// from the list: signing out is what ends one's own.
    public let current: Bool
    public let created: String
    public let expires: String?
    public let lastUsed: String?
    public let ip: String?
    public let userAgent: String?
    /// When it was ended, or nil while it still works.
    public let revoked: String?
    /// The operator who signed in as this account with it, when it was one.
    public let impersonatorUsername: String?

    public init(
        id: String, current: Bool = false, created: String, expires: String? = nil, lastUsed: String? = nil, ip: String? = nil,
        userAgent: String? = nil, revoked: String? = nil, impersonatorUsername: String? = nil
    ) {
        self.id = id
        self.current = current
        self.created = created
        self.expires = expires
        self.lastUsed = lastUsed
        self.ip = ip
        self.userAgent = userAgent
        self.revoked = revoked
        self.impersonatorUsername = impersonatorUsername
    }

    /// Whether the list offers to end it: not the one reading the list, and
    /// not one already ended.
    public var canRevoke: Bool { !current && revoked == nil }

    /// The part of the user agent a person recognizes, such as "Firefox ·
    /// Windows NT 10.0"; nil when the browser sent none. Not a parser:
    /// guessing wrongly is worse than showing less.
    public var browserName: String? {
        guard let userAgent, !userAgent.isEmpty else { return nil }
        guard let browserMatch = userAgent.firstMatch(of: #/(Firefox|Edg|OPR|Chrome|Safari)\/[\d.]+/#) else {
            return userAgent.count > 60 ? String(userAgent.prefix(60)) + "…" : userAgent
        }
        let browserNames = ["Edg": "Edge", "OPR": "Opera"]
        let browser = browserNames[String(browserMatch.1)] ?? String(browserMatch.1)
        guard let platformMatch = userAgent.firstMatch(of: #/\(([^)]+)\)/#) else { return browser }
        let platform = platformMatch.1.split(separator: ";", omittingEmptySubsequences: false).first ?? ""
        return "\(browser) · \(platform.trimmingCharacters(in: .whitespaces))"
    }

    /// The order the list shows them in: the one reading it first, since
    /// it is the one signing out everywhere also ends, then the rest as the
    /// server sent them, newest first.
    public static func listed(_ sessions: [AccountSession]) -> [AccountSession] {
        sessions.filter(\.current) + sessions.filter { !$0.current }
    }
}

/// APIToken is a token the command line tool authenticates with, without
/// its secret, which is shown once when it is made.
public struct APIToken: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String
    /// The account it belongs to, and acts as.
    public let username: String
    public let created: String
    /// When it stops working, or nil when it does not expire.
    public let expires: String?
    public let lastUsed: String?
    public let lastUsedIp: String?
    /// When it was revoked, or nil while it still works.
    public let revoked: String?

    public init(
        id: String, name: String, username: String, created: String, expires: String? = nil, lastUsed: String? = nil,
        lastUsedIp: String? = nil, revoked: String? = nil
    ) {
        self.id = id
        self.name = name
        self.username = username
        self.created = created
        self.expires = expires
        self.lastUsed = lastUsed
        self.lastUsedIp = lastUsedIp
        self.revoked = revoked
    }

    /// When it stops working, as of now.
    public func expiry(now: Date = Date()) -> APITokenExpiry {
        guard let expiresAt = ServerTime.date(expires) else { return .never }
        return expiresAt < now ? .expired : .expires(expiresAt)
    }
}

/// APITokenExpiry is when a token stops working: never, already, or at a
/// time to come. A token that never expires is a standing risk, so the list
/// marks it quietly.
public enum APITokenExpiry: Hashable, Sendable {
    case never
    case expired
    case expires(Date)
}

/// APITokenLifetime is how long a token lasts: a handful of sensible
/// answers rather than a number to type, as the dashboard offers them.
public enum APITokenLifetime: String, CaseIterable, Hashable, Sendable, Identifiable {
    case thirtyDays = "720h"
    case ninetyDays = "2160h"
    case year = "8760h"
    case never

    public var id: String { rawValue }

    /// The lifetime a new token is given unless the person picks another.
    public static let standard = APITokenLifetime.ninetyDays

    /// As CreateToken takes it: a duration, or nil for a token that does
    /// not expire.
    var creationLifetime: JSONValue {
        self == .never ? .null : .string(rawValue)
    }

    /// As UpdateToken takes it, counted from now: a duration, or "never".
    var updatedLifetime: JSONValue { .string(rawValue) }
}

/// APITokenChange is what the edit form changes about a token: its name,
/// when it differs from the one it has, and a new lifetime counted from
/// now, or nil to keep the expiry as it is. The secret stays the same.
public struct APITokenChange: Hashable, Sendable {
    public let tokenId: String
    /// The new name, or nil to keep the one it has.
    public let name: String?
    /// The new lifetime, or nil to keep the expiry it has.
    public let lifetime: APITokenLifetime?

    public init(token: APIToken, newName: String, lifetime: APITokenLifetime?) {
        let trimmedName = newName.trimmingCharacters(in: .whitespacesAndNewlines)
        tokenId = token.id
        name = trimmedName == token.name ? nil : trimmedName
        self.lifetime = lifetime
    }

    /// Whether the change would do anything at all.
    public var hasChanges: Bool { name != nil || lifetime != nil }

    var variables: [String: JSONValue] {
        [
            "tokenId": .string(tokenId),
            "name": name.map(JSONValue.string) ?? .null,
            "lifetime": lifetime?.updatedLifetime ?? .null,
        ]
    }
}

/// ConnectedApp is a program the person authorized over OAuth to act as
/// them. It renews its own token, so the app, not the token, is what is
/// named and disconnected.
public struct ConnectedApp: Codable, Hashable, Sendable, Identifiable {
    public let clientId: String
    /// What the person calls it: the name on its tokens.
    public let name: String
    /// The name it gave itself when it registered.
    public let registeredName: String
    /// The host of each address it asked to be sent back to.
    public let redirectHosts: [String]
    public let registered: String?
    public let lastUsed: String?
    public let lastUsedIp: String?
    public let expires: String?
    /// The last moment it can renew; unused past this, it is disconnected.
    public let renewableUntil: String?
    /// How many tokens it holds for the person: more than one when it was
    /// authorized more than once.
    public let tokenCount: Int

    public var id: String { clientId }

    public init(
        clientId: String, name: String, registeredName: String = "", redirectHosts: [String] = [], registered: String? = nil,
        lastUsed: String? = nil, lastUsedIp: String? = nil, expires: String? = nil, renewableUntil: String? = nil, tokenCount: Int = 1
    ) {
        self.clientId = clientId
        self.name = name
        self.registeredName = registeredName
        self.redirectHosts = redirectHosts
        self.registered = registered
        self.lastUsed = lastUsed
        self.lastUsedIp = lastUsedIp
        self.expires = expires
        self.renewableUntil = renewableUntil
        self.tokenCount = tokenCount
    }

    /// The name the list shows: the person's for it, else the one it
    /// registered with; nil when it has neither.
    public var displayName: String? {
        if !name.isEmpty { return name }
        if !registeredName.isEmpty { return registeredName }
        return nil
    }

    /// The name it registered with, when it differs from the person's.
    public var differentRegisteredName: String? {
        registeredName.isEmpty || registeredName == name ? nil : registeredName
    }
}

extension Documents {
    public static let accountSessions = """
        query ($includeRevoked: Boolean) {
          ListSessions(includeRevoked: $includeRevoked) {
            id current created expires lastUsed ip userAgent revoked impersonatorUsername
          }
        }
        """
    public static let revokeSession = """
        mutation ($sessionId: String!) { RevokeSession(sessionId: $sessionId) }
        """
    public static let revokeAllSessions = """
        mutation { RevokeAllSessions { authenticated } }
        """
    public static let apiTokens = """
        query ($includeRevoked: Boolean) {
          ListTokens(includeRevoked: $includeRevoked) {
            id name username created expires lastUsed lastUsedIp revoked
          }
        }
        """
    public static let createApiToken = """
        mutation ($name: String!, $lifetime: String) {
          CreateToken(name: $name, lifetime: $lifetime) {
            secret
            token { id name username created expires lastUsed lastUsedIp revoked }
          }
        }
        """
    public static let updateApiToken = """
        mutation ($tokenId: String!, $name: String, $lifetime: String) {
          UpdateToken(tokenId: $tokenId, name: $name, lifetime: $lifetime) { id }
        }
        """
    public static let deleteApiToken = """
        mutation ($tokenId: String!) { DeleteToken(tokenId: $tokenId) }
        """
    public static let connectedApps = """
        query {
          ListApps {
            clientId name registeredName redirectHosts registered lastUsed lastUsedIp expires renewableUntil tokenCount
          }
        }
        """
    public static let renameConnectedApp = """
        mutation ($clientId: String!, $name: String!) { RenameApp(clientId: $clientId, name: $name) }
        """
    public static let disconnectApp = """
        mutation ($clientId: String!) { DisconnectApp(clientId: $clientId) }
        """
}

extension Connection {
    /// The browsers signed in to the person's account, newest first; the
    /// ended ones too when asked, so a revocation stays visible a while.
    public func accountSessions(includeRevoked: Bool = false) async throws -> [AccountSession] {
        struct Answer: Decodable, Sendable { let ListSessions: [AccountSession?] }
        return try await execute(Documents.accountSessions, variables: ["includeRevoked": .bool(includeRevoked)], as: Answer.self)
            .ListSessions.compactMap { $0 }
    }

    /// Signs one browser out; the others are untouched.
    public func revokeSession(_ sessionId: String) async throws {
        struct Answer: Decodable, Sendable { let RevokeSession: JSONValue? }
        _ = try await execute(Documents.revokeSession, variables: ["sessionId": .string(sessionId)], as: Answer.self)
    }

    /// Signs every browser on the account out. This phone signs in with a
    /// token of its own, not a session, so it stays signed in.
    public func revokeAllSessions() async throws {
        struct Answer: Decodable, Sendable { let RevokeAllSessions: JSONValue? }
        _ = try await execute(Documents.revokeAllSessions, as: Answer.self)
    }

    /// The person's API tokens, without their secrets; the revoked ones too
    /// when asked. The tokens apps hold are listed by app instead.
    public func apiTokens(includeRevoked: Bool = false) async throws -> [APIToken] {
        struct Answer: Decodable, Sendable { let ListTokens: [APIToken?] }
        return try await execute(Documents.apiTokens, variables: ["includeRevoked": .bool(includeRevoked)], as: Answer.self)
            .ListTokens.compactMap { $0 }
    }

    /// Makes an API token and returns its secret, which is shown this once:
    /// only a hash of it is kept.
    public func createApiToken(name: String, lifetime: APITokenLifetime) async throws -> (secret: String, token: APIToken?) {
        struct Answer: Decodable, Sendable {
            struct Created: Decodable, Sendable {
                let secret: String
                let token: APIToken?
            }
            let CreateToken: Created
        }
        let created = try await execute(
            Documents.createApiToken, variables: ["name": .string(name), "lifetime": lifetime.creationLifetime], as: Answer.self
        ).CreateToken
        return (created.secret, created.token)
    }

    /// Renames a token or gives it a new lifetime counted from now; the
    /// token itself stays the same, so whatever holds it keeps working.
    public func updateApiToken(_ change: APITokenChange) async throws {
        struct Answer: Decodable, Sendable { let UpdateToken: JSONValue? }
        _ = try await execute(Documents.updateApiToken, variables: change.variables, as: Answer.self)
    }

    /// Revokes a token: whatever uses it stops working at once.
    public func deleteApiToken(_ tokenId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteToken: JSONValue? }
        _ = try await execute(Documents.deleteApiToken, variables: ["tokenId": .string(tokenId)], as: Answer.self)
    }

    /// The apps that hold a token of the person's, one row an app.
    public func connectedApps() async throws -> [ConnectedApp] {
        struct Answer: Decodable, Sendable { let ListApps: [ConnectedApp?] }
        return try await execute(Documents.connectedApps, as: Answer.self).ListApps.compactMap { $0 }
    }

    /// Renames an app, on the tokens it holds and on each it renews into.
    public func renameConnectedApp(_ clientId: String, name: String) async throws {
        struct Answer: Decodable, Sendable { let RenameApp: JSONValue? }
        _ = try await execute(Documents.renameConnectedApp, variables: ["clientId": .string(clientId), "name": .string(name)], as: Answer.self)
    }

    /// Disconnects an app: every token it holds for the person is revoked,
    /// so it has to be authorized again to come back.
    public func disconnectApp(_ clientId: String) async throws {
        struct Answer: Decodable, Sendable { let DisconnectApp: JSONValue? }
        _ = try await execute(Documents.disconnectApp, variables: ["clientId": .string(clientId)], as: Answer.self)
    }
}
