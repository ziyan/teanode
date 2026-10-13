import SwiftUI
import TeaNodeKit

/// RecordingConversationModel reads a recording's transcript and carries what a person does to a passage of it: naming the
/// speaker, marking the speaker as themselves, correcting the words.
@MainActor
@Observable
final class RecordingConversationModel {
    let connection: Connection
    let recording: Recording
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
    var isLoadingMore = false
    var isLoadingMoreFailed = false
    var editingSegment: RecordingTranscriptSegment?
    /// The passage whose pages of memory are being looked at.
    var memorySegment: RecordingTranscriptSegment?
    var selectedSegment: RecordingTranscriptSegment?
    var confirmedName = ""
    var correctionStartMS = ""
    var correctionEndMS = ""
    var isWholeGroup = false
    var isNaming = false
    var isSaving = false
    var speakerDraft: RecordingSpeakerDraft?

    init(connection: Connection, recording: Recording, transcriptRun: RecordingTranscriptRun? = nil, citation: RecordingLink? = nil) {
        self.connection = connection
        self.recording = recording
        self.transcriptRun = transcriptRun
        self.citation = citation
    }

    var visibleSegments: [RecordingTranscriptSegment] { segments.filter { citation?.includes($0) ?? true } }
    var bubbles: [RecordingConversationBubble] { RecordingConversation.bubbles(visibleSegments) }
    var speakerCount: Int { RecordingConversation.speakerCount(segments) }
    var hasMoreSegments: Bool { transcriptView?.hasMoreSegments == true || timelineView?.hasMoreSegments == true }

    /// Reads the next page of passages as the person scrolls to the end of what is shown. A page that fails is not read again
    /// until the person asks, so a failure does not turn into a loop.
    func loadMoreSegments() async {
        guard hasMoreSegments, !isLoadingMore else { return }
        isLoadingMore = true
        defer { isLoadingMore = false }
        problemText = nil
        await loadPage(isAdditional: true)
        isLoadingMoreFailed = problemText != nil
    }

    func loadPage(isAdditional: Bool) async {
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
                    recordingId: recording.id, segmentOffset: isAdditional ? segments.count : 0)
                if isAdditional, let current = timelineView, current.sourceGeneration != page.sourceGeneration {
                    // A new publication invalidates page offsets; refresh rather than duplicate or skip evidence.
                    await loadPage(isAdditional: false)
                    return
                }
                timelineView = page
                if isAdditional { segments.append(contentsOf: page.segments) } else { segments = page.segments }
            }
            // The speakers and what invalidated a reference do not change with the page, so a later page keeps them.
            if !isAdditional || speakers == nil {
                speakers = try await connection.recordingSpeakers(recordingId: recording.id)
                invalidations = try await connection.recordingReferenceInvalidations(recordingId: recording.id).filter {
                    transcriptRun == nil || $0.recordingTranscriptRunId == transcriptRun?.id
                }
            }
            problemText = nil
        } catch { problemText = error.localizedDescription }
    }

    /// Opens the form that names a passage's speaker. `startsBlank` is for "Not me": the passage is someone else, so the
    /// name of whoever it is now is not offered back.
    func beginNaming(_ segment: RecordingTranscriptSegment, startsBlank: Bool = false) {
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

    func saveSpeaker(isAccountOwner: Bool = false) async {
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

/// RecordingPassageActions is what a long press on a passage offers.
struct RecordingPassageActions {
    var play: @MainActor (RecordingTranscriptSegment) -> Void
    var copy: @MainActor (RecordingTranscriptSegment) -> Void
    var correctText: @MainActor (RecordingTranscriptSegment) -> Void
    var nameSpeaker: @MainActor (RecordingTranscriptSegment) -> Void
    var thisIsMe: @MainActor (RecordingTranscriptSegment) -> Void
    var notMe: @MainActor (RecordingTranscriptSegment) -> Void
    var inMemory: @MainActor (RecordingTranscriptSegment) -> Void = { _ in }

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
                    Text(segment.displayedTranscriptText)
                        .fontWeight(segment.id == playingSegmentId ? .semibold : .regular)
                        .contentShape(.rect)
                        .onTapGesture { actions.play(segment) }
                        .accessibilityAddTraits(.isButton)
                        .accessibilityHint("Plays the recording from here")
                        .contextMenu {
                            Button {
                                actions.play(segment)
                            } label: {
                                Label("Play from here", systemImage: "play")
                            }
                            Button {
                                actions.copy(segment)
                            } label: {
                                Label("Copy", systemImage: "doc.on.doc")
                            }
                            Button {
                                actions.inMemory(segment)
                            } label: {
                                Label("In memory", systemImage: "brain")
                            }
                            Button {
                                actions.correctText(segment)
                            } label: {
                                Label("Correct text", systemImage: "pencil")
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
    private var header: some View {
        HStack(spacing: 6) {
            if !bubble.isAccountOwner {
                if let name = bubble.confirmedSpeakerName {
                    Text(name).font(.caption.weight(.semibold))
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
        // The row after the last bubble: when it comes into view the next page is read, and again each time a page
        // arrives while it is still in view.
        if model.hasMoreSegments, model.citation == nil {
            if model.isLoadingMoreFailed {
                Button {
                    Task { await model.loadMoreSegments() }
                } label: {
                    Label("Load more passages", systemImage: "chevron.down")
                }
            } else {
                ProgressView()
                    .frame(maxWidth: .infinity)
                    .task(id: model.segments.count) { await model.loadMoreSegments() }
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
            notMe: { segment in model.beginNaming(segment, startsBlank: true) },
            inMemory: { segment in model.memorySegment = segment })
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
                RecordingTextEditor(connection: model.connection, recordingId: model.recording.id, segment: segment) {
                    await model.loadPage(isAdditional: false)
                    model.doneText = String(localized: "Passage corrected.")
                }
            }
            .sheet(isPresented: $model.isNaming) { RecordingSpeakerNamingSheet(model: model) }
            .sheet(item: $model.memorySegment) { segment in
                RecordingPassageMemorySheet(recordingId: model.recording.id, segment: segment)
            }
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
                    Toggle("Apply to this speaker group", isOn: $model.isWholeGroup)
                    if !model.isWholeGroup {
                        TextField("Passage start (milliseconds)", text: $model.correctionStartMS).keyboardType(.numberPad)
                        TextField("Passage end (milliseconds)", text: $model.correctionEndMS).keyboardType(.numberPad)
                    }
                } footer: {
                    Text(
                        "Off changes this passage only. Naming is free metadata. Suitable voice samples are selected automatically, and learned material is reconciled."
                    )
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


/// RecordingPassageMemorySheet is the pages of the agent's memory linked to
/// what was said in a passage: the people who spoke in it, and whatever the
/// agent filed from this stretch of the recording. Asked for when the menu
/// item is chosen, not for every passage of the transcript.
struct RecordingPassageMemorySheet: View {
    let recordingId: String
    let segment: RecordingTranscriptSegment
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @State private var model = InMemoryModel()
    @State private var isLoaded = false

    var body: some View {
        NavigationStack {
            List {
                if model.pages.isEmpty {
                    Text(isLoaded ? "No page in memory is linked to this passage yet." : "Reading…").foregroundStyle(.secondary)
                } else {
                    Section {
                        ForEach(model.pages) { link in
                            Button {
                                dismiss()
                                app.openedKnowledgePath = link.nodePath
                            } label: {
                                HStack {
                                    Label(link.nodeName.isEmpty ? link.nodePath : link.nodeName, systemImage: KnowledgeSymbols.symbol(link.nodeKind))
                                    Spacer()
                                    if link.isProposed {
                                        Chip(text: String(localized: "A guess"), isAlert: false)
                                    }
                                }
                            }
                        }
                    } footer: {
                        Text("Pages in the agent's memory that mention what was said here.")
                    }
                }
            }
            .navigationTitle("In memory")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .task {
                guard let connection = app.connection else { return }
                await model.load(
                    connection: connection, recordKind: "recording_passage", recordId: recordingId,
                    passageStartMS: segment.segmentStartMS, passageEndMS: segment.segmentEndMS)
                isLoaded = true
            }
        }
    }
}
