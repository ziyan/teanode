import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// RecordingRowStatus is the one line a library row says about where a recording is.
public enum RecordingRowStatus: Hashable, Sendable {
    case recording
    case paused
    case uploading(percent: Int)
    case waitingForNetwork
    case transcribing
    case ready
    case failed
}

/// RecordingServerProgress is what the server says about one recording that its list does not: whether the
/// originals arrived and how the transcripts are doing.
public struct RecordingServerProgress: Hashable, Sendable {
    public let isUploadComplete: Bool
    public let uploadedPercent: Int
    public let transcriptProcessingStates: [String]

    public init(isUploadComplete: Bool, uploadedPercent: Int, transcriptProcessingStates: [String]) {
        self.isUploadComplete = isUploadComplete
        self.uploadedPercent = uploadedPercent
        self.transcriptProcessingStates = transcriptProcessingStates
    }

    public init(view: RecordingView) {
        let totalPartCount = view.mediaFiles.reduce(0) { $0 + $1.partCount }
        let isComplete = !view.mediaFiles.isEmpty && view.mediaFiles.allSatisfy { $0.recordingUploadState == "complete" }
        self.init(
            isUploadComplete: isComplete,
            uploadedPercent: isComplete || totalPartCount == 0 ? (isComplete ? 100 : 0) : min(99, view.uploadParts.count * 100 / totalPartCount),
            transcriptProcessingStates: (view.transcriptRuns ?? []).map(\.recordingProcessingState))
    }

    /// Whether the row will change without the person doing anything, so the library keeps asking.
    public var isSettled: Bool {
        isUploadComplete && !transcriptProcessingStates.isEmpty && !transcriptProcessingStates.contains { Self.runningStates.contains($0) }
    }

    static let runningStates: Set<String> = ["queued", "preparing", "transcribing"]
}

/// RecordingLibraryRow is one recording as the library lists it, whether it is only on the phone, only on the
/// server, or both.
public struct RecordingLibraryRow: Identifiable, Hashable, Sendable {
    /// The client recording identifier both sides share, which is what makes the two one row.
    public let id: String
    public let recording: Recording?
    public let localRecording: LocalRecording?
    public let recordingTitle: String
    public let recordingSummaryFirstLine: String
    public let recordedAt: Date
    public let timelineDurationMS: Int64
    public let status: RecordingRowStatus

    public init(
        id: String, recording: Recording? = nil, localRecording: LocalRecording? = nil, recordingTitle: String, recordingSummaryFirstLine: String,
        recordedAt: Date, timelineDurationMS: Int64, status: RecordingRowStatus
    ) {
        self.id = id
        self.recording = recording
        self.localRecording = localRecording
        self.recordingTitle = recordingTitle
        self.recordingSummaryFirstLine = recordingSummaryFirstLine
        self.recordedAt = recordedAt
        self.timelineDurationMS = timelineDurationMS
        self.status = status
    }

    /// A recording that is being captured right now, or is paused, and so opens the capture screen.
    public var isLiveSession: Bool {
        guard let localRecording else { return false }
        return ["active", "paused", "interrupted"].contains(localRecording.recordingCaptureState)
    }

    public var canRemoveFromPhone: Bool {
        guard let localRecording else { return false }
        return ["stopped", "imported"].contains(localRecording.recordingCaptureState)
    }

    public var canRetryUpload: Bool {
        guard let localRecording else { return false }
        return RecordingLibraryProgress.pendingLocalRecordingIds([localRecording]).contains(localRecording.id)
    }

    public var canPlayLocalOriginal: Bool {
        localRecording.map(RecordingLibraryProgress.canPlayLocalOriginal) ?? false
    }

    public var canDelete: Bool { recording != nil }

    /// Whether the row will change by itself soon: an upload or a transcript under way, or a transcript just finished
    /// whose title and summary the server has not written yet. The library keeps asking while any row does.
    public func isStillChanging(now: Date = Date()) -> Bool {
        switch status {
        case .uploading, .transcribing: return true
        case .ready:
            guard let recording, recording.recordingTitleSource != "user" else { return false }
            let isUnnamed = recordingTitle.isEmpty && recordingSummaryFirstLine.isEmpty
            let updatedAt = ServerTime.date(recording.updatedAt) ?? .distantPast
            return isUnnamed && abs(now.timeIntervalSince(updatedAt)) < 600
        case .recording, .paused, .waitingForNetwork, .failed: return false
        }
    }
}

/// RecordingLibrary merges the phone's journal with the server's list into the one list the library shows.
public enum RecordingLibrary {
    public static func rows(
        recordings: [Recording], localRecordings: [LocalRecording], serverProgress: [String: RecordingServerProgress] = [:]
    ) -> [RecordingLibraryRow] {
        let localByClientId = Dictionary(localRecordings.map { ($0.clientRecordingId, $0) }, uniquingKeysWith: { first, _ in first })
        var rows: [RecordingLibraryRow] = recordings.map { recording in
            row(recording: recording, localRecording: localByClientId[recording.clientRecordingId], progress: serverProgress[recording.id])
        }
        let serverClientIds = Set(recordings.map(\.clientRecordingId))
        for local in localRecordings where !serverClientIds.contains(local.clientRecordingId) {
            // A fully uploaded original with no server row is on a page not loaded yet, or deleted there.
            guard !isFullyUploaded(local) else { continue }
            rows.append(row(recording: nil, localRecording: local, progress: nil))
        }
        return rows.sorted { first, second in
            first.recordedAt != second.recordedAt ? first.recordedAt > second.recordedAt : first.id > second.id
        }
    }

    public static func firstLine(of summary: String?) -> String {
        for line in (summary ?? "").split(whereSeparator: \.isNewline) {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if !trimmed.isEmpty { return trimmed }
        }
        return ""
    }

    /// The percent of a phone's originals the server has accepted, by bytes.
    public static func uploadedPercent(_ localRecording: LocalRecording) -> Int {
        let totalBytes = localRecording.localMedia.reduce(Int64(0)) { $0 + max(0, $1.originalSizeBytes) }
        guard totalBytes > 0 else { return 0 }
        let uploadedBytes = localRecording.localMedia.reduce(Int64(0)) { total, media in
            media.isDurablyUploaded ? total + media.originalSizeBytes : total + media.acceptedParts.reduce(Int64(0)) { $0 + $1.partSizeBytes }
        }
        return Int(min(99, max(0, uploadedBytes * 100 / totalBytes)))
    }

    public static func status(
        recording: Recording?, localRecording: LocalRecording?, progress: RecordingServerProgress?
    ) -> RecordingRowStatus {
        if let localRecording {
            switch localRecording.recordingCaptureState {
            case "active": return .recording
            case "paused", "interrupted": return .paused
            default: break
            }
            if localRecording.isDeletedRemotely { return .failed }
            if !isFullyUploaded(localRecording) {
                if localRecording.uploadErrorMessage != nil {
                    return localRecording.isUploadWaitingForNetwork == true ? .waitingForNetwork : .failed
                }
                return .uploading(percent: uploadedPercent(localRecording))
            }
        }
        guard let recording else { return .ready }
        guard let progress else {
            return RecordingLibrary.firstLine(of: recording.recordingSummary).isEmpty ? .transcribing : .ready
        }
        if !progress.isUploadComplete { return .uploading(percent: progress.uploadedPercent) }
        let states = progress.transcriptProcessingStates
        if states.isEmpty || states.contains(where: { RecordingServerProgress.runningStates.contains($0) }) { return .transcribing }
        return states.contains("complete") ? .ready : .failed
    }

    private static func isFullyUploaded(_ localRecording: LocalRecording) -> Bool {
        ["stopped", "imported"].contains(localRecording.recordingCaptureState) && !localRecording.localMedia.isEmpty
            && localRecording.localMedia.allSatisfy(\.isDurablyUploaded)
    }

    private static func row(recording: Recording?, localRecording: LocalRecording?, progress: RecordingServerProgress?) -> RecordingLibraryRow {
        let serverTitle = recording?.recordingTitle ?? ""
        let recordedAt =
            ServerTime.date(recording?.recordedAt) ?? localRecording?.startedAt ?? ServerTime.date(recording?.createdAt) ?? .distantPast
        return RecordingLibraryRow(
            id: recording?.clientRecordingId ?? localRecording?.clientRecordingId ?? "",
            recording: recording, localRecording: localRecording,
            recordingTitle: serverTitle.isEmpty ? (localRecording?.recordingTitle ?? "") : serverTitle,
            recordingSummaryFirstLine: firstLine(of: recording?.recordingSummary), recordedAt: recordedAt,
            timelineDurationMS: max(localRecording?.timelineDurationMS ?? 0, recording?.timelineDurationMS ?? 0),
            status: status(recording: recording, localRecording: localRecording, progress: progress))
    }

    /// Whether an upload failed for want of a connection, which clears by itself, rather than being refused.
    public static func isConnectivityFailure(_ error: Error) -> Bool {
        guard let urlError = error as? URLError else { return false }
        return [
            .notConnectedToInternet, .networkConnectionLost, .timedOut, .cannotFindHost, .cannotConnectToHost, .dnsLookupFailed,
            .dataNotAllowed, .internationalRoamingOff, .secureConnectionFailed,
        ].contains(urlError.code)
    }
}
