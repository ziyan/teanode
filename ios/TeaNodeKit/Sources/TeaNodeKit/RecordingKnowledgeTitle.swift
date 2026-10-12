import Foundation

extension KnowledgeSource {
    public var recordingTitle: String? {
        guard kind == "recording", let title = specification.storedSettings["recordingTitle"]?.stringValue,
            !title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        else { return nil }
        return title
    }
}
