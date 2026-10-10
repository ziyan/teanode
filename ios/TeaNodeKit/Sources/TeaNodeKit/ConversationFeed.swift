import Foundation

#if canImport(Darwin)
    /// ConversationFeed streams a conversation's events as they happen: a
    /// turn's words as they are written, its tools, its cards. A turn already
    /// running when the feed opens is replayed from its start, so a screen
    /// that opens mid-turn shows all of it.
    ///
    /// The stream ends with an error when the connection drops; the screen
    /// reloads the conversation and opens a new feed.
    public enum ConversationFeed {
        public static func events(
            connection: Connection, conversationId: String, session: URLSession = .shared
        ) -> AsyncThrowingStream<AgentEvent, Error> {
            AsyncThrowingStream { continuation in
                let task = Task {
                    do {
                        try await stream(connection: connection, conversationId: conversationId, session: session) {
                            continuation.yield($0)
                        }
                        continuation.finish()
                    } catch {
                        continuation.finish(throwing: error)
                    }
                }
                continuation.onTermination = { _ in task.cancel() }
            }
        }

        static func stream(
            connection: Connection, conversationId: String, session: URLSession, yield: (AgentEvent) -> Void
        ) async throws {
            let token = try await connection.accessToken()
            let server = await connection.account.server
            var components = URLComponents(url: server.endpoint("/api/v1/graphql"), resolvingAgainstBaseURL: false)!
            components.scheme = components.scheme == "http" ? "ws" : "wss"
            let socket = session.webSocketTask(with: components.url!, protocols: [GraphQLWebSocketMessage.subprotocol])
            socket.resume()
            defer { socket.cancel(with: .goingAway, reason: nil) }

            try await socket.send(.string(GraphQLWebSocketMessage.connectionInit(authorization: "Bearer \(token)").encoded()))
            try await socket.send(.string(GraphQLWebSocketMessage.start(
                id: "1", query: Documents.conversationEvents, variables: ["conversationId": .string(conversationId)]
            ).encoded()))

            while !Task.isCancelled {
                let frame = try await socket.receive()
                guard case .string(let text) = frame, let message = try? GraphQLWebSocketMessage(decoding: text) else {
                    continue
                }
                switch message {
                case .connectionError(let reason):
                    throw TeaNodeError.refused(reason)
                case .error(_, let errors):
                    throw TeaNodeError.refused(errors.first?.message ?? "The server stopped the conversation's feed.")
                case .complete:
                    return
                case .data(_, let data, let errors):
                    if let error = errors.first {
                        throw TeaNodeError.refused(error.message)
                    }
                    guard let event = data?["AgentConversationEvents"] else { continue }
                    yield(try JSONDecoder().decode(AgentEvent.self, from: JSONEncoder().encode(event)))
                default:
                    continue
                }
            }
        }
    }
#endif
