import AVFoundation
import ActivityKit
import CryptoKit
import Observation
import TeaNodeKit

/// MeetingRecorder records locally without opening a conversation or contacting a provider.
@MainActor
@Observable
final class MeetingRecorder: NSObject {
    static let shared = MeetingRecorder()
    private(set) var localRecording: LocalRecording?
    private(set) var elapsedMS: Int64 = 0
    var problemText: String?
    private var audioRecorder: AVAudioRecorder?
    private var journal: RecordingJournal?
    private var captureTask: Task<Void, Never>?
    private var interruptionObserver: (any NSObjectProtocol)?
    private var mediaResetObserver: (any NSObjectProtocol)?
    private var activityId: String?
    private var startedUptime: TimeInterval = 0
    private var timelineAtStartMS: Int64 = 0
    private var isChangingCapture = false
    private var hasPendingInterruption = false

    var isCapturing: Bool { localRecording?.recordingCaptureState == "active" }
    var hasSession: Bool { localRecording != nil && localRecording?.recordingCaptureState != "stopped" }

    static func makeJournal() throws -> RecordingJournal {
        let directory = try RecordingJournal.sharedDirectory()
        return try RecordingJournal(directory: directory)
    }

    func start(accountKey: String, title: String) async {
        guard !hasSession, !isChangingCapture, AppModel.shared.currentCall == nil else {
            problemText = String(localized: "Stop the current recording or call before starting another recording.")
            return
        }
        isChangingCapture = true
        defer { finishChangingCapture() }
        guard await AVAudioApplication.requestRecordPermission() else {
            problemText = String(localized: "Allow microphone access in Settings, then start recording again.")
            return
        }
        do {
            journal = try Self.makeJournal()
            var recording = LocalRecording(accountKey: accountKey, recordingTitle: title, recordingOrigin: "capture")
            try await journal?.save(recording)
            localRecording = recording
            elapsedMS = 0
            startedUptime = ProcessInfo.processInfo.systemUptime
            timelineAtStartMS = 0
            try await startSegment()
            recording = localRecording ?? recording
            observeInterruptions()
            startClock()
            if ActivityAuthorizationInfo().areActivitiesEnabled {
                let activity = try? Activity.request(
                    attributes: RecordingActivityAttributes(localRecordingId: recording.id.uuidString),
                    content: ActivityContent(
                        state: RecordingActivityAttributes.ContentState(recordingCaptureState: "active", elapsedMS: 0, updatedAt: Date()),
                        staleDate: nil), pushType: nil)
                activityId = activity?.id
            }
            problemText = nil
        } catch { await fail(error) }
    }

    func pause(isInterruption: Bool = false) async {
        guard isCapturing else { return }
        if isChangingCapture {
            if isInterruption { hasPendingInterruption = true }
            return
        }
        isChangingCapture = true
        defer { finishChangingCapture() }
        do {
            updateElapsed()
            try await finishSegment()
            guard var recording = localRecording else { return }
            try recording.pause(at: elapsedMS, gapReason: isInterruption ? "interrupted" : "paused")
            try await journal?.save(recording)
            localRecording = recording
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
            await updateActivity()
        } catch { await fail(error) }
    }

    func resume(localRecordingId: String? = nil) async {
        guard let current = localRecording, localRecordingId == nil || current.id.uuidString == localRecordingId,
            ["paused", "interrupted"].contains(current.recordingCaptureState), !isChangingCapture,
            AppModel.shared.currentCall == nil
        else { return }
        isChangingCapture = true
        defer { finishChangingCapture() }
        do {
            updateElapsed()
            var recording = current
            try recording.resume(at: elapsedMS)
            try await journal?.save(recording)
            localRecording = recording
            try await startSegment()
            startClock()
            await updateActivity()
        } catch { await fail(error) }
    }

    func stop(localRecordingId: String? = nil) async {
        guard let current = localRecording, localRecordingId == nil || current.id.uuidString == localRecordingId,
            current.recordingCaptureState != "stopped", !isChangingCapture
        else { return }
        isChangingCapture = true
        defer { finishChangingCapture() }
        do {
            updateElapsed()
            try await finishSegment()
            guard var recording = localRecording else { return }
            try recording.stop(at: elapsedMS)
            try await journal?.save(recording)
            localRecording = recording
            captureTask?.cancel()
            captureTask = nil
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
            if let activityId { await RecordingActivities.end(activityId) }
            activityId = nil
        } catch { await fail(error) }
    }

    func recover(accountKey: String) async {
        guard !hasSession else { return }
        do {
            journal = try Self.makeJournal()
            guard
                var recording = try await journal?.recordings(accountKey: accountKey).first(where: {
                    $0.recordingOrigin == "capture" && $0.recordingCaptureState != "stopped"
                })
            else { return }
            localRecording = recording
            elapsedMS = max(recording.timelineDurationMS, Int64(max(0, Date().timeIntervalSince(recording.startedAt)) * 1000))
            if let media = recording.localMedia.last, media.originalSha256 == nil {
                try await finishSegment()
                recording = localRecording ?? recording
            }
            if recording.recordingCaptureState == "active" {
                try recording.pause(at: recording.timelineDurationMS, gapReason: "interrupted")
                try await journal?.save(recording)
            }
            localRecording = recording
            startedUptime = ProcessInfo.processInfo.systemUptime
            timelineAtStartMS = elapsedMS
            observeInterruptions()
            startClock()
            problemText = String(localized: "Recovered local audio. Resume when you are ready; the interruption is marked as a gap.")
            for activity in Activity<RecordingActivityAttributes>.activities { await activity.end(nil, dismissalPolicy: .immediate) }
        } catch { problemText = error.localizedDescription }
    }

    private func finishChangingCapture() {
        isChangingCapture = false
        if hasPendingInterruption {
            hasPendingInterruption = false
            Task { await pause(isInterruption: true) }
        }
    }

    private func startSegment() async throws {
        guard var recording = localRecording, let journal else { throw TeaNodeError.unreadableAnswer }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .default, options: [.allowBluetoothHFP])
        try session.setActive(true)
        let media = LocalRecordingMedia(originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: elapsedMS)
        recording.localMedia.append(media)
        try await journal.save(recording)
        localRecording = recording
        let original = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: recording.accountKey)
        let recorder = try AVAudioRecorder(
            url: original,
            settings: [
                AVFormatIDKey: kAudioFormatLinearPCM, AVSampleRateKey: 16000, AVNumberOfChannelsKey: 1,
                AVLinearPCMBitDepthKey: 16, AVLinearPCMIsFloatKey: false, AVLinearPCMIsBigEndianKey: false,
            ])
        recorder.delegate = self
        guard recorder.prepareToRecord(), recorder.record(forDuration: 600) else {
            throw TeaNodeError.refused(String(localized: "Recording could not start. Check microphone access and available storage."))
        }
        try FileManager.default.setAttributes(
            [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication, .posixPermissions: 0o600], ofItemAtPath: original.path)
        audioRecorder = recorder
    }

    private func finishSegment() async throws {
        audioRecorder?.delegate = nil
        audioRecorder?.stop()
        audioRecorder = nil
        guard var recording = localRecording, let journal, let mediaIndex = recording.localMedia.indices.last,
            recording.localMedia[mediaIndex].originalSha256 == nil
        else { return }
        let media = recording.localMedia[mediaIndex]
        let original = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: recording.accountKey)
        guard FileManager.default.fileExists(atPath: original.path) else {
            recording.localMedia.remove(at: mediaIndex)
            try await journal.save(recording)
            localRecording = recording
            return
        }
        let audio = try AVAudioFile(forReading: original)
        recording.localMedia[mediaIndex].mediaDurationMS = Int64(Double(audio.length) / audio.fileFormat.sampleRate * 1000)
        let originalSize = try original.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
        recording.localMedia[mediaIndex].originalSizeBytes = Int64(originalSize)
        recording.localMedia[mediaIndex].originalSha256 = try await Task.detached { try Self.digest(original) }.value
        recording.timelineDurationMS = max(recording.timelineDurationMS, media.timelineStartMS + recording.localMedia[mediaIndex].mediaDurationMS)
        try await journal.save(recording)
        localRecording = recording
    }

    nonisolated static func digest(_ original: URL) throws -> String {
        let handle = try FileHandle(forReadingFrom: original)
        defer { try? handle.close() }
        var hash = SHA256()
        while let chunk = try handle.read(upToCount: 1024 * 1024), !chunk.isEmpty { hash.update(data: chunk) }
        return hash.finalize().map { String(format: "%02x", $0) }.joined()
    }

    private func updateElapsed() {
        elapsedMS = max(elapsedMS, timelineAtStartMS + Int64(max(0, ProcessInfo.processInfo.systemUptime - startedUptime) * 1000))
    }

    private func startClock() {
        captureTask?.cancel()
        captureTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(1))
                guard !Task.isCancelled, let self else { return }
                self.updateElapsed()
                if self.elapsedMS % 5000 < 1000 {
                    if !self.isChangingCapture, var recording = self.localRecording {
                        self.isChangingCapture = true
                        recording.timelineDurationMS = self.elapsedMS
                        do {
                            try await self.journal?.save(recording)
                            self.localRecording = recording
                            self.finishChangingCapture()
                        } catch {
                            self.finishChangingCapture()
                            await self.pause(isInterruption: true)
                            self.problemText = error.localizedDescription
                        }
                    }
                    await self.updateActivity()
                }
            }
        }
    }

    private func observeInterruptions() {
        if let interruptionObserver { NotificationCenter.default.removeObserver(interruptionObserver) }
        if let mediaResetObserver { NotificationCenter.default.removeObserver(mediaResetObserver) }
        interruptionObserver = NotificationCenter.default.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) {
            [weak self] notification in
            let isBeginning = (notification.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt) == AVAudioSession.InterruptionType.began.rawValue
            if isBeginning { Task { @MainActor in await self?.pause(isInterruption: true) } }
        }
        mediaResetObserver = NotificationCenter.default.addObserver(
            forName: AVAudioSession.mediaServicesWereResetNotification, object: nil, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in await self?.pause(isInterruption: true) }
        }
    }

    private func updateActivity() async {
        guard let activityId, let localRecording else { return }
        await RecordingActivities.update(activityId, captureState: localRecording.recordingCaptureState, elapsedMS: elapsedMS)
    }

    private func fail(_ error: Error) async {
        audioRecorder?.delegate = nil
        audioRecorder?.stop()
        audioRecorder = nil
        problemText = error.localizedDescription
        if var recording = localRecording, recording.recordingCaptureState == "active" {
            try? recording.pause(at: max(elapsedMS, recording.timelineDurationMS), gapReason: "interrupted")
            try? await journal?.save(recording)
            localRecording = recording
        }
        await updateActivity()
    }
}

extension MeetingRecorder: AVAudioRecorderDelegate {
    nonisolated func audioRecorderDidFinishRecording(_ recorder: AVAudioRecorder, successfully flag: Bool) {
        Task { @MainActor [weak self] in
            guard let self, self.isCapturing, !self.isChangingCapture else { return }
            if !flag {
                await self.pause(isInterruption: true)
                return
            }
            self.isChangingCapture = true
            defer { self.finishChangingCapture() }
            do {
                self.updateElapsed()
                try await self.finishSegment()
                try await self.startSegment()
            } catch { await self.fail(error) }
        }
    }
}
