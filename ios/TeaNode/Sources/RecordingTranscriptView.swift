import SwiftUI
import TeaNodeKit

/// RecordingTranscriptScreen is the transcript of one recording as a conversation: the latest completed one, or, for a
/// cited passage, the version the citation names.
struct RecordingTranscriptScreen: View {
    let connection: Connection
    let recording: Recording
    let transcriptRun: RecordingTranscriptRun?
    let citation: RecordingLink?
    @State private var model: RecordingConversationModel
    @State private var player: RecordingPlayer

    init(connection: Connection, recording: Recording, transcriptRun: RecordingTranscriptRun? = nil, citation: RecordingLink? = nil) {
        self.connection = connection
        self.recording = recording
        self.transcriptRun = transcriptRun
        self.citation = citation
        _model = State(
            initialValue: RecordingConversationModel(connection: connection, recording: recording, transcriptRun: transcriptRun, citation: citation))
        _player = State(initialValue: RecordingPlayer(connection: connection, durationMS: recording.timelineDurationMS))
    }

    var body: some View {
        List {
            if let citation, let startMS = citation.passageStartMS, let endMS = citation.passageEndMS {
                Section("Cited passage") {
                    if let transcriptRun { Text("Version \(transcriptRun.versionNumber)") }
                    LabeledContent("Passage start") { Text(Duration.milliseconds(startMS).formatted(.time(pattern: .hourMinuteSecond))) }
                    LabeledContent("Passage end") { Text(Duration.milliseconds(endMS).formatted(.time(pattern: .hourMinuteSecond))) }
                    if let transcriptRun {
                        NavigationLink {
                            RecordingTranscriptScreen(connection: connection, recording: recording, transcriptRun: transcriptRun)
                        } label: {
                            Label("Open transcript version", systemImage: "text.bubble")
                                .foregroundStyle(.primary)
                        }
                    }
                }
            }
            if let transcriptRun {
                Section {
                    LabeledContent(
                        "Processing",
                        value: RecordingWords.processing(
                            model.transcriptView?.transcriptRun.recordingProcessingState ?? transcriptRun.recordingProcessingState)
                    )
                    LabeledContent("Provider model", value: transcriptRun.providerModelName)
                    if let processingError = model.transcriptView?.transcriptRun.processingErrorMessage, !processingError.isEmpty {
                        Text(processingError.asReasonSentence).foregroundStyle(.secondary)
                    }
                }
            }
            if !model.invalidations.isEmpty {
                Section {
                    // A warning is orange with its symbol, not the green of the accent.
                    Label {
                        Text("Speaker reference invalidated")
                    } icon: {
                        Image(systemName: "exclamationmark.triangle").foregroundStyle(.orange)
                    }
                    .fullWidthRowSeparator()
                    Text(
                        "Generated speaker attribution from affected requests is uncertain. Your confirmed passage labels remain in place. Reprocessing is optional and can require another paid pass."
                    ).foregroundStyle(.secondary)
                    ForEach(model.invalidations) { invalidation in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(invalidation.invalidatedConfirmedName)
                            Text(invalidation.invalidationMessage.asReasonSentence).font(.callout).foregroundStyle(.secondary)
                            Text(
                                invalidation.isReconciled
                                    ? String(localized: "Learning reconciled") : String(localized: "Learning reconciliation pending")
                            ).font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
            if let transcriptView = model.transcriptView {
                Section {
                    ForEach(transcriptView.attempts) { attempt in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(attempt.providerModelName)
                            if let actualCost = attempt.actualCostAmount {
                                Text("Actual cost: \(actualCost.formatted()) \(attempt.currencyCode)")
                            } else if let estimatedCost = attempt.estimatedCostAmount {
                                Text("Estimated cost: \(estimatedCost.formatted()) \(attempt.currencyCode)")
                            } else {
                                Text("Cost unavailable")
                            }
                            if !attempt.hasProviderUsage { Text("Provider usage unavailable").font(.caption).foregroundStyle(.secondary) }
                        }
                    }
                } header: {
                    Text("Transcription usage")
                } footer: {
                    Text("Estimates use configured pricing. Actual cost is shown only when billing evidence exists.")
                }
            }
            Section {
                RecordingMiniPlayerView(
                    canPlay: player.canPlay, isPlaying: player.isPlaying, isPreparing: player.isPreparing, timelineMS: player.timelineMS,
                    durationMS: player.durationMS, onToggle: { Task { await player.toggle() } },
                    onSeek: { milliseconds in Task { await player.seek(toTimelineMS: milliseconds) } }
                )
                .listRowBackground(Color.clear)
                RecordingConversationSection(model: model, player: player)
                if let citation, !model.segments.isEmpty, !model.segments.contains(where: { citation.includes($0) }) {
                    Text("The cited passage is unavailable in this transcript version.").foregroundStyle(.secondary)
                }
            } header: {
                Text("Transcript")
            } footer: {
                if transcriptRun == nil {
                    Text(
                        "The full timeline uses the latest completed version of each original. Pending versions and incomplete originals remain available in the recording library."
                    )
                }
                Text("Speaker groups are local to each provider request. Only your confirmed names connect people across chunks and versions.")
            }
            NavigationLink {
                RecordingTextHistoryScreen(connection: connection, recordingId: recording.id)
            } label: {
                Label("Transcript edit history", systemImage: "clock.arrow.circlepath")
                    .foregroundStyle(.primary)
            }
            if let speakers = model.speakers {
                NavigationLink {
                    RecordingSpeakersScreen(connection: connection, recording: recording, initial: speakers)
                } label: {
                    Label("Speakers and voice references", systemImage: "person.2.wave.2")
                        .foregroundStyle(.primary)
                }
            }
        }
        .toast(done: $model.doneText, problem: $model.problemText)
        .navigationTitle("Transcript")
        .task {
            await model.loadPage(isAdditional: false)
            if let view = try? await connection.recording(recording.id) {
                player.update(mediaFiles: view.mediaFiles, durationMS: view.recording.timelineDurationMS)
            }
        }
        .refreshable { await model.loadPage(isAdditional: false) }
        .onChange(of: player.problemText) { _, text in
            guard let text else { return }
            model.problemText = text
            player.problemText = nil
        }
        .onDisappear { player.stop() }
        .recordingPassageSheets(model)
    }
}

struct RecordingSpeakersScreen: View {
    let connection: Connection
    let recording: Recording
    @State private var speakers: RecordingSpeakersView
    @State private var problemText: String?
    @State private var playbackMedia: RecordingMedia?
    @State private var referenceStartMS: Int64 = 0
    @State private var referenceEndMS: Int64?
    @State private var renamedIdentity: RecordingSpeakerIdentity?
    @State private var confirmedName = ""
    @State private var isRenaming = false
    @State private var isSaving = false

    init(connection: Connection, recording: Recording, initial: RecordingSpeakersView) {
        self.connection = connection
        self.recording = recording
        _speakers = State(initialValue: initial)
    }

    var body: some View {
        List {
            Section("Confirmed speakers") {
                ForEach(
                    speakers.speakerIdentities.filter { identity in speakers.speakerCorrections.contains { $0.speakerIdentityId == identity.id } }
                ) { identity in
                    Button {
                        renamedIdentity = identity
                        confirmedName = identity.confirmedName
                        isRenaming = true
                    } label: {
                        Label(identity.confirmedName, systemImage: "person.crop.circle")
                            .actionColor()
                    }
                }
            }
            Section("Confirmed passages") {
                ForEach(speakers.speakerCorrections.filter { !$0.isRemoved }) { correction in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(
                            speakers.speakerIdentities.first { $0.id == correction.speakerIdentityId }?.confirmedName
                                ?? String(localized: "Unconfirmed speaker"))
                        Text(Duration.milliseconds(correction.passageStartMS).formatted(.time(pattern: .hourMinuteSecond))).font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    .swipeActions { Button("Remove correction", role: .destructive) { Task { await removeCorrection(correction) } } }
                }
            }
            Section {
                ForEach(speakers.voiceReferences.filter { !$0.isRevoked }) { reference in
                    VStack(alignment: .leading, spacing: 6) {
                        Text(
                            speakers.speakerIdentities.first { $0.id == reference.speakerIdentityId }?.confirmedName
                                ?? String(localized: "Unconfirmed speaker"))
                        Text(Duration.milliseconds(reference.passageEndMS - reference.passageStartMS).formatted(.time(pattern: .hourMinuteSecond)))
                            .font(.caption).foregroundStyle(.secondary)
                        Button {
                            Task {
                                do {
                                    referenceStartMS = reference.passageStartMS
                                    referenceEndMS = reference.passageEndMS
                                    playbackMedia = try await connection.recording(recording.id).mediaFiles.first {
                                        $0.id == reference.recordingMediaId
                                    }
                                } catch { problemText = error.localizedDescription }
                            }
                        } label: {
                            Label("Play reference passage", systemImage: "play")
                        }
                    }.swipeActions { Button("Remove voice reference", role: .destructive) { Task { await removeReference(reference) } } }
                }
                if speakers.voiceReferences.allSatisfy(\.isRevoked) { Text("No clean reference samples available").foregroundStyle(.secondary) }
            } header: {
                Text("Voice references")
            } footer: {
                Text(
                    "References are curated only from names you confirm. Removing or correcting a label invalidates affected samples. Reprocessing is a separate paid pass."
                )
            }
        }
        .toast(done: .constant(nil), problem: $problemText)
        .navigationTitle("Speakers")
        .refreshable {
            do { speakers = try await connection.recordingSpeakers(recordingId: recording.id) } catch { problemText = error.localizedDescription }
        }
        .sheet(item: $playbackMedia) { media in
            RecordingPlaybackScreen(connection: connection, media: media, passageStartMS: referenceStartMS, passageEndMS: referenceEndMS)
        }
        .alert("Rename speaker", isPresented: $isRenaming) {
            TextField("Confirmed name", text: $confirmedName)
            Button("Save") { Task { await rename() } }
            Button("Cancel", role: .cancel) {}
        }
        .disabled(isSaving)
    }

    private func rename() async {
        guard let renamedIdentity, !isSaving else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            speakers = try await connection.renameRecordingSpeaker(
                recordingId: recording.id, expectedSourceGeneration: speakers.sourceGeneration, speakerIdentityId: renamedIdentity.id,
                confirmedName: confirmedName)
            problemText = nil
        } catch {
            problemText = error.localizedDescription
            speakers = (try? await connection.recordingSpeakers(recordingId: recording.id)) ?? speakers
        }
    }
    private func removeCorrection(_ correction: RecordingSpeakerCorrection) async {
        guard !isSaving else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            speakers = try await connection.removeRecordingSpeakerCorrection(
                recordingId: recording.id, expectedSourceGeneration: speakers.sourceGeneration, recordingSpeakerCorrectionId: correction.id)
            problemText = nil
        } catch {
            problemText = error.localizedDescription
            speakers = (try? await connection.recordingSpeakers(recordingId: recording.id)) ?? speakers
        }
    }
    private func removeReference(_ reference: RecordingVoiceReference) async {
        guard !isSaving else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            speakers = try await connection.deleteRecordingVoiceReference(
                recordingId: recording.id, expectedSourceGeneration: speakers.sourceGeneration, recordingVoiceReferenceId: reference.id)
            problemText = nil
        } catch {
            problemText = error.localizedDescription
            speakers = (try? await connection.recordingSpeakers(recordingId: recording.id)) ?? speakers
        }
    }
}

enum RecordingWords {
    static func sourceName(_ source: KnowledgeSource) -> String {
        source.kind == "recording" ? source.recordingTitle ?? String(localized: "Untitled recording") : source.name
    }

    static func processing(_ processingState: String) -> String {
        switch processingState {
        case "queued": String(localized: "Queued")
        case "preparing": String(localized: "Preparing audio")
        case "transcribing": String(localized: "Transcribing")
        case "complete": String(localized: "Complete")
        case "failed": String(localized: "Failed")
        case "uncertain": String(localized: "Provider outcome unknown")
        case "canceled": String(localized: "Canceled")
        default: String(localized: "Unknown")
        }
    }
}
