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
    /// The words heard while recording. It runs beside the recorder and never affects it.
    let liveStream = RecordingLiveStream()
    private var cutPolicy = RecordingCutPolicy()
    private var levelTask: Task<Void, Never>?
    private var liveStartTask: Task<Void, Never>?
    private var isCutting = false
    private var lastUploadKickUptime: TimeInterval = 0

    override init() {
        super.init()
        liveStream.didHearUtteranceEnd = { [weak self] in self?.heardUtteranceEnd() }
    }

    var isCapturing: Bool { localRecording?.recordingCaptureState == "active" }
    var hasSession: Bool { localRecording != nil && localRecording?.recordingCaptureState != "stopped" }

    static func makeJournal() throws -> RecordingJournal {
        let directory = try RecordingJournal.sharedDirectory()
        return try RecordingJournal(directory: directory)
    }

    /// The title is never asked for: the server names the recording from its transcript.
    func start(accountKey: String) async {
        guard !hasSession, !isChangingCapture, AppModel.shared.currentCall == nil else {
            problemText = String(localized: "Stop the current recording or call before starting another recording.")
            return
        }
        guard let ownerId = try? KeychainAccountStore().load(accountKey: accountKey)?.cachedRecordingUserId else {
            problemText = String(localized: "Open TeaNode online once to verify this account before recording offline.")
            return
        }
        isChangingCapture = true
        defer { finishChangingCapture() }
        guard await AVAudioApplication.requestRecordPermission() else {
            problemText = String(localized: "Allow microphone access in Settings, then start recording again.")
            return
        }
        guard AppModel.shared.account?.accountKey == accountKey,
            (try? KeychainAccountStore().load(accountKey: accountKey)?.cachedRecordingUserId) == ownerId, AppModel.shared.currentCall == nil
        else { return }
        do {
            journal = try Self.makeJournal()
            var recording = LocalRecording(accountKey: accountKey, recordingTitle: "", recordingOrigin: "capture", recordingUserId: ownerId)
            try await journal?.create(recording)
            localRecording = recording
            elapsedMS = 0
            startedUptime = ProcessInfo.processInfo.systemUptime
            timelineAtStartMS = 0
            liveStream.reset()
            _ = ensureRecordingActivity(canCreate: true)
            try await startSegment()
            recording = localRecording ?? recording
            observeInterruptions()
            startClock()
            startWatchingLevel()
            startLiveStream()
            kickUploads()
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
        stopLiveWords()
        do {
            updateElapsed()
            try await finishSegment()
            guard var recording = localRecording else { return }
            try recording.pause(at: max(elapsedMS, recording.timelineDurationMS), gapReason: isInterruption ? "interrupted" : "paused")
            localRecording = try await persist(recording)
            try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
            kickUploads()
            await updateActivity()
        } catch { await fail(error) }
    }

    func resume(localRecordingId: String? = nil, requiresExistingActivity: Bool = false) async {
        guard let current = localRecording, localRecordingId == nil || current.id.uuidString == localRecordingId,
            ["paused", "interrupted"].contains(current.recordingCaptureState), !isChangingCapture,
            AppModel.shared.currentCall == nil
        else { return }
        guard let retained = try? KeychainAccountStore().load(accountKey: current.accountKey),
            current.belongsTo(accountKey: retained.accountKey, recordingUserId: retained.cachedRecordingUserId)
        else { return }
        if !ensureRecordingActivity(canCreate: !requiresExistingActivity), requiresExistingActivity {
            problemText = String(localized: "Open TeaNode to resume this recording.")
            return
        }
        isChangingCapture = true
        defer { finishChangingCapture() }
        do {
            updateElapsed()
            try await finishSegment()
            guard var recording = localRecording else { return }
            try recording.resume(at: elapsedMS)
            localRecording = try await persist(recording)
            try await startSegment()
            startClock()
            startWatchingLevel()
            startLiveStream()
            kickUploads()
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
        stopLiveWords()
        do {
            updateElapsed()
            do { try await finishSegment() } catch { problemText = error.localizedDescription }
            guard var recording = localRecording else { return }
            try recording.stop(at: max(elapsedMS, recording.timelineDurationMS))
            localRecording = try await persist(recording)
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
            guard let ownerId = try KeychainAccountStore().load(accountKey: accountKey)?.cachedRecordingUserId else { return }
            let recovered: LocalRecording?
            if let localRecordingId {
                recovered = try await journal?.read(localRecordingId, accountKey: accountKey)
            } else {
                recovered = try await journal?.recordings(accountKey: accountKey).first(where: {
                    $0.recordingOrigin == "capture" && $0.recordingCaptureState != "stopped"
                        && $0.belongsTo(accountKey: accountKey, recordingUserId: ownerId)
                })
            }
            guard var recording = recovered,
                recording.belongsTo(accountKey: accountKey, recordingUserId: ownerId)
            else { return }
            localRecording = recording
            elapsedMS = max(recording.timelineDurationMS, Int64(max(0, Date().timeIntervalSince(recording.startedAt)) * 1000))
            if recording.localMedia.contains(where: { $0.originalSha256 == nil }) {
                try await finishSegment()
                recording = localRecording ?? recording
            }
            if recording.recordingCaptureState == "active" {
                try recording.pause(at: recording.timelineDurationMS, gapReason: "interrupted")
                recording = try await persist(recording)
            }
            localRecording = recording
            startedUptime = ProcessInfo.processInfo.systemUptime
            timelineAtStartMS = elapsedMS
            observeInterruptions()
            startClock()
            kickUploads()
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
            return localRecording?.id.uuidString == localRecordingId
                && retainedAccounts.contains {
                    localRecording?.belongsTo(accountKey: $0.accountKey, recordingUserId: $0.cachedRecordingUserId) == true
                }
        }
        do {
            let journal = try Self.makeJournal()
            guard
                let recovered = try await journal.recoverableRecording(
                    localRecordingId: localRecordingId,
                    retainedOwners: Dictionary(
                        uniqueKeysWithValues: retainedAccounts.compactMap { account in account.cachedRecordingUserId.map { (account.accountKey, $0) }
                        }))
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
            if pendingTransitions.shouldStartNextSegment {
                try await startSegment()
                kickUploads()
            }
        } catch { await fail(error) }
    }

    private func startSegment() async throws {
        guard var recording = localRecording, let journal else { throw TeaNodeError.unreadableAnswer }
        guard let retained = try KeychainAccountStore().load(accountKey: recording.accountKey),
            recording.belongsTo(accountKey: retained.accountKey, recordingUserId: retained.cachedRecordingUserId)
        else { throw TeaNodeError.signedOut }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .default, options: [.allowBluetoothHFP])
        try session.setActive(true)
        updateElapsed()
        cutPolicy.startPiece(atMS: elapsedMS)
        let media = LocalRecordingMedia(originalFilename: "recording.caf", originalContentType: "audio/x-caf", timelineStartMS: elapsedMS)
        recording.localMedia.append(media)
        recording = try await persist(recording)
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
        recorder.isMeteringEnabled = true
        guard recorder.prepareToRecord() else {
            throw TeaNodeError.refused(String(localized: "Recording could not start. Check microphone access and available storage."))
        }
        try FileManager.default.setAttributes(
            [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication, .posixPermissions: 0o600], ofItemAtPath: original.path)
        audioRecorder = recorder
        recorderId = UUID()
        // Pieces end where the cut policy says (at 60 seconds at the latest); the recorder's own end is only a backstop.
        guard recorder.record(forDuration: 600) else {
            throw TeaNodeError.refused(String(localized: "Recording could not start. Check microphone access and available storage."))
        }
    }

    private func finishSegment() async throws {
        audioRecorder?.delegate = nil
        audioRecorder?.stop()
        audioRecorder = nil
        recorderId = nil
        guard var recording = localRecording, let journal else { return }
        let pendingMediaIds = recording.localMedia.filter { $0.originalSha256 == nil }.map(\.id)
        for mediaId in pendingMediaIds {
            guard let mediaIndex = recording.localMedia.firstIndex(where: { $0.id == mediaId }) else { continue }
            let media = recording.localMedia[mediaIndex]
            let original = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: recording.accountKey)
            guard FileManager.default.fileExists(atPath: original.path) else {
                recording.localMedia.remove(at: mediaIndex)
                continue
            }
            if let audio = try? AVAudioFile(forReading: original), audio.fileFormat.sampleRate > 0 {
                recording.localMedia[mediaIndex].mediaDurationMS = RecordingClock.durationMS(
                    seconds: Double(audio.length) / audio.fileFormat.sampleRate)
            } else {
                problemText = String(localized: "This audio segment cannot be read locally. Its original is kept for recovery.")
            }
            let originalSize = try original.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
            recording.localMedia[mediaIndex].originalSizeBytes = Int64(originalSize)
            recording.localMedia[mediaIndex].originalSha256 = try await Task.detached { try Self.digest(original) }.value
            recording.timelineDurationMS = max(recording.timelineDurationMS, media.timelineStartMS + recording.localMedia[mediaIndex].mediaDurationMS)
        }
        localRecording = try await persist(recording)
    }

    nonisolated static func digest(_ original: URL) throws -> String {
        let handle = try FileHandle(forReadingFrom: original)
        defer { try? handle.close() }
        var hash = SHA256()
        while let chunk = try handle.read(upToCount: 1024 * 1024), !chunk.isEmpty { hash.update(data: chunk) }
        return hash.finalize().map { String(format: "%02x", $0) }.joined()
    }

    /// How loud the microphone hears the room now, from 0 to 1; 0 while nothing is being recorded.
    func inputLevel() -> Double {
        guard isCapturing, let audioRecorder else { return 0 }
        audioRecorder.updateMeters()
        return RecordingInputLevel.normalized(decibels: audioRecorder.averagePower(forChannel: 0))
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
                            self.localRecording = try await self.persist(recording)
                            self.finishChangingCapture()
                        } catch {
                            self.finishChangingCapture()
                            await self.pause(isInterruption: true)
                            self.problemText = error.localizedDescription
                        }
                    }
                    await self.updateActivity()
                }
                // The server learns how far the recording has come, and takes the pieces closed so far.
                if ProcessInfo.processInfo.systemUptime - self.lastUploadKickUptime >= 15 { self.kickUploads() }
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

    // MARK: Sharing the manifest, the uploads and the live words

    /// Writes what the recorder owns of the manifest, keeping what the uploader has written to it meanwhile, and gives back the
    /// recording with the uploader's side as it is now.
    private func persist(_ recording: LocalRecording) async throws -> LocalRecording {
        guard let journal else { return recording }
        let capture = recording
        let stored = try await journal.update(capture.id, accountKey: capture.accountKey) { $0.adoptCapture(from: capture) }
        var merged = capture
        merged.adoptUpload(from: stored)
        return merged
    }

    private func uploadConnection() -> Connection? {
        guard let accountKey = localRecording?.accountKey, let account = try? KeychainAccountStore().load(accountKey: accountKey) else { return nil }
        return Connection(account: account, transport: URLSessionTransport(), store: KeychainAccountStore())
    }

    /// Asks the uploader to look at the recording: it creates it on the server, reports how far it has come and uploads each
    /// piece once it is closed.
    private func kickUploads() {
        lastUploadKickUptime = ProcessInfo.processInfo.systemUptime
        guard let connection = uploadConnection() else { return }
        RecordingUploads.shared.resume(connection: connection)
    }

    /// Where on the recording's timeline it is now, without touching the clock the screen shows.
    private func timelineNowMS() -> Int64 {
        max(elapsedMS, timelineAtStartMS + Int64(max(0, ProcessInfo.processInfo.systemUptime - startedUptime) * 1000))
    }

    /// Starts the live stream as soon as the server knows the recording; the uploader creates it a moment after the start.
    private func startLiveStream() {
        liveStartTask?.cancel()
        liveStartTask = Task { [weak self] in
            while !Task.isCancelled {
                guard let self, self.isCapturing, let local = self.localRecording else { return }
                if let journal = self.journal, let stored = try? await journal.read(local.id, accountKey: local.accountKey),
                    let remoteId = stored.recordingId, let connection = self.uploadConnection()
                {
                    self.liveStream.begin(connection: connection, recordingId: remoteId, timelineMS: { [weak self] in self?.timelineNowMS() ?? 0 })
                    return
                }
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    private func stopLiveWords() {
        liveStartTask?.cancel()
        liveStartTask = nil
        levelTask?.cancel()
        levelTask = nil
        liveStream.end()
    }

    /// Reads the meter about twenty times a second for the cut policy, and closes the piece when the policy says.
    private func startWatchingLevel() {
        levelTask?.cancel()
        levelTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .milliseconds(50))
                guard !Task.isCancelled, let self else { return }
                self.watchLevel()
            }
        }
    }

    private func watchLevel() {
        guard isCapturing, let audioRecorder else { return }
        audioRecorder.updateMeters()
        cutPolicy.recordLevel(decibels: audioRecorder.averagePower(forChannel: 0), atMS: timelineNowMS())
        cutIfDue()
    }

    private func heardUtteranceEnd() {
        cutPolicy.recordUtteranceEnd(atMS: timelineNowMS())
        cutIfDue()
    }

    private func cutIfDue() {
        guard isCapturing, !isChangingCapture, !isCutting, let recorderId,
            cutPolicy.cutReason(atMS: timelineNowMS(), isStreamConnected: liveStream.isConnected) != nil
        else { return }
        isCutting = true
        Task {
            await rollover(recorderId: recorderId, wasSuccessful: true)
            isCutting = false
        }
    }

    private func fail(_ error: Error) async {
        stopLiveWords()
        audioRecorder?.delegate = nil
        audioRecorder?.stop()
        audioRecorder = nil
        recorderId = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        problemText = error.localizedDescription
        if var recording = localRecording, recording.recordingCaptureState == "active" {
            try? recording.pause(at: max(elapsedMS, recording.timelineDurationMS), gapReason: "interrupted")
            localRecording = (try? await persist(recording)) ?? recording
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
