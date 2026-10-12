import SwiftUI
import TeaNodeKit

struct RecordingTranscriptScreen: View {
    let connection: Connection
    let recording: Recording
    let transcriptRun: RecordingTranscriptRun?
    let citation: RecordingLink?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var timelineView: RecordingTranscriptTimeline?
    @State private var transcriptView: RecordingTranscriptView?
    @State private var segments: [RecordingTranscriptSegment] = []
    @State private var nextSegmentOffset = 0
    @State private var speakers: RecordingSpeakersView?
    @State private var invalidations: [RecordingReferenceInvalidation] = []
    @State private var problemText: String?
    @State private var editingSegment: RecordingTranscriptSegment?
    @State private var selectedSegment: RecordingTranscriptSegment?
    @State private var confirmedName = ""
    @State private var correctionStartMS = ""
    @State private var correctionEndMS = ""
    @State private var isWholeGroup = false
    @State private var isNaming = false
    @State private var isSaving = false
    @State private var speakerDraft: RecordingSpeakerDraft?
    @State private var playbackMedia: RecordingMedia?
    @State private var passageStartMS: Int64 = 0

    init(connection: Connection, recording: Recording, transcriptRun: RecordingTranscriptRun? = nil, citation: RecordingLink? = nil) {
        self.connection = connection
        self.recording = recording
        self.transcriptRun = transcriptRun
        self.citation = citation
    }

    private func mediaId(for segment: RecordingTranscriptSegment) -> String? {
        transcriptRun?.recordingMediaId ?? timelineView?.transcriptRuns.first { $0.id == segment.recordingTranscriptRunId }?.recordingMediaId
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
                        }
                    }
                }
            }
            if let transcriptRun {
                Section {
                    LabeledContent(
                        "Processing",
                        value: RecordingWords.processing(
                            transcriptView?.transcriptRun.recordingProcessingState ?? transcriptRun.recordingProcessingState)
                    )
                    LabeledContent("Provider model", value: transcriptRun.providerModelName)
                    if let processingError = transcriptView?.transcriptRun.processingErrorMessage, !processingError.isEmpty {
                        Text(processingError).foregroundStyle(.secondary)
                    }
                }
            }
            if !invalidations.isEmpty {
                Section {
                    Label("Speaker reference invalidated", systemImage: "exclamationmark.triangle")
                    Text(
                        "Generated speaker attribution from affected requests is uncertain. Your confirmed passage labels remain in place. Reprocessing is optional and can require another paid pass."
                    ).foregroundStyle(.secondary)
                    ForEach(invalidations) { invalidation in
                        VStack(alignment: .leading, spacing: 4) {
                            Text(invalidation.invalidatedConfirmedName)
                            Text(invalidation.invalidationMessage).font(.callout).foregroundStyle(.secondary)
                            Text(
                                invalidation.isReconciled
                                    ? String(localized: "Learning reconciled") : String(localized: "Learning reconciliation pending")
                            ).font(.caption).foregroundStyle(.secondary)
                        }
                    }
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
                ForEach(segments.filter { citation?.includes($0) ?? true }) { segment in
                    VStack(alignment: .leading, spacing: 8) {
                        let headerLayout =
                            dynamicTypeSize.isAccessibilitySize
                            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
                            : AnyLayout(HStackLayout(alignment: .firstTextBaseline))
                        headerLayout {
                            Text(
                                segment.confirmedSpeakerName
                                    ?? String(localized: "Unconfirmed speaker (\(segment.localSpeakerId))")
                            ).font(.headline)
                            if !dynamicTypeSize.isAccessibilitySize { Spacer() }
                            Text(Duration.milliseconds(segment.segmentStartMS).formatted(.time(pattern: .hourMinuteSecond))).font(.caption)
                                .foregroundStyle(.secondary).monospacedDigit()
                        }
                        if segment.hasUnalignedSpeakerCorrection == true {
                            Label("Speaker correction remains passage evidence", systemImage: "exclamationmark.triangle").font(.caption)
                                .foregroundStyle(.secondary)
                        }
                        Text(segment.displayedTranscriptText).textSelection(.enabled)
                        if segment.hasUnalignedTextCorrection == true {
                            Label("Text correction remains in history", systemImage: "exclamationmark.triangle").font(.caption).foregroundStyle(
                                .secondary)
                        }
                        if segment.appliedTextCorrectionId != nil {
                            Label("Text corrected", systemImage: "pencil").font(.caption).foregroundStyle(.secondary)
                        }
                        Button {
                            editingSegment = segment
                        } label: {
                            Label("Edit transcript", systemImage: "pencil")
                        }
                        Button {
                            Task {
                                do {
                                    passageStartMS = segment.segmentStartMS
                                    playbackMedia = try await connection.recording(recording.id).mediaFiles.first {
                                        $0.id == mediaId(for: segment)
                                    }
                                } catch { problemText = error.localizedDescription }
                            }
                        } label: {
                            Label("Play passage", systemImage: "play")
                        }
                        Button {
                            let retained = RecordingSpeakerDrafts.read(accountKey: connection.accountKey, segmentId: segment.id)
                            selectedSegment = retained?.segment ?? segment
                            speakerDraft = retained
                            confirmedName =
                                retained?.confirmedName ?? segment.confirmedSpeakerName ?? ""
                            correctionStartMS = String(retained?.passageStartMS ?? segment.segmentStartMS)
                            correctionEndMS = String(retained?.passageEndMS ?? segment.segmentEndMS)
                            isWholeGroup = retained?.isWholeGroup ?? false
                            isNaming = true
                        } label: {
                            Label("Name speaker", systemImage: "person.crop.circle.badge.checkmark")
                        }
                    }.padding(.vertical, 4)
                }
                if citation == nil, transcriptView?.hasMoreSegments == true || timelineView?.hasMoreSegments == true {
                    Button {
                        Task { await loadPage(isAdditional: true) }
                    } label: {
                        Label("Load more passages", systemImage: "chevron.down")
                    }
                }
                if segments.isEmpty { Text("No completed transcript passages yet").foregroundStyle(.secondary) }
                if let citation, !segments.isEmpty, !segments.contains(where: { citation.includes($0) }) {
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
        .sheet(item: $editingSegment) { segment in
            RecordingTextEditor(connection: connection, recordingId: recording.id, segment: segment) { await loadPage(isAdditional: false) }
        }
        .sheet(isPresented: $isNaming) {
            NavigationStack {
                Form {
                    Section {
                        TextField("Confirmed name", text: $confirmedName)
                        if let speakers, !speakers.speakerIdentities.isEmpty {
                            Menu("Use a confirmed name") {
                                ForEach(speakers.speakerIdentities) { identity in
                                    Button(identity.confirmedName) { confirmedName = identity.confirmedName }
                                }
                            }
                        }
                        Toggle("Apply to this speaker group", isOn: $isWholeGroup)
                        if !isWholeGroup {
                            TextField("Passage start (milliseconds)", text: $correctionStartMS).keyboardType(.numberPad)
                            TextField("Passage end (milliseconds)", text: $correctionEndMS).keyboardType(.numberPad)
                        }
                    } footer: {
                        Text(
                            "Off changes this passage only. Naming is free metadata. Suitable voice samples are selected automatically, and learned material is reconciled."
                        )
                    }
                    .disabled(speakerDraft?.hasSubmittedRequest == true)
                    if speakerDraft?.hasSubmittedRequest == true {
                        Text("This label has an unconfirmed response. Retry the same label before changing it.").foregroundStyle(.secondary)
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
            if let transcriptRun {
                var segmentOffset = isAdditional ? nextSegmentOffset : 0
                if !isAdditional, let citation, let startMS = citation.passageStartMS, let endMS = citation.passageEndMS {
                    segmentOffset = try await connection.recordingTranscriptPassageOffset(
                        recordingTranscriptRunId: transcriptRun.id, passageStartMS: startMS, passageEndMS: endMS)
                }
                var page = try await connection.recordingTranscript(
                    recordingTranscriptRunId: transcriptRun.id, segmentOffset: segmentOffset)
                transcriptView = page
                nextSegmentOffset = segmentOffset + page.segments.count
                if isAdditional { segments.append(contentsOf: page.segments) } else { segments = page.segments }
                if !isAdditional, let citationEndMS = citation?.passageEndMS {
                    while page.hasMoreSegments, !page.segments.isEmpty,
                        (segments.last?.segmentStartMS ?? 0) < citationEndMS
                    {
                        try Task.checkCancellation()
                        page = try await connection.recordingTranscript(recordingTranscriptRunId: transcriptRun.id, segmentOffset: nextSegmentOffset)
                        nextSegmentOffset += page.segments.count
                        transcriptView = page
                        segments.append(contentsOf: page.segments)
                    }
                }
            } else {
                let page = try await connection.recordingTranscriptTimeline(
                    recordingId: recording.id, segmentOffset: isAdditional ? segments.count : 0)
                if isAdditional, let current = timelineView, current.sourceGeneration != page.sourceGeneration {
                    // A new publication invalidates page offsets; refresh rather than duplicate or skip evidence.
                    await loadPage(isAdditional: false)
                    return
                }
                timelineView = page
                if isAdditional { segments.append(contentsOf: page.segments) } else { segments = page.segments }
            }
            speakers = try await connection.recordingSpeakers(recordingId: recording.id)
            invalidations = try await connection.recordingReferenceInvalidations(recordingId: recording.id).filter {
                transcriptRun == nil || $0.recordingTranscriptRunId == transcriptRun?.id
            }
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }

    private func saveSpeaker() async {
        guard !isSaving, let selectedSegment, let speakers else { return }
        isSaving = true
        defer { isSaving = false }
        var draft =
            speakerDraft
            ?? RecordingSpeakerDraft(
                accountKey: connection.accountKey, recordingId: recording.id, segment: selectedSegment,
                expectedSourceGeneration: speakers.sourceGeneration)
        do {
            if !draft.hasSubmittedRequest {
                guard let passageStartMS = Int64(correctionStartMS), let passageEndMS = Int64(correctionEndMS),
                    isWholeGroup
                        || (passageStartMS >= selectedSegment.segmentStartMS && passageEndMS <= selectedSegment.segmentEndMS
                            && passageStartMS < passageEndMS)
                else { throw TeaNodeError.refused(String(localized: "Choose a positive interval within this passage.")) }
                draft.confirmedName = confirmedName.trimmingCharacters(in: .whitespacesAndNewlines)
                draft.isWholeGroup = isWholeGroup
                draft.passageStartMS = passageStartMS
                draft.passageEndMS = passageEndMS
                draft.expectedSourceGeneration = speakers.sourceGeneration
                draft.hasSubmittedRequest = true
                try RecordingSpeakerDrafts.save(draft)
                speakerDraft = draft
            }
            self.speakers = try await connection.correctRecordingSpeaker(
                recordingId: draft.recordingId, expectedSourceGeneration: draft.expectedSourceGeneration,
                clientCorrectionId: draft.clientCorrectionId, confirmedName: draft.confirmedName,
                segment: draft.segment, isWholeGroup: draft.isWholeGroup, passageStartMS: draft.passageStartMS, passageEndMS: draft.passageEndMS)
            RecordingSpeakerDrafts.finish(draft)
            speakerDraft = nil
            isNaming = false
            await loadPage(isAdditional: false)
            problemText = nil
        } catch {
            problemText = error.localizedDescription
            self.speakers = try? await connection.recordingSpeakers(recordingId: recording.id)
            if let refusal = error as? TeaNodeError, case .refused = refusal {
                draft.hasSubmittedRequest = false
                draft.expectedSourceGeneration = self.speakers?.sourceGeneration ?? draft.expectedSourceGeneration
                speakerDraft = draft
                try? RecordingSpeakerDrafts.save(draft)
            }
        }
    }

}

struct RecordingSpeakersScreen: View {
    let connection: Connection
    let recording: Recording
    @State private var speakers: RecordingSpeakersView
    @State private var problemText: String?
    @State private var playbackMedia: RecordingMedia?
    @State private var referenceStartMS: Int64 = 0
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
            if let problemText { Text(problemText).foregroundStyle(.secondary) }
        }
        .navigationTitle("Speakers and voice references")
        .refreshable {
            do { speakers = try await connection.recordingSpeakers(recordingId: recording.id) } catch { problemText = error.localizedDescription }
        }
        .sheet(item: $playbackMedia) { media in RecordingPlaybackScreen(connection: connection, media: media, passageStartMS: referenceStartMS) }
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
