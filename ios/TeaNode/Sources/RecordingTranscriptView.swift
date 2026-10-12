import SwiftUI
import TeaNodeKit

struct RecordingTranscriptScreen: View {
    let connection: Connection
    let recording: Recording
    let transcriptRun: RecordingTranscriptRun
    @State private var transcriptView: RecordingTranscriptView?
    @State private var segments: [RecordingTranscriptSegment] = []
    @State private var speakers: RecordingSpeakersView?
    @State private var problemText: String?
    @State private var selectedSegment: RecordingTranscriptSegment?
    @State private var confirmedName = ""
    @State private var isWholeGroup = false
    @State private var isNaming = false
    @State private var isSaving = false
    @State private var correctionRequestId = UUID().uuidString
    @State private var playbackMedia: RecordingMedia?
    @State private var passageStartMS: Int64 = 0

    var body: some View {
        List {
            Section {
                LabeledContent(
                    "Processing",
                    value: RecordingWords.processing(transcriptView?.transcriptRun.recordingProcessingState ?? transcriptRun.recordingProcessingState)
                )
                LabeledContent("Provider model", value: transcriptRun.providerModelName)
                if let processingError = transcriptView?.transcriptRun.processingErrorMessage, !processingError.isEmpty {
                    Text(processingError).foregroundStyle(.secondary)
                }
            }
            if let transcriptView {
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
                ForEach(segments) { segment in
                    VStack(alignment: .leading, spacing: 8) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(
                                speakers?.confirmedName(for: segment, mediaId: transcriptRun.recordingMediaId)
                                    ?? String(localized: "Unconfirmed speaker")
                            ).font(.headline)
                            Spacer()
                            Text(Duration.milliseconds(segment.segmentStartMS).formatted(.time(pattern: .hourMinuteSecond))).font(.caption)
                                .foregroundStyle(.secondary).monospacedDigit()
                        }
                        Text(segment.transcriptText).textSelection(.enabled)
                        Button {
                            Task {
                                do {
                                    passageStartMS = segment.segmentStartMS
                                    playbackMedia = try await connection.recording(recording.id).mediaFiles.first {
                                        $0.id == transcriptRun.recordingMediaId
                                    }
                                } catch { problemText = error.localizedDescription }
                            }
                        } label: {
                            Label("Play passage", systemImage: "play")
                        }
                        Button {
                            selectedSegment = segment
                            confirmedName = speakers?.confirmedName(for: segment, mediaId: transcriptRun.recordingMediaId) ?? ""
                            isWholeGroup = false
                            correctionRequestId = UUID().uuidString
                            isNaming = true
                        } label: {
                            Label("Name speaker", systemImage: "person.crop.circle.badge.checkmark")
                        }
                    }.padding(.vertical, 4)
                }
                if transcriptView?.hasMoreSegments == true {
                    Button {
                        Task { await loadPage(isAdditional: true) }
                    } label: {
                        Label("Load more passages", systemImage: "chevron.down")
                    }
                }
            } header: {
                Text("Transcript")
            } footer: {
                Text("Speaker groups are local to each provider request. Only your confirmed names connect people across chunks and versions.")
            }
            if let speakers {
                NavigationLink {
                    RecordingSpeakersScreen(connection: connection, recording: recording, initial: speakers)
                } label: {
                    Label("Speakers and voice references", systemImage: "person.2.wave.2")
                }
            }
            if let problemText { Text(problemText).foregroundStyle(.secondary) }
        }
        .navigationTitle("Transcript")
        .task { await loadPage(isAdditional: false) }
        .refreshable { await loadPage(isAdditional: false) }
        .sheet(item: $playbackMedia) { media in RecordingPlaybackScreen(connection: connection, media: media, passageStartMS: passageStartMS) }
        .sheet(isPresented: $isNaming) {
            NavigationStack {
                Form {
                    Section {
                        TextField("Confirmed name", text: $confirmedName)
                        Toggle("Apply to this speaker group", isOn: $isWholeGroup)
                    } footer: {
                        Text(
                            "Off changes this passage only. Naming is free metadata. Suitable voice samples are selected automatically, and learned material is reconciled."
                        )
                    }
                    if let problemText { Text(problemText).foregroundStyle(.secondary) }
                }
                .navigationTitle("Name speaker")
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { isNaming = false }.disabled(isSaving) }
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Save") { Task { await saveSpeaker() } }.disabled(
                            isSaving || confirmedName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    }
                }
            }
        }
    }

    private func loadPage(isAdditional: Bool) async {
        do {
            let page = try await connection.recordingTranscript(
                recordingTranscriptRunId: transcriptRun.id, segmentOffset: isAdditional ? segments.count : 0)
            transcriptView = page
            if isAdditional { segments.append(contentsOf: page.segments) } else { segments = page.segments }
            speakers = try await connection.recordingSpeakers(recordingId: recording.id)
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }

    private func saveSpeaker() async {
        guard !isSaving, let selectedSegment, let speakers else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            self.speakers = try await connection.correctRecordingSpeaker(
                recordingId: recording.id, expectedSourceGeneration: speakers.sourceGeneration,
                clientCorrectionId: correctionRequestId, confirmedName: confirmedName.trimmingCharacters(in: .whitespacesAndNewlines),
                segment: selectedSegment, isWholeGroup: isWholeGroup)
            isNaming = false
            problemText = nil
        } catch {
            problemText = error.localizedDescription
            self.speakers = try? await connection.recordingSpeakers(recordingId: recording.id)
        }
    }
}

struct RecordingSpeakersScreen: View {
    let connection: Connection
    let recording: Recording
    @State private var speakers: RecordingSpeakersView
    @State private var problemText: String?
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
                ForEach(speakers.speakerIdentities) { identity in
                    Button {
                        renamedIdentity = identity
                        confirmedName = identity.confirmedName
                        isRenaming = true
                    } label: {
                        Label(identity.confirmedName, systemImage: "person.crop.circle")
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
            if let problemText { Text(problemText).foregroundStyle(.secondary) }
        }
        .navigationTitle("Speakers and voice references")
        .refreshable {
            do { speakers = try await connection.recordingSpeakers(recordingId: recording.id) } catch { problemText = error.localizedDescription }
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
