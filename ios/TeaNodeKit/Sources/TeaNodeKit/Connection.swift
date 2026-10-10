import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AccountStore keeps an account between launches: the keychain in the app,
/// memory in a test.
public protocol AccountStore: Sendable {
    func save(_ account: Account) throws
    func load() throws -> Account?
    func delete() throws
}

/// Connection is the app talking to its server as the person: GraphQL over
/// HTTPS with the account's token, renewed before it runs out and kept
/// wherever the store keeps it.
public actor Connection {
    public private(set) var account: Account
    let transport: any HTTPTransport
    let store: (any AccountStore)?

    /// renewBefore is how long before a token runs out it is renewed.
    static let renewBefore: TimeInterval = 24 * 60 * 60

    public init(account: Account, transport: any HTTPTransport, store: (any AccountStore)? = nil) {
        self.account = account
        self.transport = transport
        self.store = store
    }

    /// The token to send now, renewed first when it is close to running out.
    public func accessToken(now: Date = Date()) async throws -> String {
        if account.expiresAt.timeIntervalSince(now) < Self.renewBefore, !account.refreshToken.isEmpty {
            account = try await SignIn(server: account.server, transport: transport).refresh(account, now: now)
            try? store?.save(account)
        }
        return account.accessToken
    }

    struct GraphQLAnswer<Answer: Decodable>: Decodable {
        let data: Answer?
        let errors: [GraphQLError]?
    }

    /// Runs a GraphQL document and decodes its data.
    public func execute<Answer: Decodable & Sendable>(_ document: String, variables: [String: JSONValue] = [:], as answer: Answer.Type) async throws -> Answer {
        let token = try await accessToken()
        var request = URLRequest(url: account.server.endpoint("/api/v1/graphql"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.httpBody = try JSONEncoder().encode(["query": JSONValue.string(document), "variables": .object(variables)])
        let (data, response) = try await transport.send(request)
        if response.statusCode == 401 {
            throw TeaNodeError.signedOut
        }
        let decoded: GraphQLAnswer<Answer>
        do {
            decoded = try JSONDecoder().decode(GraphQLAnswer<Answer>.self, from: data)
        } catch {
            throw TeaNodeError.unreadableAnswer
        }
        if let error = decoded.errors?.first {
            throw TeaNodeError.refused(error.message)
        }
        guard let value = decoded.data else {
            throw TeaNodeError.unreadableAnswer
        }
        return value
    }

    /// Uploads a file to hand the agent with a message, and returns its id.
    public func upload(name: String, contentType: String, data: Data) async throws -> String {
        let token = try await accessToken()
        let boundary = "teanode-\(UUID().uuidString)"
        var request = URLRequest(url: account.server.endpoint("/api/v1/agent/attachments"))
        request.httpMethod = "POST"
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.httpBody = Upload.body(boundary: boundary, name: name, contentType: contentType, data: data)
        let (answer, response) = try await transport.send(request)
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
