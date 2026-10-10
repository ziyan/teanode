import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AgentFile is a file the agent handed the person: one a tool shared (the
/// share_file tool, or a skill's files), or an artifact it made.
public struct AgentFile: Hashable, Sendable {
    public let attachmentId: String
    public let name: String
    public let contentType: String
    public let size: Int
    public let caption: String
    /// For an artifact: "html", "svg" or "markdown"; empty for a file.
    public let artifactKind: String

    public var isArtifact: Bool { !artifactKind.isEmpty }
    public var isImage: Bool { contentType.hasPrefix("image/") }
    public var isVideo: Bool { contentType.hasPrefix("video/") }
    public var isAudio: Bool { contentType.hasPrefix("audio/") }

    /// The address it is read from on the server.
    public var path: String { "/api/v1/agent/attachments/\(attachmentId)" }

    /// The files a tool's result hands the person. A result names its file
    /// by the address it is served from on this server; one naming any other
    /// address is not a file of this conversation and is never fetched.
    public static func of(toolResult: String) -> [AgentFile] {
        var text = toolResult.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.hasPrefix("<untrusted-data>"), text.hasSuffix("</untrusted-data>") {
            text = String(text.dropFirst("<untrusted-data>".count).dropLast("</untrusted-data>".count))
        }
        guard let value = try? JSONDecoder().decode(JSONValue.self, from: Data(text.utf8)) else { return [] }
        var entries: [JSONValue] = [value]
        if case .array(let files)? = value["files"] {
            entries = files
        }
        return entries.compactMap { entry in
            if let artifactId = entry["artifact_id"]?.stringValue, let address = entry["url"]?.stringValue,
                address == "/api/v1/agent/attachments/\(artifactId)"
            {
                let kind = entry["kind"]?.stringValue ?? "html"
                return AgentFile(
                    attachmentId: artifactId, name: entry["title"]?.stringValue ?? "", contentType: kind == "markdown" ? "text/markdown" : "text/html",
                    size: 0, caption: "", artifactKind: kind)
            }
            guard let attachmentId = entry["attachment_id"]?.stringValue, let address = entry["url"]?.stringValue,
                address == "/api/v1/agent/attachments/\(attachmentId)"
            else { return nil }
            var size = 0
            if case .number(let number)? = entry["size"] { size = Int(number) }
            return AgentFile(
                attachmentId: attachmentId, name: entry["name"]?.stringValue ?? "", contentType: entry["content_type"]?.stringValue ?? "",
                size: size, caption: entry["caption"]?.stringValue ?? "", artifactKind: "")
        }
    }
}

extension Connection {
    /// Reads a file the server keeps for this account: an attachment, a
    /// picture an answer shows.
    public func download(_ path: String) async throws -> Data {
        let token = try await accessToken()
        var request = URLRequest(url: account.server.endpoint(path))
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, response) = try await transport.send(request)
        guard response.statusCode == 200 else {
            if response.statusCode == 401 { throw TeaNodeError.signedOut }
            throw TeaNodeError.refused("The file could not be read (\(response.statusCode)).")
        }
        return data
    }

    /// An address for an attachment that opens without signing in, for a
    /// few hours: for a player or a web view that cannot send the token.
    public func shareAddress(attachmentId: String) async throws -> URL {
        struct Answer: Decodable, Sendable { let ShareAgentAttachment: String }
        let path = try await execute(Documents.shareAttachment, variables: ["attachmentId": .string(attachmentId)], as: Answer.self).ShareAgentAttachment
        guard let address = URL(string: path, relativeTo: account.server.endpoint("/")) else {
            throw TeaNodeError.unreadableAnswer
        }
        return address.absoluteURL
    }

    /// Where a picture an answer shows is read from: through the server,
    /// which fetches only addresses a tool showed the agent in this
    /// conversation, so the phone never loads what a model wrote.
    public nonisolated static func picturePath(conversationId: String, address: String) -> String {
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "-._~")
        let encoded = address.addingPercentEncoding(withAllowedCharacters: allowed) ?? address
        return "/api/v1/agent/conversations/\(conversationId)/picture?url=\(encoded)"
    }
}
