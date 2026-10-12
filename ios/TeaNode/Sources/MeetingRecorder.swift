import AVFoundation
import ActivityKit
import CryptoKit
import Observation
import TeaNodeKit
import UIKit

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
    private var pendingTransitions = RecordingCaptureTransitions()
    private var recorderId: UUID?

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
        guard title.utf8.count <= 255 else {
            problemText = String(localized: "Use a recording title of at most 255 UTF-8 bytes.")
            return
        }
        isChangingCapture = true
        defer { finishChangingCapture() }
        guard await AVAudioApplication.requestRecordPermission() else {
            problemText = String(localized: "Allow microphone access in Settings, then start recording again.")
            return
        }
        guard AppModel.shared.account?.accountKey == accountKey, AppModel.shared.currentCall == nil else { return }
        do {
            journal = try Self.makeJournal()
            var recording = LocalRecording(accountKey: accountKey, recordingTitle: title, recordingOrigin: "capture")
            try await journal?.create(recording)
            localRecording = recording
            elapsedMS = 0
            startedUptime = ProcessInfo.processInfo.systemUptime
            timelineAtStartMS = 0
            _ = ensureRecordingActivity(canCreate: true)
            try await startSegment()
            recording = localRecording ?? recording
            observeInterruptions()
            startClock()
            problemText = nil
        } catch { await fail(error) }
    }

    func pause(isInterruption: Bool = false) async {
        guard isCapturing else { return }
        if isChangingCapture {
            pendingTransitions.enqueue(.pause(isInterruption: isInterruption))
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

    func resume(localRecordingId: String? = nil, requiresExistingActivity: Bool = false) async {
        guard let current = localRecording, localRecordingId == nil || current.id.uuidString == localRecordingId,
            ["paused", "interrupted"].contains(current.recordingCaptureState), !isChangingCapture,
            AppModel.shared.currentCall == nil
        else { return }
        if !ensureRecordingActivity(canCreate: !requiresExistingActivity), requiresExistingActivity {
            problemText = String(localized: "Open TeaNode to resume this recording.")
            return
        }
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
            current.recordingCaptureState != "stopped"
        else { return }
        if isChangingCapture {
            pendingTransitions.enqueue(.stop)
            return
        }
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
            if let account = try? KeychainAccountStore().load(accountKey: recording.accountKey) {
                RecordingUploads.shared.resume(
                    connection: Connection(account: account, transport: URLSessionTransport(), store: KeychainAccountStore()))
            }
        } catch { await fail(error) }
    }

    func recover(accountKey: String, localRecordingId: UUID? = nil) async {
        guard !hasSession else { return }
        do {
            journal = try Self.makeJournal()
            let recovered: LocalRecording?
            if let localRecordingId {
                recovered = try await journal?.read(localRecordingId, accountKey: accountKey)
            } else {
                recovered = try await journal?.recordings(accountKey: accountKey).first(where: {
                    $0.recordingOrigin == "capture" && $0.recordingCaptureState != "stopped"
                })
            }
            guard var recording = recovered else { return }
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
            for activity in Activity<RecordingActivityAttributes>.activities {
                if activity.attributes.localRecordingId == recording.id.uuidString {
                    activityId = activity.id
                } else {
                    await activity.end(nil, dismissalPolicy: .immediate)
                }
            }
        } catch { problemText = error.localizedDescription }
    }

    func stopForAccountRemoval(accountKey: String) async {
        while isChangingCapture { await Task.yield() }
        guard localRecording?.accountKey == accountKey else { return }
        await stop()
    }

    func restoreForActivity(localRecordingId: String) async -> Bool {
        let store = KeychainAccountStore()
        guard let retainedAccounts = try? store.accounts().accounts else { return false }
        if hasSession {
            return localRecording?.id.uuidString == localRecordingId && retainedAccounts.contains { $0.accountKey == localRecording?.accountKey }
        }
        do {
            let journal = try Self.makeJournal()
            guard
                let recovered = try await journal.recoverableRecording(
                    localRecordingId: localRecordingId, retainedAccountKeys: retainedAccounts.map(\.accountKey))
            else { return false }
            await recover(accountKey: recovered.accountKey, localRecordingId: recovered.id)
            return hasSession && localRecording?.id == recovered.id
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }

    private func finishChangingCapture() {
        isChangingCapture = false
        guard let command = pendingTransitions.next() else { return }
        let sessionId = localRecording?.id
        Task {
            guard localRecording?.id == sessionId else { return }
            switch command {
            case .stop: await stop(localRecordingId: sessionId?.uuidString)
            case .pause(let isInterruption): await pause(isInterruption: isInterruption)
            case .rollover(let recorderId, let wasSuccessful): await rollover(recorderId: recorderId, wasSuccessful: wasSuccessful)
            }
        }
    }

    private func rollover(recorderId expectedRecorderId: UUID, wasSuccessful: Bool) async {
        guard recorderId == expectedRecorderId, isCapturing else { return }
        if isChangingCapture {
            pendingTransitions.enqueue(.rollover(recorderId: expectedRecorderId, wasSuccessful: wasSuccessful))
            return
        }
        if !wasSuccessful {
            await pause(isInterruption: true)
            return
        }
        isChangingCapture = true
        defer { finishChangingCapture() }
        do {
            updateElapsed()
            try await finishSegment()
            if pendingTransitions.shouldStartNextSegment { try await startSegment() }
        } catch { await fail(error) }
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
        guard pendingTransitions.shouldStartNextSegment else { return }
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
        recorderId = UUID()
    }

    private func finishSegment() async throws {
        audioRecorder?.delegate = nil
        audioRecorder?.stop()
        audioRecorder = nil
        recorderId = nil
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
        if let audio = try? AVAudioFile(forReading: original) {
            recording.localMedia[mediaIndex].mediaDurationMS = Int64(Double(audio.length) / audio.fileFormat.sampleRate * 1000)
        } else {
            problemText = String(localized: "This audio segment cannot be read locally. Its original is kept for recovery.")
        }
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

    private func ensureRecordingActivity(canCreate: Bool) -> Bool {
        guard let localRecording else { return false }
        if let existing = Activity<RecordingActivityAttributes>.activities.first(where: {
            $0.attributes.localRecordingId == localRecording.id.uuidString && ($0.activityState == .active || $0.activityState == .stale)
        }) {
            activityId = existing.id
            return true
        }
        guard canCreate, ActivityAuthorizationInfo().areActivitiesEnabled, UIApplication.shared.applicationState == .active else { return false }
        do {
            let activity = try Activity.request(
                attributes: RecordingActivityAttributes(localRecordingId: localRecording.id.uuidString),
                content: ActivityContent(
                    state: RecordingActivityAttributes.ContentState(
                        recordingCaptureState: localRecording.recordingCaptureState, elapsedMS: elapsedMS, updatedAt: Date()), staleDate: nil),
                pushType: nil)
            activityId = activity.id
            return true
        } catch { return false }
    }

    private func updateActivity() async {
        guard let activityId, let localRecording else { return }
        await RecordingActivities.update(activityId, captureState: localRecording.recordingCaptureState, elapsedMS: elapsedMS)
    }

    private func fail(_ error: Error) async {
        audioRecorder?.delegate = nil
        audioRecorder?.stop()
        audioRecorder = nil
        recorderId = nil
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
            guard let self, self.audioRecorder === recorder, let recorderId = self.recorderId else { return }
            await self.rollover(recorderId: recorderId, wasSuccessful: flag)
        }
    }
}
