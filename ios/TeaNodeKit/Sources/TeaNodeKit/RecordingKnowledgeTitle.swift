import Foundation

extension KnowledgeSource {
    public var recordingTitle: String? {
        guard kind == "recording", let title = specification.storedSettings["recordingTitle"]?.stringValue,
            !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        else { return nil }
        return title
    }
}

extension Connection {
    public func saveRecordingSourceSchedule(_ source: KnowledgeSource, cron: String) async throws {
        guard source.kind == "recording" else { throw TeaNodeError.unreadableAnswer }
        _ = try await saveKnowledgeSource(
            KnowledgeSourceChange(sourceId: source.id, name: source.name, cron: cron))
    }
}
