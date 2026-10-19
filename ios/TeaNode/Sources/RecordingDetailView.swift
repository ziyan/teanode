import SwiftUI
import TeaNodeKit

/// RecordingDetailModel reads one recording and carries what the person asks of it: a new name, another transcript, a retry,
/// a delete.
@MainActor
@Observable
final class RecordingDetailModel {
    let connection: Connection
    let initialRecording: Recording
    var recordingView: RecordingView?
    var learning: RecordingLearningView?
    var limits: RecordingLimits?
    var speakers: RecordingSpeakersView?
    var problemText: String?
    var doneText: String?
    var isSubmitting = false
    /// What the rename form edits.
    var renameTitle = ""
    var renameExpectedSourceGeneration: Int64 = 0
    var hasRecordedAt = false
    var recordedAt = Date()

    init(connection: Connection, recording: Recording) {
        self.connection = connection
        self.initialRecording = recording
    }

    var recording: Recording { recordingView?.recording ?? initialRecording }
    var transcriptRuns: [RecordingTranscriptRun] { recordingView?.transcriptRuns ?? [] }
    var latestTranscriptRun: RecordingTranscriptRun? { transcriptRuns.max { $0.versionNumber < $1.versionNumber } }
    var completeMediaFiles: [RecordingMedia] { (recordingView?.mediaFiles ?? []).filter { $0.recordingUploadState == "complete" } }
    var isTranscriptionAvailable: Bool { limits?.isTranscriptionAvailable == true }

    /// What an original is called: a recording made in pieces has one original per piece, all with the same file name, so each
    /// is told by where it sits on the recording.
    func mediaTitle(_ media: RecordingMedia) -> String {
        guard (recordingView?.mediaFiles.count ?? 0) > 1 else { return media.originalFilename }
        let start = Duration.milliseconds(media.timelineStartMS).formatted(.time(pattern: .hourMinuteSecond))
        let end = Duration.milliseconds(media.timelineStartMS + media.mediaDurationMS).formatted(.time(pattern: .hourMinuteSecond))
        return String(localized: "\(start) to \(end)")
    }

    var serverProgress: RecordingServerProgress? { recordingView.map(RecordingServerProgress.init(view:)) }
    var status: RecordingRowStatus {
        let status = RecordingLibrary.status(recording: recording, localRecording: nil, progress: serverProgress)
        // With transcription unavailable on this server, an uploaded recording with no transcript is not waiting for one.
        if status == .transcribing, serverProgress?.transcriptProcessingStates.isEmpty == true, limits?.isTranscriptionAvailable == false {
            return .ready
        }
        return status
    }

    /// Whether the server is still to write the title and summary of a transcript that has just finished.
    var isAwaitingName: Bool {
        guard status == .ready, recording.recordingTitleSource != "user" else { return false }
        let updatedAt = ServerTime.date(recording.updatedAt) ?? .distantPast
        return recording.recordingTitle.isEmpty && RecordingLibrary.firstLine(of: recording.recordingSummary).isEmpty
            && Date().timeIntervalSince(updatedAt) < 600
    }

    var isStillChanging: Bool { status == .transcribing || isAwaitingName || learning?.isCurationPending == true }

    func load() async {
        do {
            recordingView = try await connection.recording(initialRecording.id)
            limits = try await connection.recordingLimits()
            learning = try await connection.recordingLearning(recordingId: initialRecording.id)
            speakers = try? await connection.recordingSpeakers(recordingId: initialRecording.id)
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }

    func rename(title: String?, recordedAt: String?, expectedSourceGeneration: Int64) async -> Bool {
        do {
            _ = try await connection.updateRecordingMetadata(
                recordingId: initialRecording.id, expectedSourceGeneration: expectedSourceGeneration, recordingTitle: title,
                recordedAt: recordedAt)
            doneText = String(localized: "Saved.")
            await load()
            return true
        } catch {
            let failure = error.localizedDescription
            await load()
            problemText = failure
            return false
        }
    }

    /// Asks for another transcription pass of every original that has arrived. A request identity kept across launches makes
    /// a repeated tap after a lost answer the same paid request.
    func transcribeAgain() async {
        guard !isSubmitting, isTranscriptionAvailable else { return }
        isSubmitting = true
        defer { isSubmitting = false }
        do {
            for media in completeMediaFiles {
                let hasPriorRun = transcriptRuns.contains { $0.recordingMediaId == media.id }
                let requestId = RecordingRequestIdentity.acquire(accountKey: connection.accountKey, mediaId: media.id)
                _ = try await connection.requestRecordingTranscription(
                    recordingMediaId: media.id, clientRunId: hasPriorRun ? requestId : "initial", isReprocessingRequested: hasPriorRun)
                RecordingRequestIdentity.finish(accountKey: connection.accountKey, mediaId: media.id, requestId: requestId)
            }
            doneText = String(localized: "Transcribing.")
            await load()
        } catch { problemText = error.localizedDescription }
    }

    func retry(_ run: RecordingTranscriptRun) async {
        do {
            _ = try await connection.retryRecordingTranscription(recordingTranscriptRunId: run.id)
            await load()
        } catch { problemText = error.localizedDescription }
    }

    func cancel(_ run: RecordingTranscriptRun) async {
        do {
            _ = try await connection.cancelRecordingTranscription(recordingTranscriptRunId: run.id)
            await load()
        } catch { problemText = error.localizedDescription }
    }

    func resumeLearning() async {
        guard let learning, !isSubmitting else { return }
        isSubmitting = true
        defer { isSubmitting = false }
        do {
            self.learning = try await connection.resumeRecordingLearning(
                recordingId: initialRecording.id, expectedSourceGeneration: learning.sourceGeneration)
            await load()
        } catch { problemText = error.localizedDescription }
    }

    func delete() async -> Bool {
        do {
            try await connection.deleteRecording(initialRecording.id)
            return true
        } catch {
            problemText = error.localizedDescription
            return false
        }
    }
}

/// RecordingDetailScreen is one recording: what it is called, when it was made, a summary, a player, and what was said.
struct RecordingDetailScreen: View {
    let connection: Connection
    let recording: Recording
    var shouldFocusLearning = false
    /// Opens with only the passages whose speaker nobody has named.
    var startsUnnamedOnly = false
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var app
    @State private var model: RecordingDetailModel
    @State private var conversation: RecordingConversationModel
    @State private var player: RecordingPlayer
    @State private var isShowingDetails = false
    @State private var isConfirmingTranscription = false
    @State private var isDeleting = false
    @State private var isRenaming = false
    @State private var sharedMedia: RecordingMedia?
    @State private var isTitleScrolledAway = false

    init(connection: Connection, recording: Recording, shouldFocusLearning: Bool = false, startsUnnamedOnly: Bool = false) {
        self.connection = connection
        self.recording = recording
        self.shouldFocusLearning = shouldFocusLearning
        self.startsUnnamedOnly = startsUnnamedOnly
        _model = State(initialValue: RecordingDetailModel(connection: connection, recording: recording))
        _conversation = State(initialValue: RecordingConversationModel(connection: connection, recording: recording))
        _player = State(initialValue: RecordingPlayer(connection: connection, durationMS: recording.timelineDurationMS))
    }

    private var displayedRecordingTitle: String {
        model.recording.recordingTitle.isEmpty ? String(localized: "Untitled recording") : model.recording.recordingTitle
    }

    private var metaLine: String {
        let recorded = ServerTime.date(model.recording.recordedAt) ?? ServerTime.date(model.recording.createdAt)
        var parts: [String] = []
        if let recorded { parts.append(recorded.formatted(date: .abbreviated, time: .shortened)) }
        parts.append(RecordingDuration.text(model.recording.timelineDurationMS))
        if conversation.speakerCount > 0 { parts.append(String(localized: "\(conversation.speakerCount) speakers")) }
        return parts.joined(separator: " · ")
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 16) {
                Text(displayedRecordingTitle).font(.title.bold()).fixedSize(horizontal: false, vertical: true)
                Text(metaLine).font(.subheadline).foregroundStyle(.secondary)
                let summary = model.recording.recordingSummary ?? ""
                if !summary.isEmpty { Text(summary).font(.body) }
                statusRow
                RecordingMiniPlayerView(
                    canPlay: player.canPlay, isPlaying: player.isPlaying, isPreparing: player.isPreparing, timelineMS: player.timelineMS,
                    durationMS: player.durationMS, onToggle: { Task { await player.toggle() } },
                    onSeek: { milliseconds in Task { await player.seek(toTimelineMS: milliseconds) } }
                )
                RecordingConversationSection(model: conversation, player: player)
            }
            .padding(.horizontal)
        }
        // Over the foot of the screen, with the content pushed up by its height, so it never covers the last bubble.
        .safeAreaInset(edge: .bottom) {
            if player.hasPlaybackSession {
                RecordingFloatingPlayerView(
                    isPlaying: player.isPlaying, isPreparing: player.isPreparing, timelineMS: player.timelineMS,
                    onToggle: { Task { await player.toggle() } }, onStop: { player.stop() })
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
        }
        .animation(.default, value: player.hasPlaybackSession)
        .onScrollGeometryChange(for: Bool.self) { geometry in geometry.contentOffset.y > 40 } action: { _, isScrolled in
            isTitleScrolledAway = isScrolled
        }
        // The full title is the first thing in the content; the bar shows it only once that has scrolled away.
        .navigationTitle(isTitleScrolledAway ? displayedRecordingTitle : "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { ToolbarItem(placement: .primaryAction) { actionsMenu } }
        .toast(
            done: Binding(get: { model.doneText ?? conversation.doneText }, set: { _ in model.doneText = nil; conversation.doneText = nil }),
            problem: Binding(get: { model.problemText ?? conversation.problemText }, set: { _ in model.problemText = nil; conversation.problemText = nil })
        )
        .task {
            await load()
            if startsUnnamedOnly { await conversation.showUnnamedOnly(true) }
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(5))
                guard !Task.isCancelled else { return }
                if !isRenaming && !isConfirmingTranscription && !model.isSubmitting && model.isStillChanging {
                    let wasTranscribing = model.status == .transcribing
                    await model.load()
                    if wasTranscribing && model.status != .transcribing { await conversation.loadPage(isAdditional: false) }
                }
            }
        }
        .task(id: model.learning) {
            guard shouldFocusLearning, model.learning != nil else { return }
            isShowingDetails = true
        }
        .refreshable { await load() }
        .onChange(of: player.problemText) { _, text in
            guard let text else { return }
            conversation.problemText = text
            player.problemText = nil
        }
        .onDisappear { player.stop() }
        .recordingPassageSheets(conversation)
        .sheet(isPresented: $isShowingDetails) {
            RecordingDetailsSheet(connection: connection, model: model, focusesLearning: shouldFocusLearning)
        }
        .sheet(isPresented: $isRenaming) { RecordingRenameSheet(model: model) }
        .sheet(item: $sharedMedia) { media in RecordingPlaybackScreen(connection: connection, media: media) }
    }

    /// Where the transcript is, until it is done: the person should not wonder why the conversation is empty.
    @ViewBuilder
    private var statusRow: some View {
        switch model.status {
        case .transcribing, .uploading:
            HStack(spacing: 8) {
                ProgressView()
                RecordingStatusLabel(status: model.status)
            }
        case .failed:
            VStack(alignment: .leading, spacing: 8) {
                RecordingStatusLabel(status: .failed)
                if let run = model.latestTranscriptRun, run.recordingProcessingState == "uncertain" {
                    // The answer to a request already sent never arrived, so
                    // whether it was charged is unknown: resuming it could pay
                    // twice. Transcribing again is a new request, said so.
                    Text("The answer from the transcription service was lost, so this recording was not transcribed. Transcribing it again is a new paid request.")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Button {
                        Task { await model.transcribeAgain() }
                    } label: {
                        Label("Transcribe again", systemImage: "arrow.clockwise")
                    }
                    .buttonStyle(.bordered)
                    .disabled(!model.isTranscriptionAvailable || model.isSubmitting)
                } else if let run = model.latestTranscriptRun {
                    Button {
                        Task { await model.retry(run) }
                    } label: {
                        Label("Retry transcription", systemImage: "arrow.clockwise")
                    }
                    .buttonStyle(.bordered)
                    .disabled(!model.isTranscriptionAvailable)
                }
            }
        default:
            EmptyView()
        }
    }

    private var actionsMenu: some View {
        Menu {
            Button {
                openRename()
            } label: {
                Label("Rename", systemImage: "pencil")
            }
            if model.completeMediaFiles.count == 1, let media = model.completeMediaFiles.first {
                Button {
                    sharedMedia = media
                } label: {
                    Label("Share original", systemImage: "square.and.arrow.up")
                }
            } else if model.completeMediaFiles.count > 1 {
                Menu {
                    ForEach(model.completeMediaFiles) { media in
                        Button(model.mediaTitle(media)) { sharedMedia = media }
                    }
                } label: {
                    Label("Share original", systemImage: "square.and.arrow.up")
                }
            }
            Button {
                isConfirmingTranscription = true
            } label: {
                Label(
                    model.transcriptRuns.isEmpty ? String(localized: "Transcribe") : String(localized: "Transcribe again"),
                    systemImage: "arrow.clockwise")
            }
            .disabled(model.completeMediaFiles.isEmpty || !model.isTranscriptionAvailable || model.isSubmitting)
            Button("Ask \(app.agentTitle)", systemImage: "bubble.left.and.text.bubble.right") {
                app.askingAbout = AgentReference(recordingId: model.recording.id, recordingTitle: displayedRecordingTitle)
            }
            Button {
                isShowingDetails = true
            } label: {
                Label("Details", systemImage: "info.circle")
            }
            Divider()
            Button(role: .destructive) {
                isDeleting = true
            } label: {
                Label("Delete recording", systemImage: "trash")
            }
            .tint(.red)
        } label: {
            Label("Actions", systemImage: "ellipsis.circle")
        }
        .confirmationDialog("Start another transcription pass?", isPresented: $isConfirmingTranscription, titleVisibility: .visible) {
            Button("Transcribe or reprocess") { Task { await transcribeAgain() } }
        } message: {
            Text(
                "A new pass can be billed again by the provider. Confirmed labels stay separate from generated versions. Rename a speaker without reprocessing to avoid another pass."
            )
        }
        .confirmationDialog("Delete recording and its originals?", isPresented: $isDeleting, titleVisibility: .visible) {
            Button("Delete recording", role: .destructive) {
                Task { if await model.delete() { dismiss() } }
            }
        } message: {
            Text("Derived audio, transcripts and reference samples will be removed. Learned material will be reconciled.")
        }
    }

    private func openRename() {
        model.renameTitle = model.recording.recordingTitle
        model.renameExpectedSourceGeneration = model.recording.sourceGeneration
        model.hasRecordedAt = model.recording.recordedAt != nil
        model.recordedAt = ServerTime.date(model.recording.recordedAt) ?? Date()
        isRenaming = true
    }

    private func transcribeAgain() async {
        await model.transcribeAgain()
        await conversation.loadPage(isAdditional: false)
    }

    private func load() async {
        await model.load()
        player.update(mediaFiles: model.recordingView?.mediaFiles ?? [], durationMS: model.recording.timelineDurationMS)
        await conversation.loadPage(isAdditional: false)
    }
}

/// RecordingRenameSheet changes the title and the date. Emptying the title hands the naming back to the server.
struct RecordingRenameSheet: View {
    @Bindable var model: RecordingDetailModel
    @Environment(\.dismiss) private var dismiss
    @State private var isSaving = false

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Title", text: $model.renameTitle)
                } footer: {
                    Text("Leave the title empty and TeaNode names the recording again from its transcript.")
                }
                Section {
                    Toggle("Meeting date is known", isOn: $model.hasRecordedAt)
                    if model.hasRecordedAt { DatePicker("Recorded at", selection: $model.recordedAt) }
                }
            }
            .toast(done: .constant(nil), problem: $model.problemText)
            .navigationTitle("Recording details")
            .navigationBarTitleDisplayMode(.inline)
            .disabled(isSaving)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task {
                            guard !isSaving else { return }
                            isSaving = true
                            defer { isSaving = false }
                            let trimmedTitle = model.renameTitle.trimmingCharacters(in: .whitespacesAndNewlines)
                            // Sending the title as it already is would turn a generated name into one the person wrote.
                            let isTitleChanged = trimmedTitle != model.recording.recordingTitle
                            let isSaved = await model.rename(
                                title: isTitleChanged ? trimmedTitle : nil,
                                recordedAt: model.hasRecordedAt ? model.recordedAt.formatted(.iso8601) : nil,
                                expectedSourceGeneration: model.renameExpectedSourceGeneration)
                            if isSaved { dismiss() }
                        }
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }
}

/// RecordingDetailsSheet holds what is not needed to read a recording: how its transcript was made, who spoke, what the agent
/// learned from it, the gaps in the capture, and the originals.
struct RecordingDetailsSheet: View {
    let connection: Connection
    @Bindable var model: RecordingDetailModel
    var focusesLearning = false
    @Environment(\.dismiss) private var dismiss
    @State private var selectedMedia: RecordingMedia?
    @State private var editingLearningSource: KnowledgeSource?

    var body: some View {
        NavigationStack {
            ScrollViewReader { scrollProxy in
                List {
                    if let run = model.latestTranscriptRun {
                        Section {
                            LabeledContent("Processing", value: RecordingWords.processing(run.recordingProcessingState))
                            LabeledContent("Provider model", value: run.providerModelName)
                            if !run.processingErrorMessage.isEmpty { Text(run.processingErrorMessage.asReasonSentence).foregroundStyle(.secondary) }
                            if ["queued", "preparing", "transcribing", "failed", "canceled"].contains(run.recordingProcessingState) {
                                Button {
                                    Task { await model.retry(run) }
                                } label: {
                                    Label("Retry transcription", systemImage: "arrow.clockwise")
                                        .actionColor()
                                }
                                .disabled(!model.isTranscriptionAvailable)
                                .fullWidthRowSeparator()
                            }
                            if run.isRunning {
                                Button("Cancel transcription", role: .destructive) { Task { await model.cancel(run) } }
                            }
                            NavigationLink {
                                RecordingTranscriptScreen(connection: connection, recording: model.recording, transcriptRun: run)
                            } label: {
                                Label("Transcription details", systemImage: "doc.text.magnifyingglass")
                                    .foregroundStyle(.primary)
                            }
                        } header: {
                            Text("Transcription")
                        } footer: {
                            if !model.isTranscriptionAvailable {
                                Text(
                                    model.limits?.transcriptionUnavailableMessage?.isEmpty == false
                                        ? (model.limits?.transcriptionUnavailableMessage ?? "").asReasonSentence
                                        : String(localized: "Transcription availability has not been verified. Refresh or ask the operator.")
                                )
                            }
                        }
                    }
                    Section {
                        if let speakers = model.speakers {
                            NavigationLink {
                                RecordingSpeakersScreen(connection: connection, recording: model.recording, initial: speakers)
                            } label: {
                                Label("Speakers and voice references", systemImage: "person.2.wave.2")
                                    .foregroundStyle(.primary)
                            }
                        }
                        NavigationLink {
                            RecordingTextHistoryScreen(connection: connection, recordingId: model.recording.id)
                        } label: {
                            Label("Transcript edit history", systemImage: "clock.arrow.circlepath")
                                .foregroundStyle(.primary)
                        }
                    }
                    if !model.recording.captureGaps.isEmpty {
                        Section("Capture gaps") {
                            ForEach(model.recording.captureGaps) { gap in
                                LabeledContent(gap.recordingGapReason == "paused" ? String(localized: "Paused") : String(localized: "Interrupted")) {
                                    Text(Duration.milliseconds(gap.gapStartMS).formatted(.time(pattern: .hourMinuteSecond))).monospacedDigit()
                                }
                            }
                        }
                    }
                    if let learning = model.learning { learningSection(learning) }
                    Section {
                        ForEach(model.recordingView?.mediaFiles ?? []) { media in
                            Button {
                                selectedMedia = media
                            } label: {
                                VStack(alignment: .leading, spacing: 4) {
                                    Label(model.mediaTitle(media), systemImage: media.originalContentType.hasPrefix("video/") ? "video" : "waveform")
                                        .actionColor()
                                    Text(
                                        media.recordingUploadState == "complete"
                                            ? String(localized: "Ready to play") : String(localized: "Upload pending")
                                    ).font(.caption).foregroundStyle(.secondary)
                                }
                            }.disabled(media.recordingUploadState != "complete")
                        }
                    } header: {
                        Text("Original media")
                    }
                }
                .task(id: model.learning) {
                    guard focusesLearning, model.learning != nil else { return }
                    try? await Task.sleep(for: .milliseconds(200))
                    scrollProxy.scrollTo("recording-learning", anchor: .top)
                }
            }
            .toast(done: $model.doneText, problem: $model.problemText)
            .navigationTitle("Details")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } } }
            .sheet(item: $selectedMedia) { media in RecordingPlaybackScreen(connection: connection, media: media) }
            .sheet(item: $editingLearningSource) { source in
                KnowledgeSourceEditor(connection: connection, source: source, sourceTypes: [], mailboxes: []) { _ in
                    Task { await model.load() }
                }
            }
        }
    }

    @ViewBuilder
    private func learningSection(_ learning: RecordingLearningView) -> some View {
        Section {
            if learning.hasStoppedReading == true {
                Text("Stopped reading transcripts").foregroundStyle(.secondary)
                Button("Resume reading", systemImage: "play") { Task { await model.resumeLearning() } }.actionColor().disabled(model.isSubmitting)
                    .fullWidthRowSeparator()
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
                if !source.lastError.isEmpty { Text(source.lastError.asReasonSentence).foregroundStyle(.secondary) }
                NavigationLink {
                    KnowledgeSourcesView(connection: connection, highlightedSourceId: source.id)
                } label: {
                    Label("Knowledge source", systemImage: "brain")
                        .foregroundStyle(.primary)
                }
                .fullWidthRowSeparator()
                Button("Edit this source", systemImage: "pencil") { editingLearningSource = source }
                    .actionColor()
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
}
