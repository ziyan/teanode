import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif
#if canImport(CryptoKit)
    import CryptoKit
#endif

// Notifications reach the phone through the push relay (internal/push on the
// server). The phone registers its device token with the relay and hands the
// sealed registration to its own server, with a key of its own making; the
// server encrypts each notification's content with that key, and the app's
// notification service extension decrypts it before iOS shows anything.

/// NotificationContent is what a notification says, as the server encrypts
/// it for this phone (`push.Content` in internal/push/payload.go).
public struct NotificationContent: Codable, Equatable, Sendable {
    /// alert, herdr_question, goal_needs_you, speak_first, message,
    /// approval, question, answer or test.
    public let notificationKind: String
    public let conversationId: String?
    public let runId: String?
    public let callId: String?
    public let title: String
    public let body: String
    public let buttons: [NotificationButton]?
    public let threadId: String?
}

/// NotificationButton is one action on a notification.
public struct NotificationButton: Codable, Equatable, Sendable {
    public let label: String
    /// approve, deny, answer:<option number>, reply or open.
    public let buttonAction: String
}

/// PayloadKey is the key an account's notifications are encrypted with:
/// 256 random bits, made on the phone and given only to that account's
/// server. Each account has its own, so the key that opens a notification
/// also says which account sent it.
public enum PayloadKey {
    #if canImport(CryptoKit)
        /// A new key, as base64.
        public static func make() -> String {
            SymmetricKey(size: .bits256).withUnsafeBytes { Data($0).base64EncodedString() }
        }

        /// Decrypts a notification's sealed content: AES-256-GCM, the nonce in
        /// front and the tag behind, which is CryptoKit's combined form.
        public static func open(_ sealed: String, key: String) throws -> NotificationContent {
            guard let keyData = Data(base64Encoded: key), keyData.count == 32,
                let combined = Data(base64Encoded: sealed)
            else {
                throw TeaNodeError.unreadableAnswer
            }
            let plain = try AES.GCM.open(AES.GCM.SealedBox(combined: combined), using: SymmetricKey(data: keyData))
            return try JSONDecoder().decode(NotificationContent.self, from: plain)
        }

        /// Decrypts a notification with each account's key in turn, and
        /// says whose key opened it: AES-GCM refuses every other key, so
        /// that is the account the notification came from.
        public static func open(_ sealed: String, keys: [AccountPayloadKey]) -> (accountKey: String, content: NotificationContent)? {
            for key in keys {
                if let content = try? open(sealed, key: key.payloadKey) {
                    return (key.accountKey, content)
                }
            }
            return nil
        }
    #endif
}

/// AccountPayloadKey is one account's payload key.
public struct AccountPayloadKey: Equatable, Sendable {
    public let accountKey: String
    public let payloadKey: String

    public init(accountKey: String, payloadKey: String) {
        self.accountKey = accountKey
        self.payloadKey = payloadKey
    }
}

/// SecretStore keeps named secrets: the keychain in the app, memory in a
/// test.
public protocol SecretStore: Sendable {
    func read(_ name: String) throws -> Data?
    func write(_ data: Data, as name: String) throws
    func delete(_ name: String) throws
}

/// PayloadKeyring keeps a payload key for each account on the phone, in a
/// store the app shares with its notification extension.
public struct PayloadKeyring: Sendable {
    /// Where the one key every server was given was kept, before each
    /// account had its own.
    static let singleName = "payloadKey"

    let secrets: any SecretStore

    public init(secrets: any SecretStore) {
        self.secrets = secrets
    }

    static func name(for accountKey: String) -> String {
        "\(singleName)|\(accountKey)"
    }

    /// The account's key, made with `make` the first time it is asked for.
    /// It is kept for as long as the account is on the phone: a new one
    /// would leave every notification already on its way unreadable.
    public func current(for accountKey: String, make: () -> String) throws -> String {
        if let saved = try stored(for: accountKey) {
            return saved
        }
        let made = make()
        try secrets.write(Data(made.utf8), as: Self.name(for: accountKey))
        return made
    }

    /// The account's key if one was made, for the notification extension,
    /// which never makes one.
    public func stored(for accountKey: String) throws -> String? {
        try secrets.read(Self.name(for: accountKey)).map { String(decoding: $0, as: UTF8.self) }
    }

    /// Forgets an account's key, when the phone signs out of it.
    public func forget(for accountKey: String) throws {
        try secrets.delete(Self.name(for: accountKey))
    }

    /// Gives the key made before each account had its own to the account
    /// in use, which is the one that registered it with its server last.
    /// It is given once and then no longer kept on its own; with no account
    /// on the phone it is left where it is.
    public func moveSingleKey(to accountKey: String?) throws {
        guard let accountKey, let single = try secrets.read(Self.singleName) else { return }
        if try secrets.read(Self.name(for: accountKey)) == nil {
            try secrets.write(single, as: Self.name(for: accountKey))
        }
        try secrets.delete(Self.singleName)
    }

    /// The key of every account kept, in the order they are kept, after the
    /// single key has gone to the account in use.
    public func keys(for ring: AccountRing) throws -> [AccountPayloadKey] {
        try moveSingleKey(to: ring.active?.accountKey)
        return try ring.accounts.compactMap { account in
            try stored(for: account.accountKey).map { AccountPayloadKey(accountKey: account.accountKey, payloadKey: $0) }
        }
    }
}

/// PushRelay is the relay the published app's notifications go through.
public struct PushRelay: Sendable {
    /// The relay for the app as published. Every TeaNode server sends
    /// through it unless its operator names another.
    public static let published = try! ServerAddress(parsing: "https://mail.teanode.com")

    let relay: ServerAddress
    let transport: any HTTPTransport

    public init(relay: ServerAddress = PushRelay.published, transport: any HTTPTransport) {
        self.relay = relay
        self.transport = transport
    }

    /// Registers the phone's device token, or a Live Activity's push token
    /// with pushType "liveactivity", and returns the sealed registration to
    /// hand the phone's own server. isSandbox is for a build installed from
    /// Xcode, which Apple's sandbox delivers to.
    public func register(deviceToken: Data, isSandbox: Bool, pushType: String = "alert") async throws -> String {
        var request = URLRequest(url: relay.endpoint("/api/v1/push/registrations"))
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        let hex = deviceToken.map { String(format: "%02x", $0) }.joined()
        request.httpBody = try JSONEncoder().encode(["deviceToken": JSONValue.string(hex), "isSandbox": .bool(isSandbox), "pushType": .string(pushType)])
        let (data, response) = try await transport.send(request)
        struct Registered: Decodable { let pushRegistration: String }
        struct Refused: Decodable { let errorMessage: String }
        guard response.statusCode == 200 else {
            let reason = (try? JSONDecoder().decode(Refused.self, from: data))?.errorMessage
            throw TeaNodeError.refused(reason ?? "The notification relay refused this phone.")
        }
        guard let registered = try? JSONDecoder().decode(Registered.self, from: data) else {
            throw TeaNodeError.unreadableAnswer
        }
        return registered.pushRegistration
    }
}
