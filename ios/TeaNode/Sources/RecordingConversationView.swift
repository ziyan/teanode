import SwiftUI
import TeaNodeKit

/// RecordingConversationModel reads a recording's transcript and carries what a person does to a passage of it: naming the
/// speaker, marking the speaker as themselves, correcting the words.
@MainActor
@Observable
final class RecordingConversationModel {
    let connection: Connection
    /// Empty for a capture whose recording the server does not know yet.
    var recordingId: String
    let transcriptRun: RecordingTranscriptRun?
    let citation: RecordingLink?
    var timelineView: RecordingTranscriptTimeline?
    var transcriptView: RecordingTranscriptView?
    var segments: [RecordingTranscriptSegment] = []
    var nextSegmentOffset = 0
    var speakers: RecordingSpeakersView?
    var invalidations: [RecordingReferenceInvalidation] = []
    var problemText: String?
    var doneText: String?
    var isLoaded = false
    var editingSegment: RecordingTranscriptSegment?
    var selectedSegment: RecordingTranscriptSegment?
    var confirmedName = ""
    var correctionStartMS = ""
    var correctionEndMS = ""
    var isWholeGroup = false
    var isNaming = false
    var isSaving = false
    var speakerDraft: RecordingSpeakerDraft?
    /// Whether this is the conversation of a recording still being captured: it shows the latest passages, the words just heard
    /// and the temporary speakers, and is read again every few seconds by its screen.
    let isCapture: Bool
    /// Words heard while the recording was captured that no finished transcript covers yet, as the server listed them.
    var liveUtterances: [RecordingLiveUtterance] = []
    /// The words this phone's own live stream heard, which the server may not have listed yet, and the words being said now.
    var heardUtterances: [RecordingLiveUtterance] = []
    var partialText = ""
    var partialStartMS: Int64 = 0

    /// How many of the latest passages a capture shows; the screen is for what is being said now.
    static let captureSegmentCount = 40

    init(connection: Connection, recording: Recording, transcriptRun: RecordingTranscriptRun? = nil, citation: RecordingLink? = nil) {
        self.connection = connection
        recordingId = recording.id
        self.transcriptRun = transcriptRun
        self.citation = citation
        isCapture = false
    }

    /// The conversation of a recording being captured; its identifier is set once the server has created it.
    init(connection: Connection, capturedRecordingId: String = "") {
        self.connection = connection
        recordingId = capturedRecordingId
        transcriptRun = nil
        citation = nil
        isCapture = true
    }

    var visibleSegments: [RecordingTranscriptSegment] { segments.filter { citation?.includes($0) ?? true } }
    var bubbles: [RecordingConversationBubble] {
        guard isCapture else { return RecordingConversation.bubbles(visibleSegments) }
        let liveWords = RecordingLiveWords.shown(
            serverUtterances: liveUtterances, heardUtterances: heardUtterances, partialText: partialText, partialStartMS: partialStartMS,
            segments: segments, windowStartMS: timelineView?.hasMoreSegments == true ? (segments.first?.segmentStartMS ?? 0) : 0)
        return RecordingConversation.bubbles(visibleSegments, liveUtterances: liveWords)
    }
    var speakerCount: Int { RecordingConversation.speakerCount(segments) }
    var hasMoreSegments: Bool { transcriptView?.hasMoreSegments == true || timelineView?.hasMoreSegments == true }

    /// Reads the latest passages of a capture, with the words just heard. A failed read is not shown: the next one, a few seconds
    /// later, shows what it can.
    func loadCapture() async {
        defer { isLoaded = true }
        guard !recordingId.isEmpty else { return }
        do {
            let first = try await connection.recordingTranscriptTimeline(
                recordingId: recordingId, segmentOffset: 0, pageSize: Self.captureSegmentCount)
            var page = first
            if first.hasMoreSegments {
                page = try await connection.recordingTranscriptTimeline(
                    recordingId: recordingId, segmentOffset: max(0, first.segmentCount - Self.captureSegmentCount),
                    pageSize: Self.captureSegmentCount)
            }
            timelineView = page
            segments = page.segments
            liveUtterances = first.liveUtterances ?? []
            speakers = try await connection.recordingSpeakers(recordingId: recordingId)
        } catch {}
    }

    func loadPage(isAdditional: Bool) async {
        if isCapture {
            await loadCapture()
            return
        }
        defer { isLoaded = true }
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
                    recordingId: recordingId, segmentOffset: isAdditional ? segments.count : 0)
                if isAdditional, let current = timelineView, current.sourceGeneration != page.sourceGeneration {
                    // A new publication invalidates page offsets; refresh rather than duplicate or skip evidence.
                    await loadPage(isAdditional: false)
                    return
                }
                timelineView = page
                if isAdditional { segments.append(contentsOf: page.segments) } else { segments = page.segments }
            }
            speakers = try await connection.recordingSpeakers(recordingId: recordingId)
            invalidations = try await connection.recordingReferenceInvalidations(recordingId: recordingId).filter {
                transcriptRun == nil || $0.recordingTranscriptRunId == transcriptRun?.id
            }
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }

    /// Opens the form that names a passage's speaker. `startsBlank` is for "Not me": the passage is someone else, so the
    /// name of whoever it is now is not offered back.
    func beginNaming(_ segment: RecordingTranscriptSegment, startsBlank: Bool = false) {
        guard segment.speakerLabelOfTemporarySpeaker == nil else {
            // A speaker of a capture under way is named by the label the timeline shows, in every piece at once: there is no
            // passage to choose and nothing to keep between attempts.
            selectedSegment = segment
            speakerDraft = nil
            confirmedName = ""
            isWholeGroup = true
            isNaming = true
            return
        }
        let retained = RecordingSpeakerDrafts.read(accountKey: connection.accountKey, segmentId: segment.id)
        selectedSegment = retained?.segment ?? segment
        speakerDraft = retained
        confirmedName = retained?.confirmedName ?? (startsBlank ? "" : segment.confirmedSpeakerName ?? "")
        correctionStartMS = String(retained?.passageStartMS ?? segment.segmentStartMS)
        correctionEndMS = String(retained?.passageEndMS ?? segment.segmentEndMS)
        isWholeGroup = retained?.isWholeGroup ?? false
        isNaming = true
    }

    /// "This is me": the speaker of this passage, wherever they speak in this request, is the person who owns the account.
    func confirmAsAccountOwner(_ segment: RecordingTranscriptSegment) async {
        if let label = segment.speakerLabelOfTemporarySpeaker {
            await nameTemporarySpeaker(label: label, confirmedName: "", isAccountOwner: true)
            return
        }
        let retained = RecordingSpeakerDrafts.read(accountKey: connection.accountKey, segmentId: segment.id)
        if let retained, retained.hasSubmittedRequest, retained.isAccountOwner != true {
            problemText = String(localized: "This label has an unconfirmed response. Retry the same label before changing it.")
            return
        }
        selectedSegment = retained?.segment ?? segment
        speakerDraft = retained
        confirmedName = ""
        isWholeGroup = true
        correctionStartMS = String(segment.segmentStartMS)
        correctionEndMS = String(segment.segmentEndMS)
        await saveSpeaker(isAccountOwner: true)
    }

    /// Whether the speaker being named is one of a capture under way, who is named by label rather than by passage.
    var isNamingTemporarySpeaker: Bool { selectedSegment?.speakerLabelOfTemporarySpeaker != nil }

    /// Names a speaker of a capture under way: from then on every piece, and the pieces still to come, show the name.
    private func nameTemporarySpeaker(label: String, confirmedName: String, isAccountOwner: Bool) async {
        guard !isSaving else { return }
        isSaving = true
        defer { isSaving = false }
        do {
            try await connection.nameRecordingTemporarySpeaker(
                recordingId: recordingId, speakerLabel: label, confirmedName: confirmedName.trimmingCharacters(in: .whitespacesAndNewlines),
                isAccountOwner: isAccountOwner)
            isNaming = false
            selectedSegment = nil
            await loadPage(isAdditional: false)
            problemText = nil
            doneText = isAccountOwner ? String(localized: "Marked as you.") : String(localized: "Speaker named.")
        } catch {
            problemText = error.localizedDescription
        }
    }

    func saveSpeaker(isAccountOwner: Bool = false) async {
        if let label = selectedSegment?.speakerLabelOfTemporarySpeaker {
            await nameTemporarySpeaker(label: label, confirmedName: confirmedName, isAccountOwner: isAccountOwner)
            return
        }
        guard !isSaving, let selectedSegment, let speakers else { return }
        isSaving = true
        defer { isSaving = false }
        var draft =
            speakerDraft
            ?? RecordingSpeakerDraft(
                accountKey: connection.accountKey, recordingId: recordingId, segment: selectedSegment,
                expectedSourceGeneration: speakers.sourceGeneration)
        do {
            if !draft.hasSubmittedRequest {
                guard let passageStartMS = Int64(correctionStartMS), let passageEndMS = Int64(correctionEndMS),
                    isWholeGroup
                        || (passageStartMS >= selectedSegment.segmentStartMS && passageEndMS <= selectedSegment.segmentEndMS
                            && passageStartMS < passageEndMS)
                else { throw TeaNodeError.refused(String(localized: "Choose a positive interval within this passage.")) }
                draft.confirmedName = confirmedName.trimmingCharacters(in: .whitespacesAndNewlines)
                draft.isAccountOwner = isAccountOwner ? true : nil
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
                segment: draft.segment, isWholeGroup: draft.isWholeGroup, passageStartMS: draft.passageStartMS, passageEndMS: draft.passageEndMS,
                isAccountOwner: draft.isAccountOwner)
            RecordingSpeakerDrafts.finish(draft)
            speakerDraft = nil
            isNaming = false
            await loadPage(isAdditional: false)
            problemText = nil
            doneText = draft.isAccountOwner == true ? String(localized: "Marked as you.") : String(localized: "Speaker named.")
        } catch {
            problemText = error.localizedDescription
            self.speakers = try? await connection.recordingSpeakers(recordingId: recordingId)
            if let refusal = error as? TeaNodeError, case .refused = refusal {
                draft.hasSubmittedRequest = false
                draft.expectedSourceGeneration = self.speakers?.sourceGeneration ?? draft.expectedSourceGeneration
                speakerDraft = draft
                try? RecordingSpeakerDrafts.save(draft)
            }
        }
    }
}

/// RecordingPassageActions is what a long press on a passage offers.
struct RecordingPassageActions {
    var play: @MainActor (RecordingTranscriptSegment) -> Void
    var copy: @MainActor (RecordingTranscriptSegment) -> Void
    var correctText: @MainActor (RecordingTranscriptSegment) -> Void
    var nameSpeaker: @MainActor (RecordingTranscriptSegment) -> Void
    var thisIsMe: @MainActor (RecordingTranscriptSegment) -> Void
    var notMe: @MainActor (RecordingTranscriptSegment) -> Void
    /// False on the screen of a capture under way, where there is no finished audio to play and no finished text to correct yet.
    var isPlaybackAvailable = true

    static var none: RecordingPassageActions {
        RecordingPassageActions(
            play: { _ in }, copy: { _ in }, correctText: { _ in }, nameSpeaker: { _ in }, thisIsMe: { _ in }, notMe: { _ in })
    }
}

/// RecordingBubbleView is what one speaker said before someone else spoke. Other people's sit on the left under their name,
/// the person's own on the right in the accent color.
struct RecordingBubbleView: View {
    let bubble: RecordingConversationBubble
    var playingSegmentId: String?
    let actions: RecordingPassageActions

    var body: some View {
        VStack(alignment: bubble.isAccountOwner ? .trailing : .leading, spacing: 4) {
            header
            VStack(alignment: .leading, spacing: 8) {
                ForEach(bubble.segments) { segment in
                    passage(segment)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            // The same look as the agent chat (PersonBubble and AgentBubble): the person's side in the accent's tint,
            // everyone else's in grey, 20 point corners, 14 by 10 padding.
            .background(
                bubble.isAccountOwner ? Color.accentColor.opacity(0.2) : Color(.secondarySystemBackground),
                in: .rect(cornerRadius: 20, style: .continuous))
        }
        .frame(maxWidth: .infinity, alignment: bubble.isAccountOwner ? .trailing : .leading)
        .padding(bubble.isAccountOwner ? .leading : .trailing, bubble.isAccountOwner ? 56 : 40)
        .listRowSeparator(.hidden)
        .listRowBackground(Color.clear)
    }

    @ViewBuilder
    private func passage(_ segment: RecordingTranscriptSegment) -> some View {
        if bubble.isLive {
            // Words just heard have no speaker yet, and no audio or finished text of their own: they can be copied.
            Text(segment.displayedTranscriptText)
                .contextMenu {
                    Button {
                        actions.copy(segment)
                    } label: {
                        Label("Copy", systemImage: "doc.on.doc")
                    }
                }
        } else {
            Text(segment.displayedTranscriptText)
                .fontWeight(segment.id == playingSegmentId ? .semibold : .regular)
                .contentShape(.rect)
                .onTapGesture { if actions.isPlaybackAvailable { actions.play(segment) } }
                .accessibilityAddTraits(actions.isPlaybackAvailable ? .isButton : [])
                .accessibilityHint(actions.isPlaybackAvailable ? Text("Plays the recording from here") : Text(verbatim: ""))
                .contextMenu {
                    if actions.isPlaybackAvailable {
                        Button {
                            actions.play(segment)
                        } label: {
                            Label("Play from here", systemImage: "play")
                        }
                    }
                    Button {
                        actions.copy(segment)
                    } label: {
                        Label("Copy", systemImage: "doc.on.doc")
                    }
                    if actions.isPlaybackAvailable {
                        Button {
                            actions.correctText(segment)
                        } label: {
                            Label("Correct text", systemImage: "pencil")
                        }
                    }
                    // Your own bubble has one way to say who it was
                    // instead: Not me, which asks for the name.
                    if bubble.isAccountOwner {
                        Button {
                            actions.notMe(segment)
                        } label: {
                            Label("Not me", systemImage: "person.crop.circle.badge.xmark")
                        }
                    } else {
                        Button {
                            actions.nameSpeaker(segment)
                        } label: {
                            Label("Name speaker", systemImage: "person.text.rectangle")
                        }
                        Button {
                            actions.thisIsMe(segment)
                        } label: {
                            Label("This is me", systemImage: "person.crop.circle.badge.checkmark")
                        }
                    }
                }
        }
    }

    @ViewBuilder
    private var header: some View {
        HStack(spacing: 6) {
            if !bubble.isAccountOwner && !bubble.isLive {
                if let name = bubble.confirmedSpeakerName {
                    Text(name).font(.caption.weight(.semibold))
                } else if let label = bubble.temporarySpeakerLabel {
                    Text(label).font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                } else {
                    Text("Speaker \(RecordingConversation.speakerLetter(ordinal: bubble.unconfirmedSpeakerOrdinal))")
                        .font(.caption.weight(.semibold)).foregroundStyle(.secondary)
                }
            }
            Text(Duration.milliseconds(bubble.startMS).formatted(.time(pattern: .hourMinuteSecond)))
                .font(.caption).foregroundStyle(.secondary).monospacedDigit()
        }
    }
}

/// RecordingConversationSection is the transcript as a conversation, for a scroll view or a list to hold.
struct RecordingConversationSection: View {
    let model: RecordingConversationModel
    let player: RecordingPlayer

    var body: some View {
        let playingSegmentId = player.isPlaying
            ? model.segments.first { $0.segmentStartMS <= player.timelineMS && player.timelineMS < $0.segmentEndMS }?.id : nil
        ForEach(model.bubbles) { bubble in
            RecordingBubbleView(bubble: bubble, playingSegmentId: playingSegmentId, actions: actions)
        }
        if model.hasMoreSegments, model.citation == nil {
            Button {
                Task { await model.loadPage(isAdditional: true) }
            } label: {
                Label("Load more passages", systemImage: "chevron.down")
            }
        }
        if model.isLoaded && model.segments.isEmpty {
            Text("No completed transcript passages yet").foregroundStyle(.secondary)
        }
    }

    private var actions: RecordingPassageActions {
        RecordingPassageActions(
            play: { segment in Task { await player.play(fromTimelineMS: segment.segmentStartMS) } },
            copy: { segment in
                UIPasteboard.general.string = segment.displayedTranscriptText
                model.doneText = String(localized: "Copied.")
            },
            correctText: { segment in model.editingSegment = segment },
            nameSpeaker: { segment in model.beginNaming(segment) },
            thisIsMe: { segment in Task { await model.confirmAsAccountOwner(segment) } },
            notMe: { segment in model.beginNaming(segment, startsBlank: true) })
    }
}

extension View {
    /// The two forms a passage's long-press menu opens as sheets: the text editor and the speaker naming form.
    func recordingPassageSheets(_ model: RecordingConversationModel) -> some View {
        modifier(RecordingPassageSheets(model: model))
    }
}

struct RecordingPassageSheets: ViewModifier {
    @Bindable var model: RecordingConversationModel

    func body(content: Content) -> some View {
        content
            .sheet(item: $model.editingSegment) { segment in
                RecordingTextEditor(connection: model.connection, recordingId: model.recordingId, segment: segment) {
                    await model.loadPage(isAdditional: false)
                    model.doneText = String(localized: "Passage corrected.")
                }
            }
            .sheet(isPresented: $model.isNaming) { RecordingSpeakerNamingSheet(model: model) }
    }
}

struct RecordingSpeakerNamingSheet: View {
    @Bindable var model: RecordingConversationModel
    @State private var suggestions: [RecordingSpeakerNameSuggestion] = []

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Confirmed name", text: $model.confirmedName)
                    .textContentType(.name)
                    if !model.isNamingTemporarySpeaker {
                        Toggle("Apply to this speaker group", isOn: $model.isWholeGroup)
                    }
                    if !model.isNamingTemporarySpeaker, !model.isWholeGroup {
                        TextField("Passage start (milliseconds)", text: $model.correctionStartMS).keyboardType(.numberPad)
                        TextField("Passage end (milliseconds)", text: $model.correctionEndMS).keyboardType(.numberPad)
                    }
                } footer: {
                    if model.isNamingTemporarySpeaker {
                        Text("Everything this speaker says in this recording, now and later, gets the name.")
                    } else {
                        Text(
                            "Off changes this passage only. Naming is free metadata. Suitable voice samples are selected automatically, and learned material is reconciled."
                        )
                    }
                }
                .disabled(model.speakerDraft?.hasSubmittedRequest == true)
                // Names from the voices already named, the agent's memory
                // and the contacts, narrowed as the name is typed.
                if !suggestions.isEmpty, model.speakerDraft?.hasSubmittedRequest != true {
                    Section("Suggestions") {
                        ForEach(suggestions) { suggestion in
                            Button {
                                model.confirmedName = suggestion.confirmedName
                            } label: {
                                Label(suggestion.confirmedName, systemImage: Self.symbolName(suggestion.speakerNameSource))
                            }
                            .accessibilityHint(Self.sourceText(suggestion.speakerNameSource))
                        }
                    }
                }
                if model.speakerDraft?.hasSubmittedRequest == true {
                    Text("This label has an unconfirmed response. Retry the same label before changing it.").foregroundStyle(.secondary)
                }
            }
            .toast(done: .constant(nil), problem: $model.problemText)
            .task(id: model.confirmedName) {
                do { try await Task.sleep(for: .milliseconds(200)) } catch { return }
                let typed = model.confirmedName.trimmingCharacters(in: .whitespacesAndNewlines)
                let offered = (try? await model.connection.suggestRecordingSpeakerNames(searchText: typed)) ?? []
                guard !Task.isCancelled else { return }
                suggestions = offered.filter { $0.confirmedName != typed }
            }
            .navigationTitle("Name speaker")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { model.isNaming = false }.disabled(model.isSaving) }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") { Task { await model.saveSpeaker() } }.disabled(
                        model.isSaving || model.confirmedName.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        }
    }
}

extension RecordingSpeakerNamingSheet {
    static func symbolName(_ speakerNameSource: String) -> String {
        switch speakerNameSource {
        case "memory": "brain"
        case "contact": "person.crop.circle"
        default: "waveform"
        }
    }

    static func sourceText(_ speakerNameSource: String) -> String {
        switch speakerNameSource {
        case "memory": String(localized: "From your agent's memory")
        case "contact": String(localized: "From your contacts")
        default: String(localized: "A voice you named before")
        }
    }
}

#if DEBUG
    private func previewBubbles() -> [RecordingConversationBubble] {
        RecordingConversation.bubbles(DemoServer.conversationSegments)
    }

    #Preview("Conversation, light") {
        ScrollView {
            LazyVStack(spacing: 12) {
                ForEach(previewBubbles()) { RecordingBubbleView(bubble: $0, playingSegmentId: nil, actions: .none) }
            }.padding()
        }.preferredColorScheme(.light)
    }
    #Preview("Conversation, dark") {
        ScrollView {
            LazyVStack(spacing: 12) {
                ForEach(previewBubbles()) { RecordingBubbleView(bubble: $0, playingSegmentId: nil, actions: .none) }
            }.padding()
        }.preferredColorScheme(.dark)
    }
#endif
