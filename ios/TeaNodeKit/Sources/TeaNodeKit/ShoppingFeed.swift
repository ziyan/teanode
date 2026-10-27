import Foundation

#if canImport(Darwin)
    /// ShoppingFeed streams which shopping list changed: each time a list the
    /// viewer reaches changes (an item added, checked off, a trip started), the
    /// server says which, and the screen reads that list again. It is opened
    /// while a shopping screen is visible.
    ///
    /// Like `ConversationFeed`, the stream ends with an error when the
    /// connection drops, and the screen opens a new one.
    public enum ShoppingFeed {
        public static func events(connection: Connection, session: URLSession = .shared) -> AsyncThrowingStream<ShoppingEvent, Error> {
            AsyncThrowingStream { continuation in
                let task = Task {
                    do {
                        try await stream(connection: connection, session: session) { continuation.yield($0) }
                        continuation.finish()
                    } catch {
                        continuation.finish(throwing: error)
                    }
                }
                continuation.onTermination = { _ in task.cancel() }
            }
        }

        static func stream(connection: Connection, session: URLSession, yield: (ShoppingEvent) -> Void) async throws {
            let token = try await connection.accessToken()
            let server = await connection.account.server
            var components = URLComponents(url: server.endpoint("/api/v1/graphql"), resolvingAgainstBaseURL: false)!
            components.scheme = components.scheme == "http" ? "ws" : "wss"
            let socket = session.webSocketTask(with: components.url!, protocols: [GraphQLWebSocketMessage.subprotocol])
            socket.resume()
            let watched = WatchedSocket(socket)
            let watchdog = Task {
                while !Task.isCancelled {
                    try? await Task.sleep(for: .seconds(5))
                    if watched.silence > ConversationFeed.silenceMost {
                        watched.close()
                        return
                    }
                }
            }
            defer {
                watchdog.cancel()
                watched.close()
            }

            try await socket.send(.string(GraphQLWebSocketMessage.connectionInit(authorization: "Bearer \(token)").encoded()))
            try await socket.send(.string(GraphQLWebSocketMessage.start(id: "1", query: Documents.shoppingEvents, variables: [:]).encoded()))

            while !Task.isCancelled {
                let frame = try await socket.receive()
                watched.heard()
                guard case .string(let text) = frame, let socketMessage = try? GraphQLWebSocketMessage(decoding: text) else {
                    continue
                }
                switch socketMessage {
                case .connectionError(let errorMessage):
                    throw TeaNodeError.refused(errorMessage)
                case .error(_, let errors):
                    throw TeaNodeError.refused(errors.first?.message ?? "The server stopped the shopping feed.")
                case .complete:
                    return
                case .data(_, let eventData, let errors):
                    if let error = errors.first {
                        throw TeaNodeError.refused(error.message)
                    }
                    guard let event = eventData?["ShoppingEvents"] else { continue }
                    yield(try JSONDecoder().decode(ShoppingEvent.self, from: JSONEncoder().encode(event)))
                default:
                    continue
                }
            }
        }
    }
#endif
