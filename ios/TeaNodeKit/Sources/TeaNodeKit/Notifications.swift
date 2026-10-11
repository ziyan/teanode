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

/// PayloadKey is the key this phone's notifications are encrypted with:
/// 256 random bits, made on the phone and given only to its own server.
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
    #endif
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
