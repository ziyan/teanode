import AVFoundation
import CryptoKit
import Foundation
import TeaNodeKit
import UniformTypeIdentifiers

struct SharedRecordingFile: Identifiable {
    let id = UUID()
    let localMediaId = UUID()
    let originalURL: URL
    let originalFilename: String
    let originalContentType: String
    let isVideo: Bool
}

enum ShareRecordingImport {
    static func stage(provider: NSItemProvider) async throws -> SharedRecordingFile? {
        guard
            let identifier = provider.registeredTypeIdentifiers.first(where: { identifier in
                guard let contentType = UTType(identifier) else { return false }
                return contentType.conforms(to: .audio) || contentType.conforms(to: .movie)
            }), let contentType = UTType(identifier), let mimeType = contentType.preferredMIMEType
        else { return nil }
        let directory = try RecordingJournal.sharedDirectory().appendingPathComponent("Staging", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let destination = directory.appendingPathComponent(UUID().uuidString).appendingPathExtension(
            contentType.preferredFilenameExtension ?? "media")
        return try await withCheckedThrowingContinuation { continuation in
            provider.loadFileRepresentation(forTypeIdentifier: identifier) { source, error in
                do {
                    if let error { throw error }
                    guard let source else { throw TeaNodeError.unreadableAnswer }
                    try FileManager.default.copyItem(at: source, to: destination)
                    try FileManager.default.setAttributes(
                        [.posixPermissions: 0o600, .protectionKey: FileProtectionType.completeUntilFirstUserAuthentication],
                        ofItemAtPath: destination.path)
                    continuation.resume(
                        returning: SharedRecordingFile(
                            originalURL: destination, originalFilename: source.lastPathComponent, originalContentType: mimeType,
                            isVideo: contentType.conforms(to: .movie)))
                } catch { continuation.resume(throwing: error) }
            }
        }
    }

    static func save(_ file: SharedRecordingFile, accountKey: String, title: String) async throws {
        let journal = try RecordingJournal(directory: RecordingJournal.sharedDirectory())
        var recording = LocalRecording(
            accountKey: accountKey, recordingTitle: title.isEmpty ? (file.originalFilename as NSString).deletingPathExtension : title,
            recordingOrigin: file.isVideo ? "videoImport" : "audioImport", localRecordingId: file.id)
        let duration = try? await AVURLAsset(url: file.originalURL).load(.duration)
        let seconds = duration.map { CMTimeGetSeconds($0) } ?? 0
        var media = LocalRecordingMedia(
            originalFilename: file.originalFilename, originalContentType: file.originalContentType, timelineStartMS: 0,
            mediaDurationMS: seconds.isFinite && seconds > 0 ? Int64(seconds * 1000) : 0, localMediaId: file.localMediaId)
        media.originalSizeBytes = Int64(try file.originalURL.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0)
        let handle = try FileHandle(forReadingFrom: file.originalURL)
        defer { try? handle.close() }
        var hash = SHA256()
        while let chunk = try handle.read(upToCount: 1024 * 1024), !chunk.isEmpty { hash.update(data: chunk) }
        media.originalSha256 = hash.finalize().map { String(format: "%02x", $0) }.joined()
        recording.timelineDurationMS = media.mediaDurationMS
        recording.localMedia.append(media)
        if let existing = try? await journal.read(recording.id, accountKey: accountKey) {
            guard let existingMedia = existing.localMedia.first(where: { $0.id == media.id }), existingMedia.originalSha256 == media.originalSha256,
                existingMedia.originalSizeBytes == media.originalSizeBytes
            else { throw TeaNodeError.unreadableAnswer }
            if existingMedia.isDurablyUploaded { return }
        } else {
            try await journal.create(recording)
        }
        let originalURL = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: accountKey)
        if !FileManager.default.fileExists(atPath: originalURL.path) {
            try await journal.importOriginal(from: file.originalURL, recordingId: recording.id, mediaId: media.id, accountKey: accountKey)
        }
    }
}
