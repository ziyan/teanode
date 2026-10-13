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
    var hasUnverifiedLocalOwner = false
    var isLoaded = false
    var isImporting = false
    var hasMoreRecordings = false
    var isLoadingPage = false

    init(connection: Connection) { self.connection = connection }

    func load() async {
        do {
            if (try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId) == nil {
                _ = try? await connection.recordingOwnerId()
            }
            await loadLocalRecordings()
            recordings = try await connection.recordings()
            hasMoreRecordings = recordings.count == 50
            isLoaded = true
            problemText = nil
            RecordingUploads.shared.resume(connection: connection)
        } catch {
            problemText = error.localizedDescription
            isLoaded = true
        }
    }

    var hasPendingLocalUploads: Bool { !RecordingLibraryProgress.pendingLocalRecordingIds(localRecordings).isEmpty }

    private func isCurrentOwner(_ recordingUserId: String) -> Bool {
        AppModel.shared.connection === connection
            && RecordingLibraryProgress.isCurrentOwner(
                accountKey: connection.accountKey, recordingUserId: recordingUserId,
                currentAccountKey: AppModel.shared.connection?.accountKey,
                currentRecordingUserId: AppModel.shared.account?.cachedRecordingUserId, isCancelled: Task.isCancelled)
            && (try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId) == recordingUserId
    }

    private func loadLocalRecordings() async {
        guard let ownerId = try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId,
            isCurrentOwner(ownerId)
        else { return }
        do {
            let journal = try MeetingRecorder.makeJournal()
            let kept = try await journal.recordings(accountKey: connection.accountKey)
            guard isCurrentOwner(ownerId) else { return }
            localRecordings = kept.filter { $0.belongsTo(accountKey: connection.accountKey, recordingUserId: ownerId) }
            hasUnverifiedLocalOwner = localRecordings.count != kept.count
        } catch {
            if isCurrentOwner(ownerId) { localRecordings = [] }
        }
    }

    func refreshUploadProgress() async {
        guard let ownerId = try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId,
            isCurrentOwner(ownerId)
        else { return }
        let pendingIds = RecordingLibraryProgress.pendingLocalRecordingIds(localRecordings)
        await loadLocalRecordings()
        guard isCurrentOwner(ownerId) else { return }
        for local in localRecordings where pendingIds.contains(local.id) {
            guard isCurrentOwner(ownerId) else { return }
            guard let recordingId = local.recordingId, !recordings.contains(where: { $0.id == recordingId }) else { continue }
            let published = try? await connection.recording(recordingId).recording
            guard isCurrentOwner(ownerId) else { return }
            guard let published else { continue }
            recordings = RecordingLibraryProgress.mergingPublishedRecording(
                published, into: recordings, from: local, accountKey: connection.accountKey, recordingUserId: ownerId,
                currentAccountKey: AppModel.shared.connection?.accountKey, currentRecordingUserId: AppModel.shared.account?.cachedRecordingUserId,
                hasMoreRecordings: hasMoreRecordings, isCancelled: Task.isCancelled)
        }
    }

    func loadMore() async {
        guard hasMoreRecordings, !isLoadingPage, let beforeRecordingId = recordings.last?.id else { return }
        isLoadingPage = true
        defer { isLoadingPage = false }
        do {
            let page = try await connection.recordings(beforeRecordingId: beforeRecordingId)
            let knownIds = Set(recordings.map(\.id))
            recordings.append(contentsOf: page.filter { !knownIds.contains($0.id) })
            hasMoreRecordings = page.count == 50 && page.last?.id != beforeRecordingId
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }

    func importFile(_ source: URL) async {
        guard !isImporting else { return }
        isImporting = true
        defer { isImporting = false }
        let hasAccess = source.startAccessingSecurityScopedResource()
        defer { if hasAccess { source.stopAccessingSecurityScopedResource() } }
        do {
            guard let ownerId = try KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId else {
                throw TeaNodeError.refused(String(localized: "Open TeaNode online once to verify this account before recording offline."))
            }
            let contentType = try source.resourceValues(forKeys: [.contentTypeKey]).contentType
            guard let contentType, contentType.conforms(to: .audio) || contentType.conforms(to: .movie), let mimeType = contentType.preferredMIMEType
            else {
                throw TeaNodeError.refused(String(localized: "Choose an audio or video file."))
            }
            let sizeBytes = Int64(try source.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0)
            let limits = try? await connection.recordingLimits()
            guard sizeBytes > 0, limits.map { sizeBytes <= $0.maximumMediaBytes } ?? true else {
                throw TeaNodeError.refused(
                    String(
                        localized: "The file exceeds this server's recording limit. Choose a smaller file or ask the operator to adjust the limit."))
            }
            let journal = try MeetingRecorder.makeJournal()
            guard try KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId == ownerId else {
                throw TeaNodeError.signedOut
            }
            var recording = LocalRecording(
                accountKey: connection.accountKey,
                recordingTitle: source.deletingPathExtension().lastPathComponent.utf8.count <= 255
                    ? source.deletingPathExtension().lastPathComponent : String(localized: "Recording"),
                recordingOrigin: contentType.conforms(to: .movie) ? "videoImport" : "audioImport", recordingUserId: ownerId)
            let duration = try? await AVURLAsset(url: source).load(.duration)
            let durationSeconds = duration.map { CMTimeGetSeconds($0) } ?? 0
            var media = LocalRecordingMedia(
                originalFilename: source.lastPathComponent, originalContentType: mimeType,
                timelineStartMS: 0, mediaDurationMS: RecordingClock.durationMS(seconds: durationSeconds))
            media.originalSizeBytes = sizeBytes
            recording.localMedia.append(media)
            guard try KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId == ownerId else {
                throw TeaNodeError.signedOut
            }
            try await journal.create(recording)
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
    @State private var localPlayback: LocalRecording?
    @State private var openedCitation: RecordingLink?

    init(connection: Connection) { _model = State(initialValue: RecordingsModel(connection: connection)) }

    var body: some View {
        NavigationStack {
            List {
                if let current = recorder.localRecording, recorder.hasSession {
                    Section {
                        VStack(alignment: .leading, spacing: 8) {
                            Text(
                                current.belongsTo(
                                    accountKey: model.connection.accountKey, recordingUserId: AppModel.shared.account?.cachedRecordingUserId)
                                    ? (current.recordingTitle.isEmpty ? String(localized: "Recording") : current.recordingTitle)
                                    : String(localized: "Recording on this phone")
                            ).font(.headline)
                            Text(Duration.milliseconds(recorder.elapsedMS).formatted(.time(pattern: .hourMinuteSecond)))
                                .monospacedDigit().contentTransition(.numericText())
                            Text(
                                recorder.isCapturing
                                    ? String(localized: "Recording locally")
                                    : (current.recordingCaptureState == "interrupted"
                                        ? String(localized: "Interrupted. Resume when you are ready.")
                                        : String(localized: "Paused. Resume when you are ready."))
                            ).foregroundStyle(.secondary)
                        }
                        Button {
                            Task { if recorder.isCapturing { await recorder.pause() } else { await recorder.resume() } }
                        } label: {
                            Label(
                                recorder.isCapturing ? String(localized: "Pause recording") : String(localized: "Resume recording"),
                                systemImage: recorder.isCapturing ? "pause" : "mic")
                        }
                        .disabled(
                            !recorder.isCapturing
                                && !current.belongsTo(
                                    accountKey: model.connection.accountKey, recordingUserId: AppModel.shared.account?.cachedRecordingUserId)
                        )
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
                                if RecordingLibraryProgress.canPlayLocalOriginal(recording) {
                                    Button {
                                        localPlayback = recording
                                    } label: {
                                        Label("Play local original", systemImage: "play")
                                    }
                                }
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
                    if model.hasMoreRecordings {
                        Button {
                            Task { await model.loadMore() }
                        } label: {
                            Label("Load more recordings", systemImage: "chevron.down")
                        }
                        .disabled(model.isLoadingPage)
                    }
                    if model.isLoaded && model.recordings.isEmpty {
                        ContentUnavailableView(
                            "No recordings", systemImage: "waveform", description: Text("Start a recording or import audio or video."))
                    }
                }
                if model.hasUnverifiedLocalOwner {
                    Section {
                        Text(
                            "Some local recordings have a different or unverified owner. They are kept on this phone and will not be uploaded automatically."
                        ).foregroundStyle(.secondary)
                    }
                }
                if let problem = model.problemText { Section { Text(problem).foregroundStyle(.secondary) } }
            }
            .navigationTitle("Recordings")
            .navigationDestination(item: $openedCitation) { citation in
                RecordingCitationScreen(connection: model.connection, citation: citation)
            }
            .onChange(of: AppModel.shared.openedRecordingLink, initial: true) { _, citation in
                guard let citation, AppModel.shared.openedRecordingAccountKey == model.connection.accountKey else { return }
                AppModel.shared.openedRecordingLink = nil
                openedCitation = citation
            }
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
                while !Task.isCancelled {
                    do { try await Task.sleep(for: .seconds(5)) } catch { break }
                    if model.hasPendingLocalUploads { await model.refreshUploadProgress() }
                }
            }
            .sheet(item: $localPlayback) { recording in
                RecordingLocalPlaybackScreen(recording: recording, accountKey: model.connection.accountKey)
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
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let connection: Connection
    let recording: Recording
    var shouldFocusLearning = false
    @Environment(\.dismiss) private var dismiss
    @State private var recordingView: RecordingView?
    @State private var learning: RecordingLearningView?
    @State private var editingLearningSource: KnowledgeSource?
    @State private var limits: RecordingLimits?
    @State private var problemText: String?
    @State private var isDeleting = false
    @State private var selectedMedia: RecordingMedia?
    @State private var reprocessedMedia: RecordingMedia?
    @State private var isReprocessing = false
    @State private var isSubmitting = false
    @State private var transcriptionRequestId = UUID().uuidString
    @State private var isEditingMetadata = false
    @State private var isSavingMetadata = false
    @State private var recordingTitle = ""
    @State private var metadataExpectedGeneration: Int64 = 0
    @State private var hasRecordedAt = false
    @State private var recordedAt = Date()

    private var displayedRecordingTitle: String {
        let latestTitle = recordingView?.recording.recordingTitle ?? recording.recordingTitle
        return latestTitle.isEmpty ? String(localized: "Recording") : latestTitle
    }

    var body: some View {
        ScrollViewReader { scrollProxy in
            List {
                if dynamicTypeSize.isAccessibilitySize {
                    Section {
                        Text(displayedRecordingTitle).font(.headline)
                    }
                }
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
                                    transcriptionRequestId = RecordingRequestIdentity.acquire(accountKey: connection.accountKey, mediaId: media.id)
                                    isReprocessing = true
                                } label: {
                                    Label("Transcribe or reprocess", systemImage: "text.bubble")
                                }.disabled(isSubmitting || limits?.isTranscriptionAvailable != true)
                            }
                        }
                    } header: {
                        Text("Original media")
                    }
                    NavigationLink {
                        RecordingTranscriptScreen(connection: connection, recording: recordingView.recording)
                    } label: {
                        Label("Full recording transcript", systemImage: "text.alignleft")
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
                                if ["queued", "preparing", "transcribing", "failed", "canceled"].contains(run.recordingProcessingState) {
                                    Button {
                                        Task {
                                            do {
                                                _ = try await connection.retryRecordingTranscription(recordingTranscriptRunId: run.id)
                                                await load()
                                            } catch { problemText = error.localizedDescription }
                                        }
                                    } label: {
                                        Label("Retry transcription", systemImage: "arrow.clockwise")
                                    }.disabled(limits?.isTranscriptionAvailable != true)
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
                    if limits?.isTranscriptionAvailable != true {
                        Section {
                            Text(
                                limits?.transcriptionUnavailableMessage?.isEmpty == false
                                    ? (limits?.transcriptionUnavailableMessage ?? "")
                                    : String(localized: "Transcription availability has not been verified. Refresh or ask the operator.")
                            ).foregroundStyle(.secondary)
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
                if let learning {
                    Section {
                        if learning.hasStoppedReading == true {
                            Text("Stopped reading transcripts").foregroundStyle(.secondary)
                            Button("Resume reading", systemImage: "play") {
                                isSubmitting = true
                                Task {
                                    defer { isSubmitting = false }
                                    do {
                                        self.learning = try await connection.resumeRecordingLearning(
                                            recordingId: recording.id, expectedSourceGeneration: learning.sourceGeneration)
                                        await load()
                                    } catch { problemText = error.localizedDescription }
                                }
                            }.disabled(isSubmitting)
                            Text("Previously learned pages remain. Transcript corrections still reconcile outdated material.").font(.callout)
                                .foregroundStyle(.secondary)
                        } else if !learning.hasAgent {
                            Text("Enable your personal agent to learn from transcripts.").foregroundStyle(.secondary)
                        } else if let source = learning.knowledgeSource {
                            LabeledContent("Passages waiting", value: learning.pendingDocumentCount.formatted())
                            LabeledContent("Processed passages", value: learning.digestedDocumentCount.formatted())
                            if let lastRunAt = ServerTime.date(source.lastRunAt) {
                                LabeledContent("Last source pass") { Text(lastRunAt, style: .date) }
                            }
                            if let nextRunAt = ServerTime.date(source.nextRunAt) {
                                LabeledContent("Next source pass") { Text(nextRunAt, style: .relative) }
                            } else if source.cron.isEmpty {
                                Text("Reads only when you ask.").foregroundStyle(.secondary)
                            }
                            if !source.lastError.isEmpty { Text(source.lastError).foregroundStyle(.secondary) }
                            NavigationLink {
                                KnowledgeSourcesView(connection: connection, highlightedSourceId: source.id)
                            } label: {
                                Label("Knowledge source", systemImage: "brain")
                            }
                            Button("Edit this source", systemImage: "pencil") { editingLearningSource = source }
                        } else {
                            Text("Waiting for a completed transcript").foregroundStyle(.secondary)
                        }
                        if learning.isCurationPending { Text("Voice reference curation pending").foregroundStyle(.secondary) }
                    } header: {
                        Text("Learning")
                    } footer: {
                        Text(
                            "Learning uses the existing knowledge and dreaming pipeline without an approval step. Processed passages show source progress, not proof of useful memories."
                        )
                    }.id("recording-learning")
                }
                if let problemText { Text(problemText).foregroundStyle(.secondary) }
                Section {
                    Button {
                        recordingTitle = recordingView?.recording.recordingTitle ?? recording.recordingTitle
                        metadataExpectedGeneration = recordingView?.recording.sourceGeneration ?? recording.sourceGeneration
                        hasRecordedAt = recordingView?.recording.recordedAt != nil
                        recordedAt = ServerTime.date(recordingView?.recording.recordedAt) ?? Date()
                        isEditingMetadata = true
                    } label: {
                        Label("Edit recording details", systemImage: "pencil")
                    }
                    Button("Delete recording", role: .destructive) { isDeleting = true }
                }
            }
            .task(id: learning) {
                guard shouldFocusLearning, learning != nil else { return }
                try? await Task.sleep(for: .milliseconds(200))
                scrollProxy.scrollTo("recording-learning", anchor: .top)
            }
        }
        .navigationTitle(displayedRecordingTitle)
        .navigationBarTitleDisplayMode(.inline)
        .task {
            await load()
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(5))
                guard !Task.isCancelled else { return }
                if !isEditingMetadata && !isReprocessing && !isSubmitting,
                    recordingView?.transcriptRuns?.contains(where: \.isRunning) == true || learning?.isCurationPending == true
                {
                    await load()
                }
            }
        }
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
                .navigationBarTitleDisplayMode(.inline)
                .disabled(isSavingMetadata)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { isEditingMetadata = false } }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") {
                            Task {
                                guard !isSavingMetadata else { return }
                                isSavingMetadata = true
                                defer { isSavingMetadata = false }
                                do {
                                    _ = try await connection.updateRecordingMetadata(
                                        recordingId: recording.id, expectedSourceGeneration: metadataExpectedGeneration,
                                        recordingTitle: recordingTitle,
                                        recordedAt: hasRecordedAt ? recordedAt.formatted(.iso8601) : nil)
                                    isEditingMetadata = false
                                    await load()
                                } catch {
                                    let failure = error.localizedDescription
                                    await load()
                                    problemText = failure
                                }
                            }
                        }
                    }
                }
            }
        }
        .sheet(item: $selectedMedia) { media in RecordingPlaybackScreen(connection: connection, media: media) }
        .sheet(item: $editingLearningSource) { source in
            KnowledgeSourceEditor(connection: connection, source: source, sourceTypes: [], mailboxes: []) { _ in
                Task { await load() }
            }
        }
        .confirmationDialog("Start another transcription pass?", isPresented: $isReprocessing, titleVisibility: .visible) {
            Button("Transcribe or reprocess") {
                guard let media = reprocessedMedia, !isSubmitting, limits?.isTranscriptionAvailable == true else { return }
                isSubmitting = true
                Task {
                    defer { isSubmitting = false }
                    do {
                        let hasPriorRun = recordingView?.transcriptRuns?.contains { $0.recordingMediaId == media.id } == true
                        _ = try await connection.requestRecordingTranscription(
                            recordingMediaId: media.id, clientRunId: hasPriorRun ? transcriptionRequestId : "initial",
                            isReprocessingRequested: hasPriorRun)
                        RecordingRequestIdentity.finish(accountKey: connection.accountKey, mediaId: media.id, requestId: transcriptionRequestId)
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
            limits = try await connection.recordingLimits()
            learning = try await connection.recordingLearning(recordingId: recording.id)
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }
}

struct RecordingPlaybackScreen: View {
    let connection: Connection
    let media: RecordingMedia
    var passageStartMS: Int64? = nil
    var passageEndMS: Int64? = nil
    @State private var player: AVPlayer?
    @State private var localURL: URL?
    @State private var problemText: String?
    @State private var isCompatibleAudioPreview = false
    @State private var recordingUserId: String?
    @State private var isPlaybackVisible = false
    @State private var isPreparingPreview = false
    @State private var previewTask: Task<Void, Never>?
    @State private var progressText = String(localized: "Downloading original for private playback")
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            Group {
                if let player {
                    VStack {
                        if isCompatibleAudioPreview {
                            Label("Compatible audio preview", systemImage: "waveform")
                            Text("Audio preview prepared locally; original unchanged.").font(.caption).foregroundStyle(.secondary)
                        }
                        VideoPlayer(player: player)
                        if let problemText { Text(problemText).font(.caption).foregroundStyle(.secondary) }
                        if !isCompatibleAudioPreview {
                            Button {
                                guard !isPreparingPreview else { return }
                                isPreparingPreview = true
                                previewTask = Task { await useAudioPreview() }
                            } label: {
                                if isPreparingPreview { ProgressView() } else { Label("Use audio preview", systemImage: "waveform") }
                            }
                            .disabled(isPreparingPreview || (try? requireCurrentConnection()) == nil)
                        }
                        if let localURL {
                            ShareLink(item: localURL) {
                                Label(
                                    isCompatibleAudioPreview
                                        ? String(localized: "Share full audio preview") : String(localized: "Share full original"),
                                    systemImage: "square.and.arrow.up")
                            }
                            .disabled(isPreparingPreview || (try? requireCurrentConnection()) == nil)
                        }
                    }
                } else if let problemText {
                    ContentUnavailableView("Cannot play recording", systemImage: "exclamationmark.triangle", description: Text(problemText))
                } else {
                    ProgressView(progressText)
                }
            }
            .navigationTitle(media.originalFilename)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .task {
                guard !Task.isCancelled else { return }
                isPlaybackVisible = true
                defer {
                    if Task.isCancelled || player == nil {
                        if let localURL { try? FileManager.default.removeItem(at: localURL) }
                        localURL = nil
                    }
                }
                do {
                    #if DEBUG
                        if DemoServer.screen == "recording-compatible-playback" || DemoServer.screen == "recording-original-playback" {
                            localURL = try DemoServer.recordingPreviewURL()
                            isCompatibleAudioPreview = DemoServer.screen == "recording-compatible-playback"
                            player = AVPlayer(url: localURL!)
                            return
                        }
                    #endif
                    recordingUserId = AppModel.shared.account?.cachedRecordingUserId
                    try requireCurrentConnection()
                    let request = try await connection.recordingPlaybackRequest(mediaId: media.id)
                    localURL = try await downloadVerifiedMedia(
                        request, expectedSizeBytes: media.originalSizeBytes, expectedSha256: media.originalSha256,
                        fileExtension: (media.originalFilename as NSString).pathExtension)
                    guard let originalURL = localURL else { throw TeaNodeError.unreadableAnswer }
                    isCompatibleAudioPreview = false
                    var playableAsset = AVURLAsset(url: originalURL)
                    let isOriginalPlayable: Bool
                    do {
                        isOriginalPlayable = try await playableAsset.load(.isPlayable)
                    } catch let assetError as AVError
                        where [.fileFormatNotRecognized, .decoderNotFound, .formatUnsupported].contains(assetError.code)
                    {
                        isOriginalPlayable = false
                    }
                    try requireCurrentConnection()
                    if !isOriginalPlayable { progressText = String(localized: "Preparing compatible audio preview") }
                    let compatiblePlayback = try await connection.compatibleRecordingPlayback(for: media, isOriginalPlayable: isOriginalPlayable)
                    try requireCurrentConnection()
                    if let playback = compatiblePlayback {
                        progressText = String(localized: "Downloading compatible audio preview")
                        let previewRequest = try await connection.recordingCompatiblePlaybackRequest(playback, for: media)
                        let previewURL = try await downloadVerifiedMedia(
                            previewRequest, expectedSizeBytes: playback.playbackSizeBytes, expectedSha256: playback.playbackSha256,
                            fileExtension: "wav")
                        try? FileManager.default.removeItem(at: originalURL)
                        localURL = previewURL
                        playableAsset = AVURLAsset(url: previewURL)
                        guard try await playableAsset.load(.isPlayable) else { throw TeaNodeError.unreadableAnswer }
                        isCompatibleAudioPreview = true
                    }
                    try requireCurrentConnection()
                    let preparedPlayer = try await makePlayer(for: playableAsset)
                    try requireCurrentConnection()
                    player = preparedPlayer
                } catch { if !Task.isCancelled { problemText = error.localizedDescription } }
            }
            .onDisappear {
                isPlaybackVisible = false
                previewTask?.cancel()
                previewTask = nil
                player?.pause()
                player = nil
                if let localURL { try? FileManager.default.removeItem(at: localURL) }
                localURL = nil
            }
        }
    }
    private func makePlayer(for playableAsset: AVURLAsset) async throws -> AVPlayer {
        if let passageStartMS, let passageEndMS {
            let startOffsetMS = media.playbackOffsetMS(for: passageStartMS)
            let endOffsetMS = media.playbackOffsetMS(for: passageEndMS)
            guard passageStartMS >= media.timelineStartMS, endOffsetMS > startOffsetMS else {
                throw TeaNodeError.unreadableAnswer
            }
            // A trimmed in-memory composition bounds the standard player's entire seekable range.
            let composition = AVMutableComposition()
            let timeRange = CMTimeRange(
                start: CMTime(value: startOffsetMS, timescale: 1000),
                duration: CMTime(value: endOffsetMS - startOffsetMS, timescale: 1000))
            let tracks = try await playableAsset.load(.tracks)
            for originalTrack in tracks {
                guard originalTrack.mediaType == .audio || originalTrack.mediaType == .video,
                    let passageTrack = composition.addMutableTrack(
                        withMediaType: originalTrack.mediaType, preferredTrackID: kCMPersistentTrackID_Invalid)
                else { continue }
                try passageTrack.insertTimeRange(timeRange, of: originalTrack, at: .zero)
                passageTrack.preferredTransform = try await originalTrack.load(.preferredTransform)
            }
            guard !composition.tracks.isEmpty else { throw TeaNodeError.unreadableAnswer }
            return AVPlayer(playerItem: AVPlayerItem(asset: composition))
        } else if passageStartMS != nil || passageEndMS != nil {
            throw TeaNodeError.unreadableAnswer
        } else {
            return AVPlayer(playerItem: AVPlayerItem(asset: playableAsset))
        }
    }

    private func useAudioPreview() async {
        defer { isPreparingPreview = false }
        guard player != nil, !isCompatibleAudioPreview else { return }
        var previewURL: URL?
        do {
            try requireCurrentConnection()
            let playback = try await connection.prepareRecordingPlayback(for: media)
            try requireCurrentConnection()
            let request = try await connection.recordingCompatiblePlaybackRequest(playback, for: media)
            previewURL = try await downloadVerifiedMedia(
                request, expectedSizeBytes: playback.playbackSizeBytes, expectedSha256: playback.playbackSha256, fileExtension: "wav")
            guard let previewURL else { throw TeaNodeError.unreadableAnswer }
            let asset = AVURLAsset(url: previewURL)
            guard try await asset.load(.isPlayable) else { throw TeaNodeError.unreadableAnswer }
            let preparedPlayer = try await makePlayer(for: asset)
            try requireCurrentConnection()
            player?.pause()
            if let localURL { try? FileManager.default.removeItem(at: localURL) }
            localURL = previewURL
            player = preparedPlayer
            isCompatibleAudioPreview = true
            problemText = nil
        } catch {
            if let previewURL { try? FileManager.default.removeItem(at: previewURL) }
            if !Task.isCancelled { problemText = error.localizedDescription }
        }
    }

    private func requireCurrentConnection() throws {
        try Task.checkCancellation()
        guard isPlaybackVisible, AppModel.shared.connection === connection, let recordingUserId,
            RecordingLibraryProgress.isCurrentOwner(
                accountKey: connection.accountKey, recordingUserId: recordingUserId,
                currentAccountKey: AppModel.shared.connection?.accountKey,
                currentRecordingUserId: AppModel.shared.account?.cachedRecordingUserId),
            (try? KeychainAccountStore().load(accountKey: connection.accountKey)?.cachedRecordingUserId) == recordingUserId
        else { throw TeaNodeError.signedOut }
    }

    private func downloadVerifiedMedia(
        _ request: URLRequest, expectedSizeBytes: Int64, expectedSha256: String, fileExtension: String
    ) async throws -> URL {
        try requireCurrentConnection()
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        let (temporary, response) = try await session.download(for: request)
        defer { try? FileManager.default.removeItem(at: temporary) }
        let verifiedDigest = try await Task.detached { try MeetingRecorder.digest(temporary) }.value
        try requireCurrentConnection()
        guard (response as? HTTPURLResponse)?.statusCode == 200,
            Int64(try temporary.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0) == expectedSizeBytes,
            verifiedDigest == expectedSha256
        else { throw TeaNodeError.unreadableAnswer }
        let destination = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathExtension(fileExtension)
        try FileManager.default.moveItem(at: temporary, to: destination)
        return destination
    }

}

private struct RecordingLocalPlaybackScreen: View {
    let recording: LocalRecording
    let accountKey: String
    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var problemText: String?
    @State private var selectedMediaId: UUID?

    var body: some View {
        NavigationStack {
            List {
                if let player { VideoPlayer(player: player).frame(minHeight: 220) }
                Section("Local original media") {
                    ForEach(recording.localMedia.filter { !$0.isDurablyUploaded && $0.originalSha256 != nil }) { media in
                        Button {
                            Task { await play(media) }
                        } label: {
                            Label(
                                Duration.milliseconds(media.timelineStartMS).formatted(.time(pattern: .hourMinuteSecond)),
                                systemImage: selectedMediaId == media.id ? "speaker.wave.2" : "play")
                        }
                    }
                }
                if let problemText { Text(problemText).foregroundStyle(.secondary) }
            }
            .navigationTitle("Local original")
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Close") { dismiss() } } }
            .task {
                if let media = recording.localMedia.first(where: { !$0.isDurablyUploaded && $0.originalSha256 != nil }) { await play(media) }
            }
            .onDisappear {
                player?.pause()
                player = nil
            }
        }
    }

    private func play(_ media: LocalRecordingMedia) async {
        do {
            guard let retained = try KeychainAccountStore().load(accountKey: accountKey),
                recording.belongsTo(accountKey: accountKey, recordingUserId: retained.cachedRecordingUserId)
            else { throw TeaNodeError.signedOut }
            let journal = try MeetingRecorder.makeJournal()
            let source = try await journal.originalURL(recordingId: recording.id, mediaId: media.id, accountKey: accountKey)
            guard FileManager.default.fileExists(atPath: source.path) else { throw TeaNodeError.unreadableAnswer }
            player?.pause()
            player = AVPlayer(url: source)
            selectedMediaId = media.id
            player?.play()
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }
}
