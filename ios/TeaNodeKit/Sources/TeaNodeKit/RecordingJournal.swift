import Foundation

/// LocalRecording is a recoverable local capture or import. Credentials never enter its journal.
public struct LocalRecording: Codable, Hashable, Sendable, Identifiable {
    public let id: UUID
    public let accountKey: String
    public let recordingUserId: String?
    public var clientRecordingId: String
    public var recordingId: String?
    public var recordingTitle: String
    public var recordingOrigin: String
    public let startedAt: Date
    public var recordingCaptureState: String
    public var timelineDurationMS: Int64
    public var captureGaps: [RecordingGap]
    public var localMedia: [LocalRecordingMedia]
    public var uploadErrorMessage: String?
    public var isDeletedRemotely: Bool
    public var hasPublishedCapture: Bool? = nil
    /// Whether the last upload attempt failed for want of a connection, which clears by itself, rather than being refused.
    public var isUploadWaitingForNetwork: Bool? = nil

    public init(
        accountKey: String, recordingTitle: String, recordingOrigin: String, recordingUserId: String? = nil, startedAt: Date = Date(),
        localRecordingId: UUID = UUID()
    ) {
        id = localRecordingId
        self.accountKey = accountKey
        self.recordingUserId = recordingUserId
        clientRecordingId = id.uuidString
        self.recordingTitle = recordingTitle
        self.recordingOrigin = recordingOrigin
        self.startedAt = startedAt
        recordingCaptureState = recordingOrigin == "capture" ? "active" : "imported"
        timelineDurationMS = 0
        captureGaps = []
        localMedia = []
        isDeletedRemotely = false
    }

    public mutating func pause(at timelineMS: Int64, gapReason: String = "paused") throws {
        guard recordingOrigin == "capture", recordingCaptureState == "active", timelineMS >= timelineDurationMS,
            ["paused", "interrupted"].contains(gapReason)
        else {
            throw TeaNodeError.refused("This recording is not active.")
        }
        timelineDurationMS = timelineMS
        recordingCaptureState = gapReason == "paused" ? "paused" : "interrupted"
        captureGaps.append(RecordingGap(gapStartMS: timelineMS, recordingGapReason: gapReason))
    }

    public mutating func resume(at timelineMS: Int64) throws {
        guard ["paused", "interrupted"].contains(recordingCaptureState), timelineMS >= timelineDurationMS,
            let gapIndex = captureGaps.indices.last, captureGaps[gapIndex].gapEndMS == nil
        else {
            throw TeaNodeError.refused("This recording cannot be resumed.")
        }
        captureGaps[gapIndex].gapEndMS = timelineMS
        timelineDurationMS = timelineMS
        recordingCaptureState = "active"
    }

    public mutating func stop(at timelineMS: Int64) throws {
        guard recordingOrigin == "capture", recordingCaptureState != "stopped", timelineMS >= timelineDurationMS else {
            throw TeaNodeError.refused("This recording cannot be stopped.")
        }
        if let gapIndex = captureGaps.indices.last, captureGaps[gapIndex].gapEndMS == nil {
            captureGaps[gapIndex].gapEndMS = timelineMS
        }
        timelineDurationMS = timelineMS
        recordingCaptureState = "stopped"
    }
}

extension LocalRecording {
    /// Whether this is a capture that can still gain audio: pieces of it are closed one by one while it runs.
    public var isCaptureUnderWay: Bool {
        recordingOrigin == "capture" && ["active", "paused", "interrupted"].contains(recordingCaptureState)
    }

    /// The gaps as the server takes them: one still open (paused or interrupted now) ends at the duration so far, and the
    /// server lets the end move on at the next report.
    public var publishedCaptureGaps: [RecordingGap] {
        captureGaps.map { gap in
            var published = gap
            if published.gapEndMS == nil { published.gapEndMS = max(timelineDurationMS, gap.gapStartMS) }
            return published
        }
    }

    /// Two writers share a capture's manifest: the recorder, which owns the state, the duration, the gaps and the pieces with
    /// their size and digest, and the uploader, which owns what it learned from the server. Each changes the manifest by
    /// handing over only what it owns, so neither undoes the other with an older copy of the whole.
    ///
    /// Takes from `capture` what the recorder owns and keeps what the uploader wrote. A piece the recorder no longer lists
    /// (its file was never written) goes; a piece only the recorder lists is added.
    public mutating func adoptCapture(from capture: LocalRecording) {
        recordingCaptureState = capture.recordingCaptureState
        timelineDurationMS = capture.timelineDurationMS
        captureGaps = capture.captureGaps
        let uploadedPieces = Dictionary(localMedia.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        localMedia = capture.localMedia.map { piece in
            guard var merged = uploadedPieces[piece.id] else { return piece }
            merged.mediaDurationMS = piece.mediaDurationMS
            merged.originalSizeBytes = piece.originalSizeBytes
            merged.originalSha256 = piece.originalSha256
            return merged
        }
    }

    /// Takes from `upload` what the uploader owns and keeps what the recorder wrote.
    public mutating func adoptUpload(from upload: LocalRecording) {
        recordingId = upload.recordingId
        hasPublishedCapture = upload.hasPublishedCapture
        uploadErrorMessage = upload.uploadErrorMessage
        isUploadWaitingForNetwork = upload.isUploadWaitingForNetwork
        isDeletedRemotely = upload.isDeletedRemotely
        for uploadedPiece in upload.localMedia {
            guard let index = localMedia.firstIndex(where: { $0.id == uploadedPiece.id }) else { continue }
            localMedia[index].mediaManifest = uploadedPiece.mediaManifest
            localMedia[index].acceptedParts = uploadedPiece.acceptedParts
            localMedia[index].completionReceipt = uploadedPiece.completionReceipt
            // A piece of a stopped capture whose digest the recorder never wrote (the app was ended while it was closed) is
            // measured by the uploader; a piece of a capture under way is not touched by it.
            if localMedia[index].originalSha256 == nil, uploadedPiece.originalSha256 != nil {
                localMedia[index].originalSizeBytes = uploadedPiece.originalSizeBytes
                localMedia[index].originalSha256 = uploadedPiece.originalSha256
                localMedia[index].mediaDurationMS = uploadedPiece.mediaDurationMS
            }
        }
    }
}

public struct LocalRecordingMedia: Codable, Hashable, Sendable, Identifiable {
    public let id: UUID
    public var mediaManifest: RecordingMedia?
    public let originalFilename: String
    public let originalContentType: String
    public let timelineStartMS: Int64
    public var mediaDurationMS: Int64
    public var originalSizeBytes: Int64
    public var originalSha256: String?
    public var acceptedParts: [RecordingMediaPart]
    public var completionReceipt: RecordingMedia?

    public init(
        originalFilename: String, originalContentType: String, timelineStartMS: Int64, mediaDurationMS: Int64 = 0, localMediaId: UUID = UUID()
    ) {
        id = localMediaId
        self.originalFilename = originalFilename
        self.originalContentType = originalContentType
        self.timelineStartMS = timelineStartMS
        self.mediaDurationMS = mediaDurationMS
        originalSizeBytes = 0
        acceptedParts = []
    }

    public var isDurablyUploaded: Bool {
        guard let mediaManifest, let completionReceipt else { return false }
        return completionReceipt.confirmsOriginal(mediaManifest)
            && mediaManifest.originalSizeBytes == originalSizeBytes && mediaManifest.originalSha256 == originalSha256
    }
}

/// RecordingJournal owns local sources and refuses to read or release another account's originals.
/// Random directory names never come from a server response or an imported filename.
public actor RecordingJournal {
    private let directory: URL
    private let fileManager: FileManager

    #if os(iOS)
        public static func sharedDirectory() throws -> URL {
            guard let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String,
                let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
            else {
                throw TeaNodeError.refused("Recording storage is unavailable. Check this app's shared-container entitlement.")
            }
            return container.appendingPathComponent("Recordings", isDirectory: true)
        }
    #endif

    public init(directory: URL, fileManager: FileManager = .default) throws {
        self.directory = directory
        self.fileManager = fileManager
        try fileManager.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    }

    public func create(_ recording: LocalRecording) throws {
        let manifest = directory.appendingPathComponent(recording.id.uuidString).appendingPathComponent("manifest.json")
        guard !fileManager.fileExists(atPath: manifest.path) else { throw TeaNodeError.refused("This local recording already exists.") }
        try write(recording)
    }

    public func save(_ recording: LocalRecording) throws {
        _ = try read(recording.id, accountKey: recording.accountKey)
        try write(recording)
    }

    /// Reads the manifest, lets `change` alter it and writes it back, with nothing else able to write in between, and gives back
    /// what was written. A writer that shares the manifest with another uses this and `LocalRecording.adoptCapture` or
    /// `adoptUpload`, rather than saving a copy it read earlier.
    public func update(_ recordingId: UUID, accountKey: String, _ change: @Sendable (inout LocalRecording) throws -> Void) throws -> LocalRecording {
        var recording = try read(recordingId, accountKey: accountKey)
        try change(&recording)
        try write(recording)
        return recording
    }

    private func write(_ recording: LocalRecording) throws {
        let recordingDirectory = directory.appendingPathComponent(recording.id.uuidString, isDirectory: true)
        try fileManager.createDirectory(at: recordingDirectory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let manifest = recordingDirectory.appendingPathComponent("manifest.json")
        try JSONEncoder().encode(recording).write(to: manifest, options: .atomic)
        try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: manifest.path)
        let handle = try FileHandle(forWritingTo: manifest)
        try handle.synchronize()
        try handle.close()
    }

    public func recordings(accountKey: String) throws -> [LocalRecording] {
        let directories = try fileManager.contentsOfDirectory(
            at: directory, includingPropertiesForKeys: [.isDirectoryKey], options: .skipsHiddenFiles)
        return directories.compactMap { entry in
            guard let recordingId = UUID(uuidString: entry.lastPathComponent),
                let recording = try? read(recordingId, accountKey: accountKey)
            else { return nil }
            return recording
        }.sorted { $0.startedAt > $1.startedAt }
    }

    public func read(_ recordingId: UUID, accountKey: String) throws -> LocalRecording {
        let manifest = directory.appendingPathComponent(recordingId.uuidString).appendingPathComponent("manifest.json")
        let recording = try JSONDecoder().decode(LocalRecording.self, from: Data(contentsOf: manifest))
        guard recording.id == recordingId, recording.accountKey == accountKey else { throw TeaNodeError.signedOut }
        return recording
    }

    public func recoverableRecording(localRecordingId: String, retainedOwners: [String: String]) throws -> LocalRecording? {
        guard let recordingId = UUID(uuidString: localRecordingId) else { return nil }
        for (accountKey, userId) in retainedOwners {
            guard let recording = try? read(recordingId, accountKey: accountKey),
                recording.belongsTo(accountKey: accountKey, recordingUserId: userId), recording.recordingOrigin == "capture",
                ["active", "paused", "interrupted"].contains(recording.recordingCaptureState), !recording.isDeletedRemotely
            else { continue }
            return recording
        }
        return nil
    }

    public func originalURL(recordingId: UUID, mediaId: UUID, accountKey: String) throws -> URL {
        let recording = try read(recordingId, accountKey: accountKey)
        guard recording.localMedia.contains(where: { $0.id == mediaId }) else { throw TeaNodeError.unreadableAnswer }
        return directory.appendingPathComponent(recordingId.uuidString).appendingPathComponent(mediaId.uuidString + ".original")
    }

    public func importOriginal(from source: URL, recordingId: UUID, mediaId: UUID, accountKey: String) throws {
        let destination = try originalURL(recordingId: recordingId, mediaId: mediaId, accountKey: accountKey)
        guard !fileManager.fileExists(atPath: destination.path) else { throw TeaNodeError.refused("This original is already stored.") }
        let pending = destination.appendingPathExtension("pending")
        try? fileManager.removeItem(at: pending)
        do {
            try fileManager.copyItem(at: source, to: pending)
            try fileManager.setAttributes([.posixPermissions: 0o600], ofItemAtPath: pending.path)
            let handle = try FileHandle(forWritingTo: pending)
            try handle.synchronize()
            try handle.close()
            try fileManager.moveItem(at: pending, to: destination)
        } catch {
            try? fileManager.removeItem(at: pending)
            throw error
        }
    }

    public func releaseUploadedOriginal(recordingId: UUID, mediaId: UUID, accountKey: String) throws {
        let recording = try read(recordingId, accountKey: accountKey)
        guard let media = recording.localMedia.first(where: { $0.id == mediaId }), media.isDurablyUploaded else {
            throw TeaNodeError.refused("Keep this original until the server verifies the complete file.")
        }
        let original = try originalURL(recordingId: recordingId, mediaId: mediaId, accountKey: accountKey)
        if fileManager.fileExists(atPath: original.path) { try fileManager.removeItem(at: original) }
    }

    public func removeLocalRecording(_ recordingId: UUID, accountKey: String) throws {
        _ = try read(recordingId, accountKey: accountKey)
        try fileManager.removeItem(at: directory.appendingPathComponent(recordingId.uuidString))
    }
}
