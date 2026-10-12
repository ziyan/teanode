import CryptoKit
import Foundation
import TeaNodeKit

/// RecordingUploads uses file-backed background tasks; source journals survive task and app restarts.
@MainActor
final class RecordingUploads: NSObject {
    static let shared = RecordingUploads()
    private lazy var session = URLSession(
        configuration: {
            let configuration = URLSessionConfiguration.background(withIdentifier: "recording-originals")
            configuration.sessionSendsLaunchEvents = true
            configuration.isDiscretionary = false
            configuration.httpMaximumConnectionsPerHost = 1
            return configuration
        }(), delegate: self, delegateQueue: nil)
    private let responses = RecordingUploadResponses()
    private let completions = RecordingUploadCompletions()
    private var workerTask: Task<Void, Never>?
    private var backgroundCompletion: (() -> Void)?

    struct PartTask: Codable, Sendable {
        let localRecordingId: UUID
        let localMediaId: UUID
        let accountKey: String
        let mediaId: String
        let partIndex: Int
        let partSizeBytes: Int64
        let partSha256: String
        let transportFilename: String
    }

    func handleBackgroundEvents(identifier: String, completion: @escaping () -> Void) {
        guard identifier == "recording-originals" else {
            completion()
            return
        }
        backgroundCompletion = completion
        _ = session
    }

    func resume(connection: Connection) {
        guard workerTask == nil else { return }
        workerTask = Task { [weak self] in
            guard let self else { return }
            defer { self.workerTask = nil }
            do {
                guard await self.session.allTasks.isEmpty else { return }
                let journal = try MeetingRecorder.makeJournal()
                let recordings = try await journal.recordings(accountKey: connection.accountKey)
                for recording in recordings where ["stopped", "imported"].contains(recording.recordingCaptureState) && !recording.isDeletedRemotely {
                    if try await self.advance(recording.id, connection: connection, journal: journal) { return }
                }
            } catch {}
        }
    }

    private func advance(_ recordingId: UUID, connection: Connection, journal: RecordingJournal) async throws -> Bool {
        var recording = try await journal.read(recordingId, accountKey: connection.accountKey)
        do {
            if recording.recordingId == nil {
                let remote = try await connection.createRecording(
                    clientRecordingId: recording.clientRecordingId, recordingTitle: recording.recordingTitle,
                    recordingOrigin: recording.recordingOrigin)
                recording.recordingId = remote.id
                try await journal.save(recording)
            }
            guard let remoteId = recording.recordingId else { throw TeaNodeError.unreadableAnswer }
            if recording.recordingOrigin == "capture", recording.hasPublishedCapture != true {
                var remote = try await connection.recording(remoteId).recording
                if remote.recordedAt == nil {
                    remote = try await connection.updateRecordingMetadata(
                        recordingId: remoteId, expectedSourceGeneration: remote.sourceGeneration,
                        recordingTitle: remote.recordingTitle, recordedAt: recording.startedAt.formatted(.iso8601))
                }
                if remote.recordingCaptureState != "stopped" || remote.timelineDurationMS != recording.timelineDurationMS
                    || remote.captureGaps != recording.captureGaps
                {
                    _ = try await connection.updateRecordingCapture(
                        recordingId: remoteId, expectedGeneration: remote.sourceGeneration,
                        recordingCaptureState: "stopped", timelineDurationMS: recording.timelineDurationMS, captureGaps: recording.captureGaps)
                }
                recording.hasPublishedCapture = true
                try await journal.save(recording)
            }
            let limits = try await connection.recordingLimits()
            guard limits.uploadPartBytes == 8 * 1024 * 1024 else { throw TeaNodeError.unreadableAnswer }
            for mediaIndex in recording.localMedia.indices {
                let localMedia = recording.localMedia[mediaIndex]
                if localMedia.isDurablyUploaded { continue }
                guard let digest = localMedia.originalSha256, localMedia.originalSizeBytes > 0 else { continue }
                let original = try await journal.originalURL(recordingId: recording.id, mediaId: localMedia.id, accountKey: recording.accountKey)
                if localMedia.mediaManifest == nil {
                    let verifiedDigest = try await Task.detached { try MeetingRecorder.digest(original) }.value
                    guard verifiedDigest == digest else {
                        throw TeaNodeError.refused(String(localized: "The local original changed. Keep it and try importing again."))
                    }
                    recording.localMedia[mediaIndex].mediaManifest = try await connection.createRecordingMedia(
                        recordingId: remoteId,
                        clientMediaId: localMedia.id.uuidString, originalFilename: localMedia.originalFilename,
                        originalContentType: localMedia.originalContentType,
                        originalSizeBytes: localMedia.originalSizeBytes, originalSha256: digest, timelineStartMS: localMedia.timelineStartMS,
                        mediaDurationMS: localMedia.mediaDurationMS)
                    try await journal.save(recording)
                }
                guard let manifest = recording.localMedia[mediaIndex].mediaManifest, manifest.recordingId == remoteId,
                    manifest.clientMediaId == localMedia.id.uuidString, manifest.originalSha256 == digest,
                    manifest.originalSizeBytes == localMedia.originalSizeBytes,
                    manifest.partCount == Int((localMedia.originalSizeBytes + limits.uploadPartBytes - 1) / limits.uploadPartBytes)
                else { throw TeaNodeError.unreadableAnswer }
                let remote = try await connection.recording(remoteId)
                recording.localMedia[mediaIndex].acceptedParts = remote.uploadParts.filter { $0.recordingMediaId == manifest.id }
                let handle = try FileHandle(forReadingFrom: original)
                defer { try? handle.close() }
                for partIndex in 0..<manifest.partCount {
                    let partOffset = UInt64(partIndex) * UInt64(limits.uploadPartBytes)
                    let partSize = Int(min(limits.uploadPartBytes, manifest.originalSizeBytes - Int64(partOffset)))
                    try handle.seek(toOffset: partOffset)
                    guard let part = try handle.read(upToCount: partSize), part.count == partSize else { throw TeaNodeError.unreadableAnswer }
                    let partDigest = SHA256.hash(data: part).map { String(format: "%02x", $0) }.joined()
                    if recording.localMedia[mediaIndex].acceptedParts.contains(where: {
                        $0.confirmsPart(mediaId: manifest.id, partIndex: partIndex, partSizeBytes: Int64(partSize), partSha256: partDigest)
                    }) {
                        continue
                    }
                    let transportFile = original.deletingLastPathComponent().appendingPathComponent(UUID().uuidString + ".upload")
                    try part.write(to: transportFile, options: .atomic)
                    try FileManager.default.setAttributes(
                        [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication, .posixPermissions: 0o600],
                        ofItemAtPath: transportFile.path)
                    var request = try await connection.recordingPlaybackRequest(mediaId: manifest.id)
                    request.url = request.url?.appendingPathComponent("parts").appendingPathComponent(String(partIndex))
                    request.httpMethod = "PUT"
                    request.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
                    request.setValue(String(partSize), forHTTPHeaderField: "Content-Length")
                    request.setValue(partDigest, forHTTPHeaderField: "X-Recording-Part-SHA256")
                    let partTask = PartTask(
                        localRecordingId: recording.id, localMediaId: localMedia.id, accountKey: recording.accountKey,
                        mediaId: manifest.id, partIndex: partIndex, partSizeBytes: Int64(partSize), partSha256: partDigest,
                        transportFilename: transportFile.lastPathComponent)
                    let task = session.uploadTask(with: request, fromFile: transportFile)
                    task.taskDescription = String(data: try JSONEncoder().encode(partTask), encoding: .utf8)
                    recording.uploadErrorMessage = nil
                    try await journal.save(recording)
                    task.resume()
                    return true
                }
                recording.localMedia[mediaIndex].completionReceipt = try await connection.completeRecordingUpload(manifest)
                try await journal.save(recording)
                try await journal.releaseUploadedOriginal(recordingId: recording.id, mediaId: localMedia.id, accountKey: recording.accountKey)
            }
            recording.uploadErrorMessage = nil
            try await journal.save(recording)
            return false
        } catch {
            recording.uploadErrorMessage = error.localizedDescription
            if error as? TeaNodeError == .recordingDeleted { recording.isDeletedRemotely = true }
            try await journal.save(recording)
            throw error
        }
    }

    private func finished(_ task: PartTask, responseCode: Int?, responseData: Data, errorMessage: String?) async {
        do {
            let journal = try MeetingRecorder.makeJournal()
            var recording = try await journal.read(task.localRecordingId, accountKey: task.accountKey)
            guard let mediaIndex = recording.localMedia.firstIndex(where: { $0.id == task.localMediaId }),
                recording.localMedia[mediaIndex].mediaManifest?.id == task.mediaId
            else { return }
            let original = try await journal.originalURL(recordingId: recording.id, mediaId: task.localMediaId, accountKey: task.accountKey)
            if UUID(uuidString: String(task.transportFilename.dropLast(".upload".count))) != nil, task.transportFilename.hasSuffix(".upload") {
                try? FileManager.default.removeItem(at: original.deletingLastPathComponent().appendingPathComponent(task.transportFilename))
            }
            if responseCode == 410 {
                recording.isDeletedRemotely = true
                recording.uploadErrorMessage = String(localized: "This recording was deleted on the server. Its local original was kept.")
            } else if errorMessage == nil, responseCode == 200, responseData.count <= 32 * 1024,
                let receipt = try? JSONDecoder().decode(RecordingMediaPart.self, from: responseData),
                receipt.confirmsPart(mediaId: task.mediaId, partIndex: task.partIndex, partSizeBytes: task.partSizeBytes, partSha256: task.partSha256)
            {
                recording.localMedia[mediaIndex].acceptedParts.removeAll { $0.partIndex == task.partIndex }
                recording.localMedia[mediaIndex].acceptedParts.append(receipt)
                recording.uploadErrorMessage = nil
            } else {
                recording.uploadErrorMessage = errorMessage ?? String(localized: "The upload was not verified. Keep the original and retry.")
            }
            try await journal.save(recording)
            if recording.uploadErrorMessage == nil, let account = try KeychainAccountStore().load(accountKey: task.accountKey) {
                resume(connection: Connection(account: account, transport: URLSessionTransport(), store: KeychainAccountStore()))
            }
        } catch {}
    }
}

extension RecordingUploads: URLSessionDataDelegate {
    nonisolated func urlSession(_ session: URLSession, dataTask: URLSessionDataTask, didReceive data: Data) {
        responses.append(data, taskId: dataTask.taskIdentifier)
    }
    nonisolated func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        let responseData = responses.take(taskId: task.taskIdentifier)
        guard let description = task.taskDescription, let descriptionData = description.data(using: .utf8),
            let partTask = try? JSONDecoder().decode(PartTask.self, from: descriptionData)
        else { return }
        let responseCode = (task.response as? HTTPURLResponse)?.statusCode
        let errorMessage = error?.localizedDescription
        let completed = Task { @MainActor in
            await finished(partTask, responseCode: responseCode, responseData: responseData, errorMessage: errorMessage)
        }
        completions.append(completed)
    }
    nonisolated func urlSessionDidFinishEvents(forBackgroundURLSession session: URLSession) {
        let completedTasks = completions.take()
        Task { @MainActor in
            for completed in completedTasks { await completed.value }
            backgroundCompletion?()
            backgroundCompletion = nil
        }
    }
}

private final class RecordingUploadResponses: @unchecked Sendable {
    private let lock = NSLock()
    private var responseDataByTaskId: [Int: Data] = [:]
    func append(_ chunk: Data, taskId: Int) {
        lock.lock()
        defer { lock.unlock() }
        var responseData = responseDataByTaskId[taskId] ?? Data()
        if responseData.count + chunk.count <= 32 * 1024 {
            responseData.append(chunk)
        } else {
            responseData = Data(repeating: 0, count: 32 * 1024 + 1)
        }
        responseDataByTaskId[taskId] = responseData
    }
    func take(taskId: Int) -> Data {
        lock.lock()
        defer { lock.unlock() }
        return responseDataByTaskId.removeValue(forKey: taskId) ?? Data()
    }
}

private final class RecordingUploadCompletions: @unchecked Sendable {
    private let lock = NSLock()
    private var completedTasks: [Task<Void, Never>] = []
    func append(_ completed: Task<Void, Never>) {
        lock.lock()
        defer { lock.unlock() }
        completedTasks.append(completed)
    }
    func take() -> [Task<Void, Never>] {
        lock.lock()
        defer { lock.unlock() }
        let completed = completedTasks
        completedTasks.removeAll()
        return completed
    }
}
