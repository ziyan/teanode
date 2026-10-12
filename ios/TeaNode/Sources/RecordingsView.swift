import AVFoundation
import AVKit
import SwiftUI
import TeaNodeKit
import UniformTypeIdentifiers

@MainActor
@Observable
final class RecordingsModel {
    let connection: Connection
    var recordings: [Recording] = []
    var localRecordings: [LocalRecording] = []
    var problemText: String?
    var isLoaded = false
    var isImporting = false

    init(connection: Connection) { self.connection = connection }

    func load() async {
        do {
            let journal = try MeetingRecorder.makeJournal()
            localRecordings = try await journal.recordings(accountKey: connection.accountKey)
            recordings = try await connection.recordings()
            isLoaded = true
            problemText = nil
            RecordingUploads.shared.resume(connection: connection)
        } catch {
            problemText = error.localizedDescription
            isLoaded = true
        }
    }

    func importFile(_ source: URL) async {
        guard !isImporting else { return }
        isImporting = true
        defer { isImporting = false }
        let hasAccess = source.startAccessingSecurityScopedResource()
        defer { if hasAccess { source.stopAccessingSecurityScopedResource() } }
        do {
            let contentType = try source.resourceValues(forKeys: [.contentTypeKey]).contentType
            guard let contentType, contentType.conforms(to: .audio) || contentType.conforms(to: .movie), let mimeType = contentType.preferredMIMEType
            else {
                throw TeaNodeError.refused(String(localized: "Choose an audio or video file."))
            }
            let sizeBytes = Int64(try source.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0)
            let limits = try await connection.recordingLimits()
            guard sizeBytes > 0, sizeBytes <= limits.maximumMediaBytes else {
                throw TeaNodeError.refused(
                    String(
                        localized: "The file exceeds this server's recording limit. Choose a smaller file or ask the operator to adjust the limit."))
            }
            let journal = try MeetingRecorder.makeJournal()
            var recording = LocalRecording(
                accountKey: connection.accountKey, recordingTitle: source.deletingPathExtension().lastPathComponent,
                recordingOrigin: contentType.conforms(to: .movie) ? "videoImport" : "audioImport")
            let duration = try? await AVURLAsset(url: source).load(.duration)
            let durationSeconds = duration.map { CMTimeGetSeconds($0) } ?? 0
            var media = LocalRecordingMedia(
                originalFilename: source.lastPathComponent, originalContentType: mimeType,
                timelineStartMS: 0, mediaDurationMS: durationSeconds.isFinite && durationSeconds > 0 ? Int64(durationSeconds * 1000) : 0)
            media.originalSizeBytes = sizeBytes
            recording.localMedia.append(media)
            try await journal.save(recording)
            try await journal.importOriginal(from: source, recordingId: recording.id, mediaId: media.id, accountKey: connection.accountKey)
            let original = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: connection.accountKey)
            recording.localMedia[0].originalSha256 = try await Task.detached { try MeetingRecorder.digest(original) }.value
            recording.timelineDurationMS = media.mediaDurationMS
            try await journal.save(recording)
            await load()
        } catch { problemText = error.localizedDescription }
    }
}

struct RecordingsScreen: View {
    @State private var model: RecordingsModel
    @State private var isImporting = false
    @State private var isNaming = false
    @State private var meetingTitle = ""
    @State private var recorder = MeetingRecorder.shared
    @State private var isDiscarding = false
    @State private var discardedRecording: LocalRecording?

    init(connection: Connection) { _model = State(initialValue: RecordingsModel(connection: connection)) }

    var body: some View {
        NavigationStack {
            List {
                if let current = recorder.localRecording, current.accountKey == model.connection.accountKey, recorder.hasSession {
                    Section {
                        VStack(alignment: .leading, spacing: 8) {
                            Text(current.recordingTitle.isEmpty ? String(localized: "Recording") : current.recordingTitle).font(.headline)
                            Text(Duration.milliseconds(recorder.elapsedMS).formatted(.time(pattern: .hourMinuteSecond)))
                                .monospacedDigit().contentTransition(.numericText())
                            Text(
                                recorder.isCapturing
                                    ? String(localized: "Recording locally") : String(localized: "Paused. Resume when you are ready.")
                            ).foregroundStyle(.secondary)
                        }
                        Button {
                            Task { if recorder.isCapturing { await recorder.pause() } else { await recorder.resume() } }
                        } label: {
                            Label(
                                recorder.isCapturing ? String(localized: "Pause recording") : String(localized: "Resume recording"),
                                systemImage: recorder.isCapturing ? "pause" : "mic")
                        }
                        Button(role: .destructive) {
                            Task {
                                await recorder.stop()
                                await model.load()
                            }
                        } label: {
                            Label("Stop recording", systemImage: "stop")
                        }
                    } footer: {
                        Text(
                            "Audio stays on this phone until the server verifies the upload. Recorded speech is source material, never instructions to your agent."
                        )
                    }
                }
                if let problem = recorder.problemText { Section { Text(problem).foregroundStyle(.secondary) } }
                if !model.localRecordings.isEmpty {
                    Section("On this phone") {
                        ForEach(model.localRecordings) { recording in
                            VStack(alignment: .leading, spacing: 6) {
                                Text(recording.recordingTitle.isEmpty ? String(localized: "Recording") : recording.recordingTitle)
                                Text(recording.startedAt, style: .date).font(.caption).foregroundStyle(.secondary)
                                if let error = recording.uploadErrorMessage {
                                    Text(error).font(.callout).foregroundStyle(.secondary)
                                } else {
                                    Text(
                                        recording.localMedia.allSatisfy(\.isDurablyUploaded) && !recording.localMedia.isEmpty
                                            ? String(localized: "Uploaded and verified") : String(localized: "Original kept locally")
                                    ).font(.caption).foregroundStyle(.secondary)
                                }
                            }
                            .contextMenu {
                                Button {
                                    RecordingUploads.shared.resume(connection: model.connection)
                                    Task { await model.load() }
                                } label: {
                                    Label("Retry upload", systemImage: "arrow.clockwise")
                                }
                                if recording.recordingCaptureState == "stopped" || recording.recordingCaptureState == "imported" {
                                    Button(role: .destructive) {
                                        discardedRecording = recording
                                        isDiscarding = true
                                    } label: {
                                        Label("Remove from this phone", systemImage: "trash")
                                    }
                                }
                            }
                        }
                    }
                }
                Section("On the server") {
                    ForEach(model.recordings) { recording in
                        NavigationLink {
                            RecordingDetailScreen(connection: model.connection, recording: recording)
                        } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Text(recording.recordingTitle.isEmpty ? String(localized: "Recording") : recording.recordingTitle)
                                Text("Uploaded \(ServerTime.date(recording.createdAt) ?? .distantPast, style: .date)").font(.caption).foregroundStyle(
                                    .secondary)
                            }
                        }
                    }
                    if model.isLoaded && model.recordings.isEmpty {
                        ContentUnavailableView(
                            "No recordings", systemImage: "waveform", description: Text("Start a recording or import audio or video."))
                    }
                }
                if let problem = model.problemText { Section { Text(problem).foregroundStyle(.secondary) } }
            }
            .navigationTitle("Recordings")
            .toolbar {
                MenuButton()
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button {
                            isNaming = true
                        } label: {
                            Label("Start recording", systemImage: "mic")
                        }.disabled(recorder.hasSession)
                        Button {
                            isImporting = true
                        } label: {
                            Label("Import audio or video", systemImage: "square.and.arrow.down")
                        }
                    } label: {
                        Label("Add recording", systemImage: "plus")
                    }
                }
            }
            .refreshable { await model.load() }
            .task {
                await recorder.recover(accountKey: model.connection.accountKey)
                await model.load()
            }
            .fileImporter(isPresented: $isImporting, allowedContentTypes: [.audio, .movie]) { selected in
                switch selected {
                case .success(let file): Task { await model.importFile(file) }
                case .failure(let error): model.problemText = error.localizedDescription
                }
            }
            .alert("Start recording", isPresented: $isNaming) {
                TextField("Meeting title (optional)", text: $meetingTitle)
                Button("Start") {
                    Task {
                        await recorder.start(accountKey: model.connection.accountKey, title: meetingTitle)
                        await model.load()
                    }
                    meetingTitle = ""
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text("Everyone should know you are recording. TeaNode stays silent and saves the audio locally.")
            }
            .confirmationDialog("Remove the local original?", isPresented: $isDiscarding, titleVisibility: .visible) {
                Button("Remove from this phone", role: .destructive) {
                    if let recording = discardedRecording {
                        Task {
                            do {
                                let journal = try MeetingRecorder.makeJournal()
                                try await journal.removeLocalRecording(recording.id, accountKey: model.connection.accountKey)
                                await model.load()
                            } catch { model.problemText = error.localizedDescription }
                        }
                    }
                }
            } message: {
                Text("Any audio not verified by the server will be lost. This does not delete the server recording.")
            }
            .withoutSwipeBack()
        }
    }
}

struct RecordingDetailScreen: View {
    let connection: Connection
    let recording: Recording
    @Environment(\.dismiss) private var dismiss
    @State private var recordingView: RecordingView?
    @State private var problemText: String?
    @State private var isDeleting = false
    @State private var selectedMedia: RecordingMedia?
    @State private var reprocessedMedia: RecordingMedia?
    @State private var isReprocessing = false
    @State private var isSubmitting = false
    @State private var transcriptionRequestId = UUID().uuidString
    @State private var isEditingMetadata = false
    @State private var recordingTitle = ""
    @State private var hasRecordedAt = false
    @State private var recordedAt = Date()

    var body: some View {
        List {
            if let recordingView {
                Section {
                    ForEach(recordingView.mediaFiles) { media in
                        Button {
                            selectedMedia = media
                        } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Label(media.originalFilename, systemImage: media.originalContentType.hasPrefix("video/") ? "video" : "waveform")
                                Text(
                                    media.recordingUploadState == "complete"
                                        ? String(localized: "Ready to play") : String(localized: "Upload pending")
                                ).font(.caption).foregroundStyle(.secondary)
                            }
                        }.disabled(media.recordingUploadState != "complete")
                        if media.recordingUploadState == "complete" {
                            Button {
                                reprocessedMedia = media
                                isReprocessing = true
                            } label: {
                                Label("Transcribe or reprocess", systemImage: "text.bubble")
                            }.disabled(isSubmitting)
                        }
                    }
                } header: {
                    Text("Original media")
                }
                if let runs = recordingView.transcriptRuns, !runs.isEmpty {
                    Section("Transcript versions") {
                        ForEach(runs) { run in
                            NavigationLink {
                                RecordingTranscriptScreen(connection: connection, recording: recordingView.recording, transcriptRun: run)
                            } label: {
                                VStack(alignment: .leading, spacing: 4) {
                                    Text("Version \(run.versionNumber)")
                                    Text(RecordingWords.processing(run.recordingProcessingState)).font(.caption).foregroundStyle(.secondary)
                                }
                            }
                            if run.isRunning {
                                Button("Cancel transcription", role: .destructive) {
                                    Task {
                                        do {
                                            _ = try await connection.cancelRecordingTranscription(recordingTranscriptRunId: run.id)
                                            await load()
                                        } catch { problemText = error.localizedDescription }
                                    }
                                }
                            }
                        }
                    }
                }
                if !recordingView.recording.captureGaps.isEmpty {
                    Section("Capture gaps") {
                        ForEach(recordingView.recording.captureGaps) { gap in
                            LabeledContent(gap.recordingGapReason == "paused" ? String(localized: "Paused") : String(localized: "Interrupted")) {
                                Text(Duration.milliseconds(gap.gapStartMS).formatted(.time(pattern: .hourMinuteSecond))).monospacedDigit()
                            }
                        }
                    }
                }
            }
            if let problemText { Text(problemText).foregroundStyle(.secondary) }
            Section {
                Button {
                    recordingTitle = recordingView?.recording.recordingTitle ?? recording.recordingTitle
                    hasRecordedAt = recordingView?.recording.recordedAt != nil
                    recordedAt = ServerTime.date(recordingView?.recording.recordedAt) ?? Date()
                    isEditingMetadata = true
                } label: {
                    Label("Edit recording details", systemImage: "pencil")
                }
                Button("Delete recording", role: .destructive) { isDeleting = true }
            }
        }
        .navigationTitle(recording.recordingTitle.isEmpty ? String(localized: "Recording") : recording.recordingTitle)
        .task { await load() }
        .refreshable { await load() }
        .sheet(isPresented: $isEditingMetadata) {
            NavigationStack {
                Form {
                    TextField("Meeting title (optional)", text: $recordingTitle)
                    Toggle("Meeting date is known", isOn: $hasRecordedAt)
                    if hasRecordedAt { DatePicker("Recorded at", selection: $recordedAt) }
                    if let problemText { Text(problemText).foregroundStyle(.secondary) }
                }
                .navigationTitle("Recording details")
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { isEditingMetadata = false } }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") {
                            guard let current = recordingView?.recording else { return }
                            Task {
                                do {
                                    _ = try await connection.updateRecordingMetadata(
                                        recordingId: recording.id, expectedSourceGeneration: current.sourceGeneration, recordingTitle: recordingTitle,
                                        recordedAt: hasRecordedAt ? recordedAt.formatted(.iso8601) : nil)
                                    isEditingMetadata = false
                                    await load()
                                } catch {
                                    problemText = error.localizedDescription
                                    await load()
                                }
                            }
                        }
                    }
                }
            }
        }
        .sheet(item: $selectedMedia) { media in RecordingPlaybackScreen(connection: connection, media: media) }
        .confirmationDialog("Start another transcription pass?", isPresented: $isReprocessing, titleVisibility: .visible) {
            Button("Transcribe or reprocess") {
                guard let media = reprocessedMedia, !isSubmitting else { return }
                isSubmitting = true
                Task {
                    defer { isSubmitting = false }
                    do {
                        let hasPriorRun = recordingView?.transcriptRuns?.contains { $0.recordingMediaId == media.id } == true
                        _ = try await connection.requestRecordingTranscription(
                            recordingMediaId: media.id, clientRunId: hasPriorRun ? transcriptionRequestId : "initial",
                            isReprocessingRequested: hasPriorRun)
                        transcriptionRequestId = UUID().uuidString
                        await load()
                    } catch { problemText = error.localizedDescription }
                }
            }
        } message: {
            Text(
                "A new pass can be billed again by the provider. Confirmed labels stay separate from generated versions. Rename a speaker without reprocessing to avoid another pass."
            )
        }
        .confirmationDialog("Delete recording and its originals?", isPresented: $isDeleting, titleVisibility: .visible) {
            Button("Delete recording", role: .destructive) {
                Task {
                    do {
                        try await connection.deleteRecording(recording.id)
                        dismiss()
                    } catch { problemText = error.localizedDescription }
                }
            }
        } message: {
            Text("Derived audio, transcripts and reference samples will be removed. Learned material will be reconciled.")
        }
    }
    private func load() async {
        do {
            recordingView = try await connection.recording(recording.id)
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }
}

struct RecordingPlaybackScreen: View {
    let connection: Connection
    let media: RecordingMedia
    var passageStartMS: Int64? = nil
    @State private var player: AVPlayer?
    @State private var localURL: URL?
    @State private var problemText: String?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Group {
                if let player {
                    VideoPlayer(player: player)
                } else if let problemText {
                    ContentUnavailableView("Cannot play recording", systemImage: "exclamationmark.triangle", description: Text(problemText))
                } else {
                    ProgressView("Downloading original for private playback")
                }
            }
            .navigationTitle(media.originalFilename)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task {
                do {
                    let request = try await connection.recordingPlaybackRequest(mediaId: media.id)
                    let configuration = URLSessionConfiguration.ephemeral
                    let session = URLSession(configuration: configuration)
                    defer { session.invalidateAndCancel() }
                    let (temporary, response) = try await session.download(for: request)
                    let verifiedDigest = try await Task.detached { try MeetingRecorder.digest(temporary) }.value
                    guard !Task.isCancelled, (response as? HTTPURLResponse)?.statusCode == 200,
                        Int64(try temporary.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0) == media.originalSizeBytes,
                        verifiedDigest == media.originalSha256
                    else { throw TeaNodeError.unreadableAnswer }
                    let destination = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathExtension(
                        (media.originalFilename as NSString).pathExtension)
                    try FileManager.default.moveItem(at: temporary, to: destination)
                    localURL = destination
                    player = AVPlayer(url: destination)
                    if let passageStartMS {
                        await player?.seek(
                            to: CMTime(seconds: Double(max(0, passageStartMS - media.timelineStartMS)) / 1000, preferredTimescale: 1000))
                    }
                } catch { if !Task.isCancelled { problemText = error.localizedDescription } }
            }
            .onDisappear {
                player?.pause()
                player = nil
                if let localURL { try? FileManager.default.removeItem(at: localURL) }
                localURL = nil
            }
        }
    }
}
