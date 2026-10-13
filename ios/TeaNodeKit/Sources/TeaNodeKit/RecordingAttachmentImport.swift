import Foundation

public struct RecordingAttachmentImportDraft: Codable, Hashable, Sendable {
    public let accountKey: String
    public let agentAttachmentId: String
    public let clientRecordingId: String
    public let recordingTitle: String

    public init(accountKey: String, agentAttachmentId: String, recordingTitle: String) {
        self.accountKey = accountKey
        self.agentAttachmentId = agentAttachmentId
        self.recordingTitle = recordingTitle
        clientRecordingId = UUID().uuidString
    }
}

@MainActor
public enum RecordingAttachmentImportDrafts {
    public static func read(accountKey: String, attachmentId: String, in defaults: UserDefaults = .standard) -> RecordingAttachmentImportDraft? {
        guard let encoded = defaults.data(forKey: key(accountKey: accountKey, attachmentId: attachmentId)),
            let draft = try? JSONDecoder().decode(RecordingAttachmentImportDraft.self, from: encoded), draft.accountKey == accountKey,
            draft.agentAttachmentId == attachmentId
        else { return nil }
        return draft
    }
    public static func save(_ draft: RecordingAttachmentImportDraft, in defaults: UserDefaults = .standard) throws {
        defaults.set(try JSONEncoder().encode(draft), forKey: key(accountKey: draft.accountKey, attachmentId: draft.agentAttachmentId))
    }
    public static func finish(_ draft: RecordingAttachmentImportDraft, in defaults: UserDefaults = .standard) {
        if read(accountKey: draft.accountKey, attachmentId: draft.agentAttachmentId, in: defaults)?.clientRecordingId == draft.clientRecordingId {
            defaults.removeObject(forKey: key(accountKey: draft.accountKey, attachmentId: draft.agentAttachmentId))
        }
    }
    private static func key(accountKey: String, attachmentId: String) -> String {
        "recording.attachment-import." + Data(accountKey.utf8).base64EncodedString() + "." + Data(attachmentId.utf8).base64EncodedString()
    }
}

extension Documents {
    public static let importRecordingAttachment = """
        mutation ($agentAttachmentId: String!, $clientRecordingId: String!, $recordingTitle: String!, $recordingMediaId: String) {
        ImportRecordingAttachment(agentAttachmentId: $agentAttachmentId, clientRecordingId: $clientRecordingId, recordingTitle:
        $recordingTitle, recordingMediaId: $recordingMediaId) { recording { \(recordingFields) } mediaFiles { \(recordingMediaFields) }
        uploadParts { recordingMediaId partIndex partSizeBytes partSha256 storedAt } transcriptRuns { id recordingId recordingMediaId
        versionNumber recordingProcessingState providerModelName processingErrorMessage createdAt } } }
        """
}

extension Connection {
    public func importRecordingAttachment(_ attachment: AgentAttachment, draft: RecordingAttachmentImportDraft) async throws -> RecordingView {
        guard accountKey == draft.accountKey, attachment.id == draft.agentAttachmentId else { throw TeaNodeError.signedOut }
        struct Answer: Decodable, Sendable { let ImportRecordingAttachment: RecordingView }
        let view = try await execute(
            Documents.importRecordingAttachment,
            variables: [
                "agentAttachmentId": .string(draft.agentAttachmentId), "clientRecordingId": .string(draft.clientRecordingId),
                "recordingTitle": .string(draft.recordingTitle), "recordingMediaId": .null,
            ], as: Answer.self
        ).ImportRecordingAttachment
        guard view.recording.clientRecordingId == draft.clientRecordingId,
            let original = view.mediaFiles.first(where: { $0.clientMediaId == "attachment-" + attachment.id }),
            original.recordingId == view.recording.id, original.originalFilename == attachment.name,
            original.originalContentType == attachment.contentType, original.originalSizeBytes == Int64(attachment.size),
            original.recordingUploadState == "complete", original.originalSha256.count == 64,
            original.originalSha256.allSatisfy({ "0123456789abcdef".contains($0) }), ServerTime.date(original.verifiedAt ?? "") != nil
        else { throw TeaNodeError.unreadableAnswer }
        return view
    }
}
