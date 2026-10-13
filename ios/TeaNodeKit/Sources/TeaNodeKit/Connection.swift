import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AccountStore keeps an account between launches: the keychain in the app,
/// memory in a test.
public protocol AccountStore: Sendable {
    func save(_ account: Account) throws
    func load() throws -> Account?
    /// The kept sign-in of one account, whether or not it is the one in use.
    func load(accountKey: String) throws -> Account?
    func delete() throws
}

extension AccountStore {
    /// A store that keeps one sign-in has only the one in use.
    public func load(accountKey: String) throws -> Account? {
        try load().flatMap { $0.accountKey == accountKey ? $0 : nil }
    }
}

/// Connection is the app talking to its server as the person: GraphQL over
/// HTTPS with the account's token, renewed before it runs out and kept
/// wherever the store keeps it.
public actor Connection {
    /// The account identity remains the same when its tokens are renewed.
    public nonisolated let accountKey: String
    public internal(set) var account: Account
    let transport: any HTTPTransport
    let store: (any AccountStore)?

    /// The renewal under way. Whoever needs a token meanwhile waits for it
    /// rather than renewing again: the server retires a refresh token the
    /// first time it is used, so a second renewal with it is refused.
    private var renewal: Task<Account, Error>?

    /// Whether the server has ended this sign-in: it refused the refresh
    /// token, or refused a request again with a token loaded afresh. The app
    /// signs out of an account only when its own connection says so.
    public private(set) var isSignedOut = false

    /// renewBefore is how long before a token runs out it is renewed.
    static let renewBefore: TimeInterval = 24 * 60 * 60

    public init(account: Account, transport: any HTTPTransport, store: (any AccountStore)? = nil) {
        self.accountKey = account.accountKey
        self.account = account
        self.transport = transport
        self.store = store
    }

    /// The token to send now, renewed first when it is close to running out.
    public func accessToken(now: Date = Date()) async throws -> String {
        if let renewal {
            return try await outcome(of: renewal).accessToken
        }
        // The app and its extensions share the stored sign-in. One that
        // renewed it has stored a newer one, whose refresh token is the
        // only one still good: it is taken up before renewing again.
        if account.expiresAt.timeIntervalSince(now) < Self.renewBefore, let stored = try? store?.load(accountKey: account.accountKey),
            stored.expiresAt > account.expiresAt
        {
            try Self.requireSameRecordingOwner(account, stored)
            account = stored
        }
        if account.expiresAt.timeIntervalSince(now) < Self.renewBefore, !account.refreshToken.isEmpty {
            return try await renew(now: now).accessToken
        }
        return account.accessToken
    }

    /// Renews the sign-in once, however many callers ask while it is under
    /// way, and stores it.
    func renew(now: Date) async throws -> Account {
        if let renewal {
            return try await outcome(of: renewal)
        }
        let renewing = Task { [account, transport, store] in
            try await Self.renewed(account, transport: transport, store: store, now: now)
        }
        renewal = renewing
        defer { renewal = nil }
        return try await outcome(of: renewing)
    }

    /// Waits for a renewal and keeps what it gave. Every caller waiting
    /// keeps it, so a refusal has ended the sign-in by the time any of them
    /// hears of it.
    private func outcome(of renewing: Task<Account, Error>) async throws -> Account {
        do {
            let renewed = try await renewing.value
            try Self.requireSameRecordingOwner(account, renewed)
            // A caller that wakes late does not put back an older sign-in.
            if renewed.expiresAt >= account.expiresAt {
                account = renewed
            }
            return renewed
        } catch {
            if error as? TeaNodeError == .signedOut {
                isSignedOut = true
            }
            throw error
        }
    }

    /// The sign-in renewed and stored. A refused refresh token is checked
    /// against the store before the sign-in counts as ended: the widget, the
    /// share sheet or Siri may have renewed it a moment before, which
    /// retired the token this connection holds, and stored the one to go on
    /// with.
    static func renewed(_ account: Account, transport: any HTTPTransport, store: (any AccountStore)?, now: Date) async throws -> Account {
        do {
            var renewed = try await SignIn(server: account.server, transport: transport).refresh(account, now: now)
            renewed.cachedRecordingUserId = account.cachedRecordingUserId
            if let store, account.cachedRecordingUserId != nil {
                guard let retained = try store.load(accountKey: account.accountKey) else { throw TeaNodeError.signedOut }
                try requireSameRecordingOwner(account, retained)
                if retained.refreshToken != account.refreshToken {
                    guard retained.expiresAt >= renewed.expiresAt else { throw TeaNodeError.signedOut }
                    return retained
                }
                try store.save(renewed)
            } else {
                try? store?.save(renewed)
            }
            return renewed
        } catch TeaNodeError.signedOut {
            if let stored = try? store?.load(accountKey: account.accountKey), !stored.refreshToken.isEmpty,
                stored.refreshToken != account.refreshToken
            {
                try requireSameRecordingOwner(account, stored)
                return stored
            }
            throw TeaNodeError.signedOut
        }
    }

    private static func requireSameRecordingOwner(_ current: Account, _ replacement: Account) throws {
        if let ownerId = current.cachedRecordingUserId, replacement.cachedRecordingUserId != ownerId {
            throw TeaNodeError.signedOut
        }
    }

    /// Sends a request with the account's token. A 401 is tried once more
    /// with a token loaded afresh, since another process may have renewed
    /// the sign-in and so retired the token sent; a second 401 means the
    /// sign-in has ended.
    func send(authorizing request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        var request = request
        let token = try await accessToken()
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, response) = try await transport.send(request)
        guard response.statusCode == 401 else {
            return (data, response)
        }
        let freshToken = try await accessToken(replacing: token)
        request.setValue("Bearer \(freshToken)", forHTTPHeaderField: "Authorization")
        let (retriedData, retriedResponse) = try await transport.send(request)
        if retriedResponse.statusCode == 401 {
            isSignedOut = true
            throw TeaNodeError.signedOut
        }
        return (retriedData, retriedResponse)
    }

    /// A token to use in place of one the server refused: a newer one this
    /// connection or another process already has, or else a renewal.
    func accessToken(replacing refused: String, now: Date = Date()) async throws -> String {
        if let renewal {
            return try await outcome(of: renewal).accessToken
        }
        if account.accessToken != refused {
            return account.accessToken
        }
        if let stored = try? store?.load(accountKey: account.accountKey), stored.accessToken != refused,
            stored.expiresAt >= account.expiresAt
        {
            try Self.requireSameRecordingOwner(account, stored)
            account = stored
            return stored.accessToken
        }
        guard !account.refreshToken.isEmpty else {
            isSignedOut = true
            throw TeaNodeError.signedOut
        }
        return try await renew(now: now).accessToken
    }

    struct GraphQLAnswer<Answer: Decodable>: Decodable {
        let data: Answer?
        let errors: [GraphQLError]?
    }

    /// Runs a GraphQL document and decodes its data.
    public func execute<Answer: Decodable & Sendable>(_ document: String, variables: [String: JSONValue] = [:], as answer: Answer.Type) async throws -> Answer {
        var request = URLRequest(url: account.server.endpoint("/api/v1/graphql"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(["query": JSONValue.string(document), "variables": .object(variables)])
        let (data, _) = try await send(authorizing: request)
        // The errors are read on their own first: a refused field comes back
        // as null beside them, which the answer's own shape does not allow.
        if let error = (try? JSONDecoder().decode(GraphQLAnswer<JSONValue>.self, from: data))?.errors?.first {
            throw TeaNodeError.refused(error.message)
        }
        let decoded: GraphQLAnswer<Answer>
        do {
            decoded = try JSONDecoder().decode(GraphQLAnswer<Answer>.self, from: data)
        } catch {
            throw TeaNodeError.unreadableAnswer
        }
        guard let value = decoded.data else {
            throw TeaNodeError.unreadableAnswer
        }
        return value
    }

    /// Uploads a file to hand the agent with a message, and returns its id.
    public func upload(name: String, contentType: String, data: Data) async throws -> String {
        let boundary = "teanode-\(UUID().uuidString)"
        var request = URLRequest(url: account.server.endpoint("/api/v1/agent/attachments"))
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        request.httpBody = Upload.body(boundary: boundary, name: name, contentType: contentType, data: data)
        let (answer, response) = try await send(authorizing: request)
        guard response.statusCode == 200 else {
            if response.statusCode == 413 { throw TeaNodeError.refused("That file is larger than the server takes.") }
            throw TeaNodeError.refused(String(decoding: answer.prefix(300), as: UTF8.self))
        }
        struct Uploaded: Decodable {
            struct File: Decodable { let id: String }
            let attachments: [File]
        }
        guard let id = try JSONDecoder().decode(Uploaded.self, from: answer).attachments.first?.id else {
            throw TeaNodeError.unreadableAnswer
        }
        return id
    }
}

/// Upload writes a file as the multipart body the attachments endpoint takes.
public enum Upload {
    public static func body(boundary: String, name: String, contentType: String, data: Data) -> Data {
        let safeName = name.replacingOccurrences(of: "\"", with: "'").replacingOccurrences(of: "\r", with: "").replacingOccurrences(of: "\n", with: "")
        var body = Data()
        body.append(Data("--\(boundary)\r\n".utf8))
        body.append(Data("Content-Disposition: form-data; name=\"file\"; filename=\"\(safeName)\"\r\n".utf8))
        body.append(Data("Content-Type: \(contentType)\r\n\r\n".utf8))
        body.append(data)
        body.append(Data("\r\n--\(boundary)--\r\n".utf8))
        return body
    }
}
