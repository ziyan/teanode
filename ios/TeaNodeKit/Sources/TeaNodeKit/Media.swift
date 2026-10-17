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
    /// For a picture the photo tool showed: the photo it is, which the app
    /// opens in its photo viewer. Nil for every other file.
    public let photoId: String?

    public var isArtifact: Bool { !artifactKind.isEmpty }
    public var isImage: Bool { contentType.hasPrefix("image/") }
    public var isVideo: Bool { contentType.hasPrefix("video/") }
    public var isAudio: Bool { contentType.hasPrefix("audio/") }

    /// The address it is read from on the server.
    public var path: String { "/api/v1/agent/attachments/\(attachmentId)" }

    /// The files a tool's result hands the person. A result names its file
    /// by the address it is served from on this server; one naming any other
    /// address is not a file of this conversation and is never fetched. The
    /// photo of a file is read only from the photo tool's result, and only
    /// when it is shaped like an id of the server's: any other tool's answer
    /// is somebody else's text.
    public static func of(toolResult: String, tool: String = "") -> [AgentFile] {
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
                    size: 0, caption: "", artifactKind: kind, photoId: nil)
            }
            guard let attachmentId = entry["attachment_id"]?.stringValue, let address = entry["url"]?.stringValue,
                address == "/api/v1/agent/attachments/\(attachmentId)"
            else { return nil }
            var size = 0
            if case .number(let number)? = entry["size"] { size = Int(number) }
            return AgentFile(
                attachmentId: attachmentId, name: entry["name"]?.stringValue ?? "", contentType: entry["content_type"]?.stringValue ?? "",
                size: size, caption: entry["caption"]?.stringValue ?? "", artifactKind: "",
                photoId: tool == "photo" ? entry["photo_id"]?.stringValue.flatMap(Self.photoId(from:)) : nil)
        }
    }
}

extension AgentFile {
    /// The id when it is shaped like a photo's (letters, digits, "-" and
    /// "_", up to 64), and nil otherwise.
    static func photoId(from text: String) -> String? {
        guard (1...64).contains(text.utf8.count), text.utf8.allSatisfy({ byte in
            (48...57).contains(byte) || (65...90).contains(byte) || (97...122).contains(byte) || byte == 45 || byte == 95
        }) else { return nil }
        return text
    }
}

extension Connection {
    /// Reads a file the server keeps for this account: an attachment, a
    /// picture an answer shows.
    public func download(_ path: String) async throws -> Data {
        let (data, response) = try await send(authorizing: URLRequest(url: account.server.endpoint(path)))
        guard response.statusCode == 200 else {
            throw TeaNodeError.refused("The file could not be read (\(response.statusCode)).")
        }
        return data
    }

    /// An address for an attachment that opens without signing in, for a
    /// few hours: for a player or a web view that cannot send the token.
    public func shareAddress(attachmentId: String) async throws -> URL {
        struct Answer: Decodable, Sendable { let ShareAgentAttachment: String }
        let path = try await execute(Documents.shareAttachment, variables: ["attachmentId": .string(attachmentId)], as: Answer.self).ShareAgentAttachment
        return try absoluteAddress(of: path)
    }

    /// A path the server answered with, as an address on that server.
    func absoluteAddress(of path: String) throws -> URL {
        guard let address = URL(string: path, relativeTo: account.server.endpoint("/")) else {
            throw TeaNodeError.unreadableAnswer
        }
        return address.absoluteURL
    }

    /// Reads what a signed address serves, which needs no sign-in, into a
    /// file named fileName in a folder of its own, with a download task so
    /// that the bytes never sit in memory. The caller removes the folder.
    func downloadFile(from address: URL, fileName: String) async throws -> URL {
        let (temporaryURL, response) = try await transport.download(URLRequest(url: address))
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        do {
            guard response.statusCode == 200 else {
                // "No such file for you" is told from a server that failed, so
                // that only the first lets a preview stand in for the file.
                throw [403, 404, 410].contains(response.statusCode) ? PhotoError.fileNotAvailable : PhotoError.fileCouldNotBeRead
            }
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            let safeName = fileName.isEmpty ? "photo" : fileName.replacingOccurrences(of: "/", with: "-")
            let fileURL = folder.appendingPathComponent(safeName)
            try FileManager.default.moveItem(at: temporaryURL, to: fileURL)
            return fileURL
        } catch {
            try? FileManager.default.removeItem(at: temporaryURL)
            try? FileManager.default.removeItem(at: folder)
            throw error
        }
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
