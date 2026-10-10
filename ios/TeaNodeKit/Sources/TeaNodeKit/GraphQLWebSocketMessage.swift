import Foundation

/// GraphQLWebSocketMessage is one message of the websocket protocol the
/// server speaks at `/api/v1/graphql` for subscriptions: the `graphql-ws`
/// subprotocol, the older Apollo one, which the dashboard speaks too
/// (`internal/api/v1api/apigraph/websocket.go`).
///
/// The client opens with `connection_init`, carrying its token, since a
/// program has no dashboard session; the server answers `connection_ack`
/// and then `ka` every second. Each subscription is a `start` with an id,
/// answered by `data` messages with that id until `complete`; `stop` ends
/// one early.
public enum GraphQLWebSocketMessage: Equatable, Sendable {
    // From the app.
    case connectionInit(authorization: String)
    case start(id: String, query: String, variables: [String: JSONValue])
    case stop(id: String)
    case connectionTerminate

    // From the server.
    case connectionAck
    case connectionError(message: String)
    case keepAlive
    case data(id: String, data: JSONValue?, errors: [GraphQLError])
    case error(id: String, errors: [GraphQLError])
    case complete(id: String)

    /// The subprotocol to ask for when opening the websocket.
    public static let subprotocol = "graphql-ws"
}

/// GraphQLError is one error in a GraphQL answer.
public struct GraphQLError: Equatable, Sendable, Codable {
    public let message: String

    public init(message: String) {
        self.message = message
    }
}

extension GraphQLWebSocketMessage {
    private struct Envelope: Codable {
        var type: String
        var id: String?
        var payload: JSONValue?
    }

    /// The message as the text frame sent on the websocket.
    public func encoded() throws -> String {
        let envelope: Envelope
        switch self {
        case .connectionInit(let authorization):
            envelope = Envelope(type: "connection_init", payload: .object(["Authorization": .string(authorization)]))
        case .start(let id, let query, let variables):
            envelope = Envelope(
                type: "start", id: id, payload: .object(["query": .string(query), "variables": .object(variables)]))
        case .stop(let id):
            envelope = Envelope(type: "stop", id: id)
        case .connectionTerminate:
            envelope = Envelope(type: "connection_terminate")
        case .connectionAck:
            envelope = Envelope(type: "connection_ack")
        case .connectionError(let message):
            envelope = Envelope(type: "connection_error", payload: .object(["message": .string(message)]))
        case .keepAlive:
            envelope = Envelope(type: "ka")
        case .data(let id, let data, let errors):
            var payload: [String: JSONValue] = ["data": data ?? .null]
            if !errors.isEmpty {
                payload["errors"] = .array(errors.map { .object(["message": .string($0.message)]) })
            }
            envelope = Envelope(type: "data", id: id, payload: .object(payload))
        case .error(let id, let errors):
            envelope = Envelope(
                type: "error", id: id, payload: .array(errors.map { .object(["message": .string($0.message)]) }))
        case .complete(let id):
            envelope = Envelope(type: "complete", id: id)
        }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return String(decoding: try encoder.encode(envelope), as: UTF8.self)
    }

    /// Reads a text frame the server sent.
    public init(decoding text: String) throws {
        let envelope = try JSONDecoder().decode(Envelope.self, from: Data(text.utf8))
        switch envelope.type {
        case "connection_ack":
            self = .connectionAck
        case "connection_error":
            self = .connectionError(message: envelope.payload?["message"]?.stringValue ?? "the server refused the connection")
        case "ka":
            self = .keepAlive
        case "data":
            self = .data(
                id: try Self.id(of: envelope), data: envelope.payload?["data"],
                errors: Self.errors(in: envelope.payload?["errors"]))
        case "error":
            self = .error(id: try Self.id(of: envelope), errors: Self.errors(in: envelope.payload))
        case "complete":
            self = .complete(id: try Self.id(of: envelope))
        default:
            throw GraphQLWebSocketMessageError.isUnknown(type: envelope.type)
        }
    }

    private static func id(of envelope: Envelope) throws -> String {
        guard let id = envelope.id else {
            throw GraphQLWebSocketMessageError.hasNoID(type: envelope.type)
        }
        return id
    }

    /// The errors in a payload: a list of them, or one alone.
    private static func errors(in payload: JSONValue?) -> [GraphQLError] {
        switch payload {
        case .array(let errors):
            return errors.map { GraphQLError(message: $0["message"]?.stringValue ?? "unknown error") }
        case .object:
            return [GraphQLError(message: payload?["message"]?.stringValue ?? "unknown error")]
        default:
            return []
        }
    }
}

/// GraphQLWebSocketMessageError is why a frame from the server could not be
/// read.
public enum GraphQLWebSocketMessageError: Error, Equatable, Sendable {
    case isUnknown(type: String)
    case hasNoID(type: String)
}
