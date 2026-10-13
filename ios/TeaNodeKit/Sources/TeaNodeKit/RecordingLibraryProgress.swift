import Foundation

/// RecordingLibraryProgress applies upload results only to the same verified account owner.
public enum RecordingLibraryProgress {
    public static func isCurrentOwner(
        accountKey: String, recordingUserId: String, currentAccountKey: String?, currentRecordingUserId: String?, isCancelled: Bool = false
    ) -> Bool {
        !isCancelled && accountKey == currentAccountKey && recordingUserId == currentRecordingUserId
    }

    public static func canPlayLocalOriginal(_ recording: LocalRecording) -> Bool {
        ["stopped", "imported"].contains(recording.recordingCaptureState)
            && recording.localMedia.contains { !$0.isDurablyUploaded && $0.originalSha256 != nil }
    }

    public static func pendingLocalRecordingIds(_ recordings: [LocalRecording]) -> Set<UUID> {
        Set(
            recordings.filter {
                ["stopped", "imported"].contains($0.recordingCaptureState) && !$0.isDeletedRemotely
                    && $0.localMedia.contains { !$0.isDurablyUploaded }
            }.map(\.id))
    }

    public static func mergingPublishedRecording(
        _ published: Recording, into existing: [Recording], from local: LocalRecording,
        accountKey: String, recordingUserId: String, currentAccountKey: String?, currentRecordingUserId: String?,
        hasMoreRecordings: Bool = true, isCancelled: Bool = false
    ) -> [Recording] {
        guard
            isCurrentOwner(
                accountKey: accountKey, recordingUserId: recordingUserId,
                currentAccountKey: currentAccountKey, currentRecordingUserId: currentRecordingUserId, isCancelled: isCancelled),
            local.belongsTo(accountKey: accountKey, recordingUserId: recordingUserId),
            published.userId == recordingUserId, local.recordingId == published.id,
            published.clientRecordingId == local.clientRecordingId,
            !existing.contains(where: { $0.id == published.id })
        else { return existing }
        if hasMoreRecordings, let last = existing.last, !isBefore(published, last) { return existing }
        var merged = existing
        let insertionIndex = existing.firstIndex { isBefore(published, $0) } ?? existing.count
        merged.insert(published, at: insertionIndex)
        return merged
    }

    private static func isBefore(_ firstRecording: Recording, _ secondRecording: Recording) -> Bool {
        firstRecording.id > secondRecording.id
    }
}
