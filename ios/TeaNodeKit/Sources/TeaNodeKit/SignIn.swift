import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// The app's own return address, which it registers with every server. A
/// connect code needs no return; signing in with a password comes back here
/// from the server's sign-in page. It is what marks the app, to the server,
/// as an app installed on a device (internal/api/v1api/apioauth/connect.go).
public let appRedirectAddress = "com.teanode.app:/oauth"

/// The grant a connect code is exchanged with.
let connectCodeGrant = "urn:teanode:params:oauth:grant-type:connect-code"

/// Account is one server the app is connected to, and its sign-in there.
public struct Account: Codable, Equatable, Sendable {
    public let server: ServerAddress
    public let clientId: String
    public var accessToken: String
    public var refreshToken: String
    public var expiresAt: Date
    public var username: String

    public init(server: ServerAddress, clientId: String, accessToken: String, refreshToken: String, expiresAt: Date, username: String) {
        self.server = server
        self.clientId = clientId
        self.accessToken = accessToken
        self.refreshToken = refreshToken
        self.expiresAt = expiresAt
        self.username = username
    }
}

/// SignIn connects the app to a server: it registers the app, exchanges a
/// connect code, refreshes and gives up a sign-in.
public struct SignIn: Sendable {
    let server: ServerAddress
    let transport: any HTTPTransport

    public init(server: ServerAddress, transport: any HTTPTransport) {
        self.server = server
        self.transport = transport
    }

    struct TokenResponse: Decodable {
        let accessToken: String
        let refreshToken: String?
        let expiresIn: Double?

        enum CodingKeys: String, CodingKey {
            case accessToken = "access_token"
            case refreshToken = "refresh_token"
            case expiresIn = "expires_in"
        }
    }

    struct OAuthRefusal: Decodable {
        let error: String
        let errorDescription: String?

        enum CodingKeys: String, CodingKey {
            case error
            case errorDescription = "error_description"
        }
    }

    /// Registers the app with the server and returns its client identifier.
    public func register(clientName: String = "TeaNode") async throws -> String {
        var request = URLRequest(url: server.endpoint("/oauth/register"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(["client_name": JSONValue.string(clientName), "redirect_uris": .array([.string(appRedirectAddress)])])
        let (data, response) = try await transport.send(request)
        guard response.statusCode == 201 || response.statusCode == 200 else {
            throw Self.refusal(data)
        }
        struct Registered: Decodable {
            let clientId: String
            enum CodingKeys: String, CodingKey { case clientId = "client_id" }
        }
        return try JSONDecoder().decode(Registered.self, from: data).clientId
    }

    /// Exchanges a connect code for the app's sign-in. The username is not
    /// known yet; the caller asks the server who the token belongs to.
    public func exchange(connectCode: String, clientId: String, deviceName: String, now: Date = Date()) async throws -> Account {
        let token = try await post(form: [
            "grant_type": connectCodeGrant, "client_id": clientId, "connect_code": connectCode, "device_name": deviceName,
        ], invalidGrant: .connectCodeSpent)
        return Account(
            server: server, clientId: clientId, accessToken: token.accessToken, refreshToken: token.refreshToken ?? "",
            expiresAt: now.addingTimeInterval(token.expiresIn ?? 3600), username: "")
    }

    /// The server's page where the person signs in and allows this app the
    /// whole account; it returns to appRedirectAddress with a code. The API
    /// is named as the resource, which is what asks for the whole account
    /// rather than the agent tools (docs/decisions/20261011-an-installed-app-may-sign-in-with-a-password.md).
    public func authorizationAddress(clientId: String, codeChallenge: String, state: String) -> URL {
        var components = URLComponents(url: server.endpoint("/oauth/authorize"), resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "response_type", value: "code"),
            URLQueryItem(name: "client_id", value: clientId),
            URLQueryItem(name: "redirect_uri", value: appRedirectAddress),
            URLQueryItem(name: "code_challenge", value: codeChallenge),
            URLQueryItem(name: "code_challenge_method", value: "S256"),
            URLQueryItem(name: "state", value: state),
            URLQueryItem(name: "resource", value: server.endpoint("/api/v1").absoluteString),
        ]
        return components.url!
    }

    /// Exchanges the code the sign-in page returned for the app's sign-in,
    /// with the verifier whose hash the page was given.
    public func exchange(authorizationCode: String, codeVerifier: String, clientId: String, now: Date = Date()) async throws -> Account {
        let token = try await post(form: [
            "grant_type": "authorization_code", "client_id": clientId, "code": authorizationCode, "code_verifier": codeVerifier,
            "redirect_uri": appRedirectAddress,
        ], invalidGrant: .refused("The sign-in expired before it finished. Try again."))
        return Account(
            server: server, clientId: clientId, accessToken: token.accessToken, refreshToken: token.refreshToken ?? "",
            expiresAt: now.addingTimeInterval(token.expiresIn ?? 3600), username: "")
    }

    /// The code a return to appRedirectAddress carries, when it answers the
    /// request made with `state`; nil for anything else.
    public static func authorizationCode(in returned: URL, state: String) -> String? {
        let items = URLComponents(url: returned, resolvingAgainstBaseURL: false)?.queryItems ?? []
        guard items.first(where: { $0.name == "state" })?.value == state else { return nil }
        return items.first(where: { $0.name == "code" })?.value
    }

    /// Renews an account's sign-in. The old refresh token stops working.
    public func refresh(_ account: Account, now: Date = Date()) async throws -> Account {
        let token = try await post(form: [
            "grant_type": "refresh_token", "client_id": account.clientId, "refresh_token": account.refreshToken,
        ], invalidGrant: .signedOut)
        var renewed = account
        renewed.accessToken = token.accessToken
        renewed.refreshToken = token.refreshToken ?? account.refreshToken
        renewed.expiresAt = now.addingTimeInterval(token.expiresIn ?? 3600)
        return renewed
    }

    /// Gives up a sign-in, for a person who chose not to connect after all,
    /// or who signs the phone out.
    public func revoke(_ account: Account) async {
        var request = URLRequest(url: server.endpoint("/oauth/revoke"))
        request.httpMethod = "POST"
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        request.httpBody = Self.formBody(["token": account.refreshToken.isEmpty ? account.accessToken : account.refreshToken])
        _ = try? await transport.send(request)
    }

    func post(form: [String: String], invalidGrant: TeaNodeError) async throws -> TokenResponse {
        var request = URLRequest(url: server.endpoint("/oauth/token"))
        request.httpMethod = "POST"
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        request.httpBody = Self.formBody(form)
        let (data, response) = try await transport.send(request)
        guard response.statusCode == 200 else {
            if let refusal = try? JSONDecoder().decode(OAuthRefusal.self, from: data), refusal.error == "invalid_grant" {
                throw invalidGrant
            }
            throw Self.refusal(data)
        }
        return try JSONDecoder().decode(TokenResponse.self, from: data)
    }

    static func refusal(_ data: Data) -> TeaNodeError {
        if let refusal = try? JSONDecoder().decode(OAuthRefusal.self, from: data) {
            return .refused(refusal.errorDescription ?? refusal.error)
        }
        return .unreadableAnswer
    }

    /// The fields form-encoded, in a stable order.
    static func formBody(_ form: [String: String]) -> Data {
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "-._~")
        let pairs = form.sorted { $0.key < $1.key }.map { key, value in
            "\(key.addingPercentEncoding(withAllowedCharacters: allowed) ?? key)=\(value.addingPercentEncoding(withAllowedCharacters: allowed) ?? value)"
        }
        return Data(pairs.joined(separator: "&").utf8)
    }
}
